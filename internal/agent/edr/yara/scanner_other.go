//go:build !linux

package yara

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
)

// Result holds a single YARA match result (stub).
type Result struct {
	FilePath   string
	RuleName   string
	ThreatType string
	Severity   string
	Tags       []string
}

// Scanner is a no-op stub on non-Linux platforms.
type Scanner struct{}

// NewScanner returns nil on non-Linux (YARA scanning disabled).
func NewScanner(_ *zap.Logger, _ chan<- *event.Event) *Scanner {
	return nil
}

// Start is a no-op stub.
func (s *Scanner) Start(_ context.Context, _ *sync.WaitGroup) {}

// ShouldScan always returns false on non-Linux.
func (s *Scanner) ShouldScan(_ *event.Event) bool { return false }

// Enqueue is a no-op stub.
func (s *Scanner) Enqueue(_ string, _ map[string]string) {}

// Stats returns zero counters.
func (s *Scanner) Stats() (total, matched uint64) { return 0, 0 }
