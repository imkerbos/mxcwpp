//go:build linux

// Package edr implements the built-in EDR engine for the MxSec Agent.
//
// The engine runs in the same process as the Agent (single-process architecture),
// collecting kernel/userspace events and forwarding them to the Server via the
// existing gRPC transport layer.
//
// Architecture decision: EDR is not a plugin. Single process = zero IPC overhead
// on the hot path, unified resource management, and simpler self-protection.
// Scanner and Baseline remain as separate plugin processes.
package edr

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	grpcProto "github.com/imkerbos/mxsec-platform/api/proto/grpc"
	"github.com/imkerbos/mxsec-platform/internal/agent/edr/collector"
	"github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
	"github.com/imkerbos/mxsec-platform/internal/agent/edr/ioc"
	"github.com/imkerbos/mxsec-platform/internal/agent/edr/rule"
	"github.com/imkerbos/mxsec-platform/internal/agent/transport"
)

// iocTaskDataType is the DataType for IOC data push from AgentCenter.
// Registered in docs/datatype-allocation.md — do not reuse.
const iocTaskDataType = int32(9300)

// Engine is the core EDR engine that manages the event collection pipeline.
type Engine struct {
	logger      *zap.Logger
	transport   *transport.Manager
	collector   collector.Collector
	ruleMgr     *rule.Manager
	actionExec  *rule.ActionExecutor
	auditLog    *rule.AuditLogger
	selfProtect *SelfProtect
	iocStore    *ioc.Store
	taskCh      <-chan *grpcProto.Task
	wg          sync.WaitGroup

	// Pipeline counters for monitoring and heartbeat reporting.
	eventsForwarded atomic.Uint64
	eventsDropped   atomic.Uint64
	rulesMatched    atomic.Uint64
	iocMatched      atomic.Uint64
}

// DefaultRuleDir is the default rule directory on Linux agents.
const DefaultRuleDir = "/var/lib/mxsec/rules"

// NewEngine creates a new EDR engine.
// It auto-detects the best collector mode (eBPF or userspace) for the running kernel.
// ruleDir is the directory containing YAML rule files; empty string uses DefaultRuleDir.
func NewEngine(logger *zap.Logger, transportMgr *transport.Manager, ruleDir string) (*Engine, error) {
	coll, err := collector.DetectAndCreate(logger)
	if err != nil {
		return nil, err
	}

	if ruleDir == "" {
		ruleDir = DefaultRuleDir
	}

	rm := rule.NewManager(logger.Named("rule"), ruleDir)
	if err := rm.Load(); err != nil {
		// Rule loading failure is non-fatal — engine still collects events.
		logger.Warn("failed to load rules, running without rule engine",
			zap.String("rule_dir", ruleDir),
			zap.Error(err),
		)
	}

	// Initialize audit logger and action executor.
	auditLog, err := rule.NewAuditLogger(logger.Named("audit"), "")
	if err != nil {
		logger.Warn("failed to create audit logger, response actions disabled",
			zap.Error(err),
		)
	}

	var actionExec *rule.ActionExecutor
	if auditLog != nil {
		actionExec = rule.NewActionExecutor(logger.Named("action"), auditLog, "")
	}

	logger.Info("EDR engine initialized",
		zap.String("collector_mode", string(coll.Mode())),
		zap.Any("capabilities", coll.Capabilities()),
		zap.Int("rules_loaded", rm.Rules().Count),
	)

	// Initialize IOC store and register task channel for IOC data delivery.
	iocStore := ioc.NewStore(logger.Named("ioc"))
	taskCh := transportMgr.RegisterTaskChannel("edr")

	return &Engine{
		logger:      logger,
		transport:   transportMgr,
		collector:   coll,
		ruleMgr:     rm,
		actionExec:  actionExec,
		auditLog:    auditLog,
		selfProtect: NewSelfProtect(logger.Named("selfprotect")),
		iocStore:    iocStore,
		taskCh:      taskCh,
	}, nil
}

