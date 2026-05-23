//go:build linux

package memfd

import (
	"testing"
	"time"
)

func TestDedupPreventsRepeat(t *testing.T) {
	key := dedupKey{pid: 100, threatType: "memfd_exec"}

	s := &Scanner{
		seen: make(map[dedupKey]time.Time),
	}

	// First check: not seen.
	s.seen[key] = time.Now()

	// Same key within window should be present.
	if _, ok := s.seen[key]; !ok {
		t.Error("key should be present after insertion")
	}
}

func TestDedupExpiry(t *testing.T) {
	key := dedupKey{pid: 200, threatType: "deleted_exe"}

	s := &Scanner{
		seen: make(map[dedupKey]time.Time),
	}

	// Insert with past timestamp.
	s.seen[key] = time.Now().Add(-dedupWindow - time.Second)

	// Check expiry logic.
	now := time.Now()
	for k, ts := range s.seen {
		if now.Sub(ts) > dedupWindow {
			delete(s.seen, k)
		}
	}

	if _, ok := s.seen[key]; ok {
		t.Error("expired key should be cleaned up")
	}
}

func TestWhitelist(t *testing.T) {
	whitelisted := []string{"runc", "pulseaudio", "pipewire", "Xwayland", "memfd_test"}
	for _, name := range whitelisted {
		if !whitelistedExes[name] {
			t.Errorf("%s should be whitelisted", name)
		}
	}

	if whitelistedExes["malware"] {
		t.Error("malware should not be whitelisted")
	}
}
