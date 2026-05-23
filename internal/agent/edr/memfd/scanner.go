//go:build linux

// Package memfd detects fileless attack techniques by scanning /proc for:
//   - memfd_create: processes with memfd-backed file descriptors
//   - deleted executables: processes running from deleted ELF files
//   - anonymous executable memory: suspicious rwx memory mappings
package memfd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
)

const (
	// scanInterval controls how often the /proc scan runs.
	scanInterval = 30 * time.Second

	// dedupWindow prevents duplicate alerts for the same PID + threat type.
	dedupWindow = 10 * time.Minute
)

// whitelistedExes are processes that legitimately use memfd or deleted exe patterns.
var whitelistedExes = map[string]bool{
	"runc":       true, // container runtime uses memfd for seccomp
	"memfd_test": true, // kernel self-test
	"pulseaudio": true, // uses memfd for shared memory
	"pipewire":   true, // same as pulseaudio
	"Xwayland":   true, // uses memfd for buffer sharing
}

type dedupKey struct {
	pid        int
	threatType string
}

// Scanner detects fileless threats by scanning /proc.
type Scanner struct {
	logger  *zap.Logger
	eventCh chan<- *event.Event

	mu   sync.Mutex
	seen map[dedupKey]time.Time // dedup map

	// Counters.
	scansTotal   atomic.Uint64
	threatsFound atomic.Uint64
}

// NewScanner creates a memory threat scanner.
func NewScanner(logger *zap.Logger, eventCh chan<- *event.Event) *Scanner {
	return &Scanner{
		logger:  logger,
		eventCh: eventCh,
		seen:    make(map[dedupKey]time.Time),
	}
}

// Start launches the periodic scan goroutine.
func (s *Scanner) Start(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(2)
	go s.scanLoop(ctx, wg)
	go s.cleanupLoop(ctx, wg)
	s.logger.Info("memory threat scanner started", zap.Duration("interval", scanInterval))
}

// Stats returns scan counters.
func (s *Scanner) Stats() (scans, threats uint64) {
	return s.scansTotal.Load(), s.threatsFound.Load()
}

func (s *Scanner) scanLoop(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	// Initial scan after short delay to let process table stabilize.
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}
	s.scan()

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

