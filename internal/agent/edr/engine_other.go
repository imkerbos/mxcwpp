//go:build !linux

// Stub for non-Linux platforms. The EDR engine requires Linux kernel features
// (eBPF, /proc, cn_proc, fanotify). On non-Linux, NewEngine returns an error
// and the Agent continues without EDR (graceful degradation in main.go).
package edr

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/agent/transport"
)

// Engine is a stub on non-Linux platforms.
type Engine struct{}

// NewEngine returns an error on non-Linux platforms.
func NewEngine(_ *zap.Logger, _ *transport.Manager) (*Engine, error) {
	return nil, fmt.Errorf("EDR engine requires Linux")
}

// Start is a no-op stub.
func (e *Engine) Start(_ context.Context) error { return nil }

// Stop is a no-op stub.
func (e *Engine) Stop() error { return nil }

// Stats is a no-op stub.
func (e *Engine) Stats() (forwarded, dropped uint64) { return 0, 0 }

// DegradationLevel is a no-op stub.
func (e *Engine) DegradationLevel() int32 { return 0 }

// GetEDRMode is a no-op stub.
func (e *Engine) GetEDRMode() string { return "" }

// GetEDRCapabilities is a no-op stub.
func (e *Engine) GetEDRCapabilities() []string { return nil }

// GetEDRHookType is a no-op stub.
func (e *Engine) GetEDRHookType() string { return "" }

// GetEDRStats is a no-op stub.
func (e *Engine) GetEDRStats() (forwarded, dropped uint64) { return 0, 0 }
