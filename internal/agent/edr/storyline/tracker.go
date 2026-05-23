// Package storyline implements the Agent-side CausalTracker for attack storyline
// correlation. When a detection rule matches, the tracker assigns a story_id (UUID)
// to the triggering process. All subsequent events from that process and its
// descendants inherit the story_id, enabling the Server to reconstruct the full
// attack narrative from a single correlated event stream.
package storyline

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// maxEntries caps the tracker size to prevent unbounded growth.
	maxEntries = 10000
	// entryTTL controls how long a PID→story mapping is retained after last activity.
	entryTTL = 2 * time.Hour
)

type entry struct {
	storyID  string
	lastSeen time.Time
}

// Tracker maintains PID → story_id mappings for the local host.
// Thread-safe; designed for high-frequency event path.
type Tracker struct {
	mu      sync.RWMutex
	entries map[int32]*entry // pid → story entry
	logger  *zap.Logger
}

// NewTracker creates a CausalTracker.
func NewTracker(logger *zap.Logger) *Tracker {
	return &Tracker{
		entries: make(map[int32]*entry),
		logger:  logger,
	}
}

// Assign creates a new story_id for the given PID (triggered by rule match).
// If the PID already has a story_id, returns the existing one.
func (t *Tracker) Assign(pid int32) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if e, ok := t.entries[pid]; ok {
		e.lastSeen = time.Now()
		return e.storyID
	}

	sid := generateStoryID()
	t.entries[pid] = &entry{storyID: sid, lastSeen: time.Now()}
	return sid
}

// Inherit propagates story_id from parent PID to child PID.
// Called on process_exec events. Returns the inherited story_id or empty string.
func (t *Tracker) Inherit(ppid, pid int32) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	parent, ok := t.entries[ppid]
	if !ok {
		return ""
	}
	parent.lastSeen = time.Now()

	// Child inherits parent's story_id.
	t.entries[pid] = &entry{storyID: parent.storyID, lastSeen: time.Now()}
	return parent.storyID
}

// Lookup returns the story_id for a PID, or empty string if not tracked.
func (t *Tracker) Lookup(pid int32) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	e, ok := t.entries[pid]
	if !ok {
		return ""
	}
	return e.storyID
}

// LookupStr converts string PID to int32 and looks up story_id.
func (t *Tracker) LookupStr(pidStr string) string {
	pid, err := strconv.ParseInt(pidStr, 10, 32)
	if err != nil {
		return ""
	}
	return t.Lookup(int32(pid))
}

// Remove removes a PID from tracking (called on process_exit).
// Does NOT immediately remove — keeps entry for entryTTL for late-arriving events.
func (t *Tracker) Remove(pid int32) {
	// No-op: rely on Cleanup() TTL instead of immediate removal.
	// Exit events may arrive before related file/network events.
}

// Cleanup removes stale entries older than entryTTL.
func (t *Tracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := time.Now().Add(-entryTTL)
	for pid, e := range t.entries {
		if e.lastSeen.Before(cutoff) {
			delete(t.entries, pid)
		}
	}

	// Hard cap: if still over maxEntries, evict oldest.
	if len(t.entries) > maxEntries {
		type pidTime struct {
			pid      int32
			lastSeen time.Time
		}
		all := make([]pidTime, 0, len(t.entries))
		for pid, e := range t.entries {
			all = append(all, pidTime{pid, e.lastSeen})
		}
		// Sort by lastSeen ascending (oldest first) — simple selection of oldest half.
		target := len(t.entries) - maxEntries
		for i := 0; i < target; i++ {
			oldest := i
			for j := i + 1; j < len(all); j++ {
				if all[j].lastSeen.Before(all[oldest].lastSeen) {
					oldest = j
				}
			}
			all[i], all[oldest] = all[oldest], all[i]
			delete(t.entries, all[i].pid)
		}
	}
}

// Stats returns the number of tracked PIDs and unique story_ids.
func (t *Tracker) Stats() (pids, stories int) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, e := range t.entries {
		seen[e.storyID] = struct{}{}
	}
	return len(t.entries), len(seen)
}

// generateStoryID creates a random 16-byte hex string (32 chars).
func generateStoryID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