func (s *Scanner) cleanupLoop(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(dedupWindow)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for k, t := range s.seen {
				if now.Sub(t) > dedupWindow {
					delete(s.seen, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

// scan performs a single pass over /proc.
func (s *Scanner) scan() {
	s.scansTotal.Add(1)

	entries, err := os.ReadDir("/proc")
	if err != nil {
		s.logger.Warn("failed to read /proc", zap.Error(err))
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 2 {
			continue // skip non-PID dirs and kernel threads
		}

		s.checkProcess(pid)
	}
}

// checkProcess inspects a single process for memory threats.
func (s *Scanner) checkProcess(pid int) {
	procDir := fmt.Sprintf("/proc/%d", pid)

	// Read exe link first — needed for whitelisting and event fields.
	exeLink, err := os.Readlink(procDir + "/exe")
	if err != nil {
		return // process likely exited
	}

	// Skip self.
	if isSelf(pid) {
		return
	}

	baseName := filepath.Base(strings.TrimSuffix(exeLink, " (deleted)"))
	if whitelistedExes[baseName] {
		return
	}

	// Check 1: deleted executable.
	if strings.HasSuffix(exeLink, " (deleted)") {
		s.emitIfNew(pid, "deleted_exe", exeLink, baseName, procDir)
	}

	// Check 2: memfd-backed file descriptors.
	s.checkMemfd(pid, baseName, procDir)

	// Check 3: anonymous executable memory regions (suspicious rwx).
	s.checkAnonExec(pid, baseName, procDir)
}

// checkMemfd scans /proc/[pid]/fd for memfd: links.
func (s *Scanner) checkMemfd(pid int, baseName, procDir string) {
	fdDir := procDir + "/fd"
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return
	}

	for _, fd := range fds {
		target, err := os.Readlink(fdDir + "/" + fd.Name())
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, "/memfd:") {
			s.emitIfNew(pid, "memfd_exec", target, baseName, procDir)
			return // one memfd detection per PID is enough
		}
	}
}

// checkAnonExec looks for suspicious anonymous executable memory mappings.
func (s *Scanner) checkAnonExec(pid int, baseName, procDir string) {
	mapsPath := procDir + "/maps"
	data, err := os.ReadFile(mapsPath)
	if err != nil {
		return
	}

	// Count anonymous rwx regions. Normal processes have 0-1.
	// Malware with injected shellcode typically has multiple.
	var anonRWXCount int
	for line := range strings.SplitSeq(string(data), "\n") {
		if len(line) < 50 {
			continue
		}
		// Format: address perms offset dev inode pathname
		// Example: 7f1234000000-7f1234001000 rwxp 00000000 00:00 0
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		perms := fields[1]
		if len(perms) < 4 || perms[0] != 'r' || perms[1] != 'w' || perms[2] != 'x' {
			continue
		}
		// Anonymous = no pathname or [heap]/[stack] (inode == 0 and no path)
		inode := fields[4]
		hasPath := len(fields) >= 6 && fields[5] != ""
		if inode == "0" && !hasPath {
			anonRWXCount++
		}
	}

	// 3+ anonymous rwx regions is suspicious (JIT engines like Node/JVM have some).
	if anonRWXCount >= 3 {
		s.emitIfNew(pid, "anonymous_exec", fmt.Sprintf("anon_rwx_count=%d", anonRWXCount), baseName, procDir)
	}
}

// emitIfNew emits a memory threat event if not seen recently (dedup).
func (s *Scanner) emitIfNew(pid int, threatType, detail, exeName, procDir string) {
	key := dedupKey{pid: pid, threatType: threatType}
	s.mu.Lock()
	if t, ok := s.seen[key]; ok && time.Since(t) < dedupWindow {
		s.mu.Unlock()
		return
	}
	s.seen[key] = time.Now()
	s.mu.Unlock()

	s.threatsFound.Add(1)

	// Read additional process info.
	ppid, uid, cmdline := readProcStatus(procDir)

	evt := &event.Event{
		DataType:  event.DataTypeMemory,
		EventType: event.EventType(threatType),
		Timestamp: time.Now(),
		Fields: map[string]string{
			"event_type":  threatType,
			"pid":         strconv.Itoa(pid),
			"ppid":        ppid,
			"uid":         uid,
			"exe":         exeName,
			"cmdline":     cmdline,
			"threat_type": threatType,
			"detail":      detail,
		},
	}

	s.logger.Warn("memory threat detected",
		zap.String("threat_type", threatType),
		zap.Int("pid", pid),
		zap.String("exe", exeName),
		zap.String("detail", detail),
	)

	select {
	case s.eventCh <- evt:
	default:
		s.logger.Warn("event channel full, dropping memory threat event")
	}
}

// readProcStatus reads ppid, uid, and cmdline from /proc/[pid]/.
func readProcStatus(procDir string) (ppid, uid, cmdline string) {
	// Read status for ppid and uid.
	data, err := os.ReadFile(procDir + "/status")
	if err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			if val, ok := strings.CutPrefix(line, "PPid:\t"); ok {
				ppid = val
			} else if val, ok := strings.CutPrefix(line, "Uid:\t"); ok {
				parts := strings.Fields(val)
				if len(parts) > 0 {
					uid = parts[0] // real UID
				}
			}
		}
	}

	// Read cmdline.
	cmdData, err := os.ReadFile(procDir + "/cmdline")
	if err == nil && len(cmdData) > 0 {
		// cmdline is null-separated.
		cmdline = strings.ReplaceAll(string(cmdData), "\x00", " ")
		cmdline = strings.TrimSpace(cmdline)
		if len(cmdline) > 512 {
			cmdline = cmdline[:512]
		}
	}

	return
}

// isSelf checks if the PID belongs to this process.
func isSelf(pid int) bool {
	return pid == os.Getpid()
}
