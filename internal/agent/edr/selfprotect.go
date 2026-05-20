//go:build linux

package edr

import (
	"context"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SelfProtect implements agent self-protection mechanisms:
//   - systemd sd_notify integration (READY + WATCHDOG heartbeat)
//   - chattr +i file immutability for critical directories
type SelfProtect struct {
	logger     *zap.Logger
	notifyConn net.Conn
}

// NewSelfProtect creates a new self-protection manager.
func NewSelfProtect(logger *zap.Logger) *SelfProtect {
	return &SelfProtect{
		logger: logger,
	}
}

// Start initializes self-protection: sd_notify READY + watchdog loop + chattr.
func (sp *SelfProtect) Start(ctx context.Context, wg *sync.WaitGroup) {
	// 1. sd_notify READY=1
	sp.sdNotify("READY=1")

	// 2. Apply file immutability to critical paths.
	sp.applyImmutable()

	// 3. Start watchdog heartbeat goroutine.
	wg.Add(1)
	go sp.watchdogLoop(ctx, wg)
}

// Stop cleans up self-protection resources.
func (sp *SelfProtect) Stop() {
	sp.sdNotify("STOPPING=1")
	if sp.notifyConn != nil {
		_ = sp.notifyConn.Close()
	}
}

// TemporaryUnlock temporarily removes immutable flag for file operations
// (e.g., rule updates, agent upgrades). Caller must call the returned
// function to re-apply immutability.
func (sp *SelfProtect) TemporaryUnlock(path string) func() {
	sp.removeImmutable(path)
	return func() {
		sp.setImmutable(path)
	}
}

// sdNotify sends a notification to systemd via the NOTIFY_SOCKET.
// No-op if not running under systemd (NOTIFY_SOCKET not set).
func (sp *SelfProtect) sdNotify(state string) {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return
	}

	conn, err := net.Dial("unixgram", socketPath)
	if err != nil {
		sp.logger.Debug("sd_notify dial failed (not running under systemd)",
			zap.Error(err),
		)
		return
	}

	_, err = conn.Write([]byte(state))
	if err != nil {
		sp.logger.Warn("sd_notify write failed",
			zap.String("state", state),
			zap.Error(err),
		)
	}

	// Keep connection for WATCHDOG=1 heartbeats.
	if state == "READY=1" {
		sp.notifyConn = conn
	} else {
		_ = conn.Close()
	}
}

// watchdogLoop sends WATCHDOG=1 to systemd at half the watchdog interval.
// systemd restarts the agent if it stops receiving heartbeats.
func (sp *SelfProtect) watchdogLoop(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	// WatchdogSec is configured in the systemd unit file (e.g., 60s).
	// We send heartbeats at half that interval.
	interval := 30 * time.Second

	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		sp.logger.Debug("no NOTIFY_SOCKET, watchdog loop skipped")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sp.sdNotifyDirect(socketPath, "WATCHDOG=1")
		}
	}
}

// sdNotifyDirect sends to the notify socket directly (for watchdog loop).
func (sp *SelfProtect) sdNotifyDirect(socketPath, state string) {
	conn, err := net.Dial("unixgram", socketPath)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}

// protectedDirs are directories that get chattr +i protection.
var protectedDirs = []string{
	"/var/lib/mxsec/rules",
	"/usr/local/mxsec",
}

// applyImmutable sets the immutable attribute on protected directories.
func (sp *SelfProtect) applyImmutable() {
	for _, dir := range protectedDirs {
		sp.setImmutable(dir)
	}
}

// setImmutable applies chattr +i to a path.
func (sp *SelfProtect) setImmutable(path string) {
	if _, err := os.Stat(path); err != nil {
		return // Path doesn't exist yet, skip.
	}

	cmd := exec.Command("chattr", "+i", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		sp.logger.Debug("chattr +i failed (may lack capability)",
			zap.String("path", path),
			zap.String("output", string(output)),
			zap.Error(err),
		)
	} else {
		sp.logger.Info("file protection applied",
			zap.String("path", path),
		)
	}
}

// removeImmutable removes chattr +i from a path.
func (sp *SelfProtect) removeImmutable(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}

	cmd := exec.Command("chattr", "-i", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		sp.logger.Warn("chattr -i failed",
			zap.String("path", path),
			zap.String("output", string(output)),
			zap.Error(err),
		)
	}
}

// GenerateSystemdUnit returns a recommended systemd unit file content
// for the MxSec Agent with self-protection features.
func GenerateSystemdUnit() string {
	return `[Unit]
Description=MxSec Security Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=/usr/local/mxsec/mxsec-agent
Restart=on-failure
RestartSec=3s
StartLimitBurst=5
StartLimitIntervalSec=3600
WatchdogSec=60
NotifyAccess=main
LimitNOFILE=65536
LimitMEMLOCK=infinity

[Install]
WantedBy=multi-user.target
`
}
