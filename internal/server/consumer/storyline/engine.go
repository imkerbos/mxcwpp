// Package storyline aggregates Agent-side story_id-tagged events into
// attack storylines on the Server. Each storyline groups causally related
// events on a single host, tracks severity escalation, and persists
// timeline data for SOC investigation.
package storyline

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

const (
	// flushInterval controls how often in-memory storylines are checkpointed to DB.
	flushInterval = 30 * time.Second
	// staleTimeout marks storylines as stale (no new events) for cleanup.
	staleTimeout = 30 * time.Minute
)

// storyState holds in-memory state for an active storyline.
type storyState struct {
	mu          sync.Mutex
	storyID     string
	hostID      string
	hostname    string
	severity    string
	phase       string
	ruleNames   map[string]struct{}
	eventCount  int
	alertCount  int
	riskScore   float64
	firstSeen   time.Time
	lastSeen    time.Time
	dirty       bool
	pendingEvts []model.StorylineEvent
}

// Engine aggregates story_id-tagged events into attack storylines.
type Engine struct {
	mu      sync.RWMutex
	stories map[string]*storyState // story_id → state
	db      *gorm.DB
	logger  *zap.Logger
}

// NewEngine creates a storyline aggregation engine.
func NewEngine(db *gorm.DB, logger *zap.Logger) *Engine {
	return &Engine{
		stories: make(map[string]*storyState),
		db:      db,
		logger:  logger,
	}
}

// Ingest processes an event with a story_id.
// Called from the consumer router for events carrying story_id field.
func (e *Engine) Ingest(storyID, hostID, hostname string, dataType int32, fields map[string]string) {
	st := e.getOrCreate(storyID, hostID, hostname)
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	st.lastSeen = now
	st.eventCount++
	st.dirty = true

	// Track matched rules.
	isAlert := false
	ruleName := fields["agent_rule_name"]
	if ruleName != "" {
		st.ruleNames[ruleName] = struct{}{}
		st.alertCount++
		isAlert = true
	}

	// Escalate severity.
	severity := fields["agent_severity"]
	if severityRank(severity) > severityRank(st.severity) {
		st.severity = severity
	}

	// Track MITRE phase.
	if tactic := fields["agent_mitre_tactic"]; tactic != "" {
		st.phase = tactic
	}

	// Update risk score based on alert density.
	if st.eventCount > 0 {
		alertRatio := float64(st.alertCount) / float64(st.eventCount)
		st.riskScore = alertRatio * 100 * severityMultiplier(st.severity)
		if st.riskScore > 100 {
			st.riskScore = 100
		}
	}

	// Build denormalized event detail (key fields for timeline).
	detail := buildDetail(dataType, fields)

	evt := model.StorylineEvent{
		StoryID:   storyID,
		HostID:    hostID,
		DataType:  dataType,
		EventType: fields["event_type"],
		PID:       fields["pid"],
		Exe:       fields["exe"],
		Detail:    detail,
		Timestamp: model.LocalTime(now),
	}
	if isAlert {
		evt.RuleName = ruleName
		evt.Severity = severity
	}
	st.pendingEvts = append(st.pendingEvts, evt)
}

// StartFlush starts a background goroutine that periodically flushes dirty storylines to DB.
func (e *Engine) StartFlush(done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				e.flush()
				return
			case <-ticker.C:
				e.flush()
			}
		}
	}()
}