// Start begins event collection, rule matching, and self-protection.
func (e *Engine) Start(ctx context.Context) error {
	eventCh, err := e.collector.Start(ctx)
	if err != nil {
		return err
	}

	// Start self-protection (systemd notify + chattr).
	e.selfProtect.Start(ctx, &e.wg)

	e.wg.Add(2)
	go e.forwardEvents(ctx, eventCh)
	go e.processTaskLoop(ctx)

	e.logger.Info("EDR engine started")
	return nil
}

// Stop gracefully shuts down the EDR engine.
func (e *Engine) Stop() error {
	e.selfProtect.Stop()
	err := e.collector.Stop()
	e.wg.Wait()
	if e.auditLog != nil {
		_ = e.auditLog.Close()
	}
	e.logger.Info("EDR engine stopped")
	return err
}

// Mode returns the current collector mode (for heartbeat reporting).
func (e *Engine) Mode() collector.Mode {
	return e.collector.Mode()
}

// Capabilities returns the current collector capabilities (for heartbeat reporting).
func (e *Engine) Capabilities() []collector.Capability {
	return e.collector.Capabilities()
}

// HookType returns the BPF hook mechanism detected (for heartbeat reporting).
func (e *Engine) HookType() collector.HookType {
	return e.collector.HookType()
}

// Stats returns cumulative event pipeline counters (forwarded, dropped).
func (e *Engine) Stats() (forwarded, dropped uint64) {
	return e.eventsForwarded.Load(), e.eventsDropped.Load()
}

// DegradationLevel returns the current dynamic degradation level.
func (e *Engine) DegradationLevel() int32 {
	return e.collector.DegradationLevel()
}

// GetEDRMode implements heartbeat.EDRStatusGetter.
func (e *Engine) GetEDRMode() string {
	return string(e.collector.Mode())
}

// GetEDRCapabilities implements heartbeat.EDRStatusGetter.
func (e *Engine) GetEDRCapabilities() []string {
	caps := e.collector.Capabilities()
	result := make([]string, len(caps))
	for i, c := range caps {
		result[i] = string(c)
	}
	return result
}

// GetEDRHookType implements heartbeat.EDRStatusGetter.
func (e *Engine) GetEDRHookType() string {
	return string(e.collector.HookType())
}

// GetEDRStats implements heartbeat.EDRStatusGetter.
func (e *Engine) GetEDRStats() (forwarded, dropped uint64) {
	return e.eventsForwarded.Load(), e.eventsDropped.Load()
}

// RulesVersion returns the current rule set version for heartbeat reporting.
func (e *Engine) RulesVersion() string {
	return e.ruleMgr.Rules().Version
}

// RulesCount returns the number of loaded agent-enabled rules.
func (e *Engine) RulesCount() int {
	return e.ruleMgr.Rules().Count
}

// RulesMatched returns the cumulative count of rule match events.
func (e *Engine) RulesMatched() uint64 {
	return e.rulesMatched.Load()
}

// ReloadRules reloads rules from the rule directory. Thread-safe.
func (e *Engine) ReloadRules() error {
	return e.ruleMgr.Load()
}

// SelfProtectManager returns the self-protection manager for use by other
// modules (e.g., updater needs to temporarily unlock file immutability
// before installing packages).
func (e *Engine) SelfProtectManager() *SelfProtect {
	return e.selfProtect
}

// forwardEvents reads events from the collector channel, evaluates rules,
// annotates matching events, and sends them through the transport layer.
func (e *Engine) forwardEvents(ctx context.Context, eventCh <-chan *event.Event) {
	defer e.wg.Done()

	const sourceName = "edr"
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-eventCh:
			if !ok {
				return
			}

			// Rule matching: evaluate all rules indexed for this event type.
			e.evaluateRules(evt)

			// IOC collision: check network events against threat intelligence.
			e.checkIOC(evt)

			record := evt.ToRecord()

			if err := e.transport.SendPluginData(sourceName, record); err != nil {
				e.eventsDropped.Add(1)
				e.logger.Warn("failed to send EDR event",
					zap.String("event_type", string(evt.EventType)),
					zap.Error(err),
				)
			} else {
				e.eventsForwarded.Add(1)
			}
		}
	}
}

