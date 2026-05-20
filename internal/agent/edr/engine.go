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
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/agent/edr/collector"
	"github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
	"github.com/imkerbos/mxsec-platform/internal/agent/transport"
)

// Engine is the core EDR engine that manages the event collection pipeline.
type Engine struct {
	logger    *zap.Logger
	transport *transport.Manager
	collector collector.Collector
	wg        sync.WaitGroup

	// Pipeline counters for monitoring and heartbeat reporting.
	eventsForwarded atomic.Uint64
	eventsDropped   atomic.Uint64
}

// NewEngine creates a new EDR engine.
// It auto-detects the best collector mode (eBPF or userspace) for the running kernel.
func NewEngine(logger *zap.Logger, transportMgr *transport.Manager) (*Engine, error) {
	coll, err := collector.DetectAndCreate(logger)
	if err != nil {
		return nil, err
	}

	logger.Info("EDR engine initialized",
		zap.String("collector_mode", string(coll.Mode())),
		zap.Any("capabilities", coll.Capabilities()),
	)

	return &Engine{
		logger:    logger,
		transport: transportMgr,
		collector: coll,
	}, nil
}

// Start begins event collection and forwarding.
// It blocks until the context is cancelled.
func (e *Engine) Start(ctx context.Context) error {
	eventCh, err := e.collector.Start(ctx)
	if err != nil {
		return err
	}

	e.wg.Add(1)
	go e.forwardEvents(ctx, eventCh)

	e.logger.Info("EDR engine started")
	return nil
}

// Stop gracefully shuts down the EDR engine.
func (e *Engine) Stop() error {
	err := e.collector.Stop()
	e.wg.Wait()
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

// forwardEvents reads events from the collector channel, converts them to
// bridge.Record, and sends them through the transport layer to the Server.
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