func (e *Engine) getOrCreate(storyID, hostID, hostname string) *storyState {
	e.mu.RLock()
	st, ok := e.stories[storyID]
	e.mu.RUnlock()
	if ok {
		return st
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if st, ok = e.stories[storyID]; ok {
		return st
	}
	st = &storyState{
		storyID:   storyID,
		hostID:    hostID,
		hostname:  hostname,
		severity:  "low",
		ruleNames: make(map[string]struct{}),
		firstSeen: time.Now(),
		lastSeen:  time.Now(),
	}
	e.stories[storyID] = st
	return st
}

// flush persists all dirty storylines and their events to DB.
func (e *Engine) flush() {
	e.mu.RLock()
	var dirty []*storyState
	var stale []string
	cutoff := time.Now().Add(-staleTimeout)
	for sid, st := range e.stories {
		st.mu.Lock()
		if st.dirty {
			dirty = append(dirty, st)
		}
		if st.lastSeen.Before(cutoff) {
			stale = append(stale, sid)
		}
		st.mu.Unlock()
	}
	e.mu.RUnlock()

	for _, st := range dirty {
		e.persistStory(st)
	}

	// Evict stale storylines from memory (already persisted).
	if len(stale) > 0 {
		e.mu.Lock()
		for _, sid := range stale {
			delete(e.stories, sid)
		}
		e.mu.Unlock()
	}
}

func (e *Engine) persistStory(st *storyState) {
	st.mu.Lock()
	ruleList := make([]string, 0, len(st.ruleNames))
	for r := range st.ruleNames {
		ruleList = append(ruleList, r)
	}
	record := model.Storyline{
		StoryID:     st.storyID,
		HostID:      st.hostID,
		Hostname:    st.hostname,
		Severity:    st.severity,
		Phase:       st.phase,
		RuleNames:   strings.Join(ruleList, ","),
		EventCount:  st.eventCount,
		AlertCount:  st.alertCount,
		RiskScore:   st.riskScore,
		FirstSeenAt: model.LocalTime(st.firstSeen),
		LastSeenAt:  model.LocalTime(st.lastSeen),
	}
	events := make([]model.StorylineEvent, len(st.pendingEvts))
	copy(events, st.pendingEvts)
	st.pendingEvts = st.pendingEvts[:0]
	st.dirty = false
	st.mu.Unlock()

	// Upsert storyline.
	result := e.db.Where("story_id = ?", record.StoryID).
		Assign(model.Storyline{
			Severity:   record.Severity,
			Phase:      record.Phase,
			RuleNames:  record.RuleNames,
			EventCount: record.EventCount,
			AlertCount: record.AlertCount,
			RiskScore:  record.RiskScore,
			LastSeenAt: record.LastSeenAt,
		}).
		FirstOrCreate(&record)
	if result.Error != nil {
		e.logger.Warn("持久化故事线失败", zap.String("story_id", record.StoryID), zap.Error(result.Error))
		return
	}

	// Batch insert events.
	if len(events) > 0 {
		if err := e.db.CreateInBatches(events, 100).Error; err != nil {
			e.logger.Warn("持久化故事线事件失败", zap.String("story_id", record.StoryID), zap.Error(err))
		}
	}
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func severityMultiplier(s string) float64 {
	switch s {
	case "critical":
		return 1.0
	case "high":
		return 0.8
	case "medium":
		return 0.5
	default:
		return 0.3
	}
}

func buildDetail(dataType int32, fields map[string]string) string {
	detail := make(map[string]string)
	// Include event-type-specific key fields.
	switch dataType {
	case 3000: // process
		for _, k := range []string{"ppid", "cmdline", "uid", "cwd"} {
			if v := fields[k]; v != "" {
				detail[k] = v
			}
		}
	case 3001: // file
		for _, k := range []string{"file_path", "file_action"} {
			if v := fields[k]; v != "" {
				detail[k] = v
			}
		}
	case 3002: // network
		for _, k := range []string{"remote_addr", "remote_port", "protocol"} {
			if v := fields[k]; v != "" {
				detail[k] = v
			}
		}
	case 3003: // DNS
		for _, k := range []string{"domain", "rcode"} {
			if v := fields[k]; v != "" {
				detail[k] = v
			}
		}
	}
	// Add IOC info if present.
	if fields["ioc_match"] == "true" {
		detail["ioc_type"] = fields["ioc_type"]
		detail["ioc_value"] = fields["ioc_value"]
	}
	b, _ := json.Marshal(detail)
	return string(b)
}
