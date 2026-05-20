//go:build linux

package collector

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
)

// userspaceCollector implements Collector using userspace APIs for old kernels (< 5.4).
//   - Process events: cn_proc (netlink PROC_EVENT)
//   - File events: fanotify (FAN_OPEN_PERM, FAN_CLOSE_WRITE)
//   - Network events: /proc/net/tcp + /proc/net/udp polling (5s interval)
//
// Phase 1: skeleton only — actual implementations will be added in step 1.6.
type userspaceCollector struct {
	logger  *zap.Logger
	eventCh chan *event.Event
}

func newUserspaceCollector(logger *zap.Logger) (*userspaceCollector, error) {
	return &userspaceCollector{
		logger: logger,
	}, nil
}

func (c *userspaceCollector) Mode() Mode {
	return ModeUserspace
}

func (c *userspaceCollector) Capabilities() []Capability {
	// Userspace mode has reduced capabilities compared to eBPF
	return []Capability{}
}

func (c *userspaceCollector) Start(ctx context.Context) (<-chan *event.Event, error) {
	c.eventCh = make(chan *event.Event, 4096)

	c.logger.Info("userspace collector started (skeleton — no probes attached yet)")

	// TODO (Phase 1.6):
	//   - cn_proc netlink for process events
	//   - fanotify for file events
	//   - /proc/net polling goroutine for network events

	go func() {
		<-ctx.Done()
		close(c.eventCh)
		c.logger.Info("userspace collector stopped")
	}()

	return c.eventCh, nil
}

func (c *userspaceCollector) Stop() error {
	// TODO: close netlink socket, fanotify fd, stop polling goroutines
	c.logger.Info("userspace collector resources released")
	return nil
}

// String implements fmt.Stringer for logging.
func (c *userspaceCollector) String() string {
	return fmt.Sprintf("userspace-collector(mode=%s)", c.Mode())
}