// evaluateRules runs the rule engine against an event.
// If rules match, the event Fields are annotated with match metadata.
// Server-side CEL can use these annotations for deeper analysis.
func (e *Engine) evaluateRules(evt *event.Event) {
	matched := e.ruleMgr.Match(string(evt.EventType), evt.Fields)
	if len(matched) == 0 {
		return
	}

	e.rulesMatched.Add(uint64(len(matched)))

	// Annotate event with first (highest severity) match.
	// Multiple matches are joined in agent_rule_ids for Server correlation.
	best := matched[0]
	for _, r := range matched[1:] {
		if severityRank(r.Severity) > severityRank(best.Severity) {
			best = r
		}
	}

	evt.SetField("agent_match", "true")
	evt.SetField("agent_rule_id", best.ID)
	evt.SetField("agent_rule_name", best.Name)
	evt.SetField("agent_severity", string(best.Severity))
	evt.SetField("agent_action", string(best.Agent.Action))
	evt.SetField("agent_enforce", boolStr(best.Agent.Enforce))

	if len(matched) > 1 {
		ids := make([]string, len(matched))
		for i, r := range matched {
			ids[i] = r.ID
		}
		evt.SetField("agent_rule_ids", strings.Join(ids, ","))
	}

	if best.MITRE != nil {
		evt.SetField("agent_mitre_tactic", best.MITRE.Tactic)
		evt.SetField("agent_mitre_technique", best.MITRE.Technique)
	}

	// Execute response actions for all matching rules.
	if e.actionExec != nil {
		for _, r := range matched {
			if r.Agent.Action != rule.ActionAlert {
				e.actionExec.Execute(r, evt.Fields)
			}
		}
	}
}

// severityRank returns numeric rank for severity comparison.
func severityRank(s rule.Severity) int {
	switch s {
	case rule.SeverityInfo:
		return 0
	case rule.SeverityLow:
		return 1
	case rule.SeverityMedium:
		return 2
	case rule.SeverityHigh:
		return 3
	case rule.SeverityCritical:
		return 4
	default:
		return -1
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// IOCVersion returns the current IOC store version for heartbeat reporting.
func (e *Engine) IOCVersion() string {
	return e.iocStore.Version()
}

// IOCCount returns the total number of loaded IOC entries.
func (e *Engine) IOCCount() int {
	return e.iocStore.Count()
}

// IOCMatched returns the cumulative count of IOC match events.
func (e *Engine) IOCMatched() uint64 {
	return e.iocMatched.Load()
}

// processTaskLoop listens for tasks dispatched to the "edr" channel
// and handles IOC data delivery (DataType 9100).
func (e *Engine) processTaskLoop(ctx context.Context) {
	defer e.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-e.taskCh:
			if !ok {
				return
			}
			e.handleTask(task)
		}
	}
}

// handleTask processes a single task delivered to the EDR engine.
func (e *Engine) handleTask(task *grpcProto.Task) {
	switch task.DataType {
	case iocTaskDataType:
		if err := e.iocStore.Load(task.Data); err != nil {
			e.logger.Warn("failed to load IOC data",
				zap.Error(err),
				zap.Int("data_len", len(task.Data)),
			)
		}
	default:
		e.logger.Debug("ignoring unknown EDR task",
			zap.Int32("data_type", task.DataType),
		)
	}
}

// checkIOC checks event fields against the IOC store.
// Currently only network events (DataType 3002) are checked against IP IOCs.
func (e *Engine) checkIOC(evt *event.Event) {
	if evt.DataType != event.DataTypeNetwork {
		return
	}

	remoteAddr := evt.Fields["remote_addr"]
	if remoteAddr == "" {
		return
	}

	if !e.iocStore.CheckIP(remoteAddr) {
		return
	}

	e.iocMatched.Add(1)

	evt.SetField("ioc_match", "true")
	evt.SetField("ioc_type", "ip")
	evt.SetField("ioc_value", remoteAddr)
	evt.SetField("ioc_source", "abuse.ch")

	e.logger.Warn("IOC hit: malicious IP connection detected",
		zap.String("remote_addr", remoteAddr),
		zap.String("event_type", string(evt.EventType)),
		zap.String("pid", evt.Fields["pid"]),
	)
}
