//go:build linux

package collector

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
)

// BPF event type constants — must match bpf/common.h
const (
	bpfEventProcessExec = 1
	bpfEventProcessExit = 2

	bpfEventFileOpen   = 10
	bpfEventFileRename = 11
	bpfEventFileUnlink = 12
	bpfEventFileChmod  = 13
)

// Path mode constants — must match bpf/common.h
const (
	pathModeDentryWalk = 0
	pathModeBasename   = 1
)

// pathDelimiter separates dentry components in BPF output (\xff).
const pathDelimiter = '\xff'

// processEvent mirrors struct process_event from bpf/common.h.
// Field order, sizes, and padding MUST stay in sync with the C struct.
// Verified against bpf2go-generated processProcessEvent in process_bpfel.go.
// All fields are exported because binary.Read uses reflect and cannot write unexported fields.
type processEvent struct {
	EventType   uint8
	Pad0        [3]uint8
	Pid         uint32
	Tgid        uint32
	Ppid        uint32
	Uid         uint32
	Gid         uint32
	ExitCode    int32
	Pad1        [4]byte // alignment padding for 8-byte StartTs
	StartTs     uint64
	InContainer uint8
	Pad2        [7]uint8
	Comm        [16]byte
	Filename    [256]byte
	Cmdline     [512]byte
}

// fileEvent mirrors struct file_event from bpf/common.h.
// Field order, sizes, and padding MUST stay in sync with the C struct.
// All fields exported for binary.Read (reflect).
type fileEvent struct {
	EventType   uint8
	PathMode    uint8
	Pad0        [2]uint8
	Pid         uint32
	Tgid        uint32
	Ppid        uint32
	Uid         uint32
	Gid         uint32
	Inode       uint64
	OpenFlags   uint32
	NewMode     uint32
	StartTs     uint64
	InContainer uint8
	Pad1        [7]uint8
	Comm        [16]byte
	Filepath    [256]byte
	Filepath2   [256]byte
}

// ebpfCollector implements Collector using cilium/ebpf for kernel-level event collection.
//
// Lifecycle:
//  1. newEBPFCollector() — no BPF objects loaded yet
//  2. Start() — load BPF objects, attach tracepoints/kprobes, start perf readers
//  3. Stop()  — detach all hooks, close perf readers, release BPF resources
type ebpfCollector struct {
	logger  *zap.Logger
	eventCh chan *event.Event

	// Process BPF resources (nil until Start)
	procObjs       *processObjects
	execLink       link.Link
	exitLink       link.Link
	procPerfReader *perf.Reader

	// File BPF resources (nil until Start; may remain nil if load/attach fails)
	fileObjs       *fileObjects
	fileLinks      []link.Link // independent kprobe attachments
	filePerfReader *perf.Reader

	wg sync.WaitGroup
}

func newEBPFCollector(logger *zap.Logger) (*ebpfCollector, error) {
	return &ebpfCollector{
		logger: logger,
	}, nil
}

func (c *ebpfCollector) Mode() Mode {
	return ModeEBPF
}

func (c *ebpfCollector) Capabilities() []Capability {
	return []Capability{
		CapEBPFFull,
		CapFileFull,
		CapContainerCtx,
	}
}

// Start loads BPF programs, attaches hooks, and begins reading events.
func (c *ebpfCollector) Start(ctx context.Context) (<-chan *event.Event, error) {
	c.eventCh = make(chan *event.Event, 4096)

	// ----- Process collector (required) -----
	if err := c.startProcessCollector(); err != nil {
		close(c.eventCh)
		return nil, err
	}

	// ----- File collector (optional — degraded mode if fails) -----
	if err := c.startFileCollector(); err != nil {
		c.logger.Warn("file collector failed to start, continuing without file events",
			zap.Error(err),
		)
	}

	// Start reader goroutines
	c.wg.Add(1)
	go c.readProcessEvents(ctx)

	if c.filePerfReader != nil {
		c.wg.Add(1)
		go c.readFileEvents(ctx)
	}

	// Close eventCh when all readers finish
	go func() {
		c.wg.Wait()
		close(c.eventCh)
	}()

	c.logger.Info("eBPF collector started",
		zap.Bool("process_events", true),
		zap.Bool("file_events", c.filePerfReader != nil),
		zap.Int("file_kprobes_attached", len(c.fileLinks)),
	)

	return c.eventCh, nil
}

// startProcessCollector loads process BPF objects and attaches raw tracepoints.
func (c *ebpfCollector) startProcessCollector() error {
	objs := &processObjects{}
	if err := loadProcessObjects(objs, &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogLevel: ebpf.LogLevelInstruction,
		},
	}); err != nil {
		return fmt.Errorf("load process BPF objects: %w", err)
	}
	c.procObjs = objs

	execLink, err := link.AttachRawTracepoint(link.RawTracepointOptions{
		Name:    "sched_process_exec",
		Program: objs.TracepointSchedProcessExec,
	})
	if err != nil {
		objs.Close()
		return fmt.Errorf("attach sched_process_exec: %w", err)
	}
	c.execLink = execLink

	exitLink, err := link.AttachRawTracepoint(link.RawTracepointOptions{
		Name:    "sched_process_exit",
		Program: objs.TracepointSchedProcessExit,
	})
	if err != nil {
		execLink.Close()
		objs.Close()
		return fmt.Errorf("attach sched_process_exit: %w", err)
	}
	c.exitLink = exitLink

	reader, err := perf.NewReader(objs.Events, 16*4096)
	if err != nil {
		exitLink.Close()
		execLink.Close()
		objs.Close()
		return fmt.Errorf("create process perf reader: %w", err)
	}
	c.procPerfReader = reader

	return nil
}

// startFileCollector loads file BPF objects and attaches kprobes independently.
// Each kprobe attach is independent — one failure does not block others.
func (c *ebpfCollector) startFileCollector() error {
	objs := &fileObjects{}
	if err := loadFileObjects(objs, &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogLevel: ebpf.LogLevelInstruction,
		},
	}); err != nil {
		return fmt.Errorf("load file BPF objects: %w", err)
	}
	c.fileObjs = objs

	// Independent kprobe attach — each can fail without blocking others.
	type kprobeSpec struct {
		name    string
		program *ebpf.Program
	}
	kprobes := []kprobeSpec{
		{"security_file_open", objs.KprobeSecurityFileOpen},
		{"security_inode_rename", objs.KprobeSecurityInodeRename},
		{"security_inode_unlink", objs.KprobeSecurityInodeUnlink},
		{"security_inode_setattr", objs.KprobeSecurityInodeSetattr},
	}

	for _, kp := range kprobes {
		l, err := link.Kprobe(kp.name, kp.program, nil)
		if err != nil {
			c.logger.Warn("failed to attach file kprobe, skipping",
				zap.String("hook", kp.name),
				zap.Error(err),
			)
			continue
		}
		c.fileLinks = append(c.fileLinks, l)
		c.logger.Debug("file kprobe attached", zap.String("hook", kp.name))
	}

	if len(c.fileLinks) == 0 {
		objs.Close()
		c.fileObjs = nil
		return fmt.Errorf("no file kprobes attached")
	}

	// Open perf reader for file events.
	// Buffer: 32 pages (128 KiB). File events are ~584 bytes each.
	reader, err := perf.NewReader(objs.FileEvents, 32*4096)
	if err != nil {
		for _, l := range c.fileLinks {
			l.Close()
		}
		c.fileLinks = nil
		objs.Close()
		c.fileObjs = nil
		return fmt.Errorf("create file perf reader: %w", err)
	}
	c.filePerfReader = reader

	return nil
}

// Stop detaches all BPF programs and releases resources.
func (c *ebpfCollector) Stop() error {
	// Close perf readers first — this unblocks reader goroutines.
	if c.filePerfReader != nil {
		c.filePerfReader.Close()
	}
	if c.procPerfReader != nil {
		c.procPerfReader.Close()
	}

	// Wait for all reader goroutines to finish.
	c.wg.Wait()

	// Detach file kprobes.
	for _, l := range c.fileLinks {
		l.Close()
	}

	// Release file BPF objects.
	if c.fileObjs != nil {
		c.fileObjs.Close()
	}

	// Detach process tracepoints.
	if c.exitLink != nil {
		c.exitLink.Close()
	}
	if c.execLink != nil {
		c.execLink.Close()
	}

	// Release process BPF objects.
	if c.procObjs != nil {
		c.procObjs.Close()
	}

	c.logger.Info("eBPF collector resources released")
	return nil
}

// ----- Process event reader -----

// readProcessEvents reads from the process perf buffer and decodes events.
func (c *ebpfCollector) readProcessEvents(ctx context.Context) {
	defer c.wg.Done()

	for {
		record, err := c.procPerfReader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				return
			}
			c.logger.Warn("process perf reader error", zap.Error(err))
			continue
		}

		if record.LostSamples > 0 {
			c.logger.Warn("process perf buffer lost samples",
				zap.Uint64("lost", record.LostSamples),
			)
			continue
		}

		evt, err := c.decodeProcessEvent(record.RawSample)
		if err != nil {
			c.logger.Debug("failed to decode process event", zap.Error(err))
			continue
		}

		c.sendEvent(ctx, evt)
	}
}

// decodeProcessEvent parses a raw BPF perf event sample into an event.Event.
func (c *ebpfCollector) decodeProcessEvent(raw []byte) (*event.Event, error) {
	var pe processEvent
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &pe); err != nil {
		return nil, fmt.Errorf("binary decode: %w", err)
	}

	comm := cString(pe.Comm[:])
	filename := cString(pe.Filename[:])
	cmdline := cmdlineString(pe.Cmdline[:])

	switch pe.EventType {
	case bpfEventProcessExec:
		evt := event.NewProcessExec(
			int(pe.Tgid),
			int(pe.Ppid),
			filename,
			cmdline,
		)
		evt.SetField("uid", fmt.Sprintf("%d", pe.Uid))
		evt.SetField("gid", fmt.Sprintf("%d", pe.Gid))
		evt.SetField("comm", comm)
		evt.SetField("ktime_ns", fmt.Sprintf("%d", pe.StartTs))
		if pe.InContainer == 1 {
			evt.SetField("in_container", "true")
		}
		return evt, nil

	case bpfEventProcessExit:
		evt := event.NewProcessExit(
			int(pe.Tgid),
			int(pe.ExitCode),
		)
		evt.SetField("ppid", fmt.Sprintf("%d", pe.Ppid))
		evt.SetField("uid", fmt.Sprintf("%d", pe.Uid))
		evt.SetField("comm", comm)
		evt.SetField("ktime_ns", fmt.Sprintf("%d", pe.StartTs))
		if pe.InContainer == 1 {
			evt.SetField("in_container", "true")
		}
		return evt, nil

	default:
		return nil, fmt.Errorf("unknown process event type: %d", pe.EventType)
	}
}

// ----- File event reader -----

// readFileEvents reads from the file perf buffer and decodes events.
func (c *ebpfCollector) readFileEvents(ctx context.Context) {
	defer c.wg.Done()

	for {
		record, err := c.filePerfReader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				return
			}
			c.logger.Warn("file perf reader error", zap.Error(err))
			continue
		}

		if record.LostSamples > 0 {
			c.logger.Warn("file perf buffer lost samples",
				zap.Uint64("lost", record.LostSamples),
			)
			continue
		}

		evt, err := c.decodeFileEvent(record.RawSample)
		if err != nil {
			c.logger.Debug("failed to decode file event", zap.Error(err))
			continue
		}

		c.sendEvent(ctx, evt)
	}
}

// decodeFileEvent parses a raw BPF file event sample into an event.Event.
func (c *ebpfCollector) decodeFileEvent(raw []byte) (*event.Event, error) {
	var fe fileEvent
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &fe); err != nil {
		return nil, fmt.Errorf("binary decode: %w", err)
	}

	filePath := dentryPathString(fe.Filepath[:])
	comm := cString(fe.Comm[:])

	var evtType event.EventType
	switch fe.EventType {
	case bpfEventFileOpen:
		evtType = event.FileOpen
	case bpfEventFileRename:
		evtType = event.FileRename
	case bpfEventFileUnlink:
		evtType = event.FileUnlink
	case bpfEventFileChmod:
		evtType = event.FileChmod
	default:
		return nil, fmt.Errorf("unknown file event type: %d", fe.EventType)
	}

	evt := event.NewFileEvent(evtType, int(fe.Tgid), filePath)
	evt.SetField("ppid", fmt.Sprintf("%d", fe.Ppid))
	evt.SetField("uid", fmt.Sprintf("%d", fe.Uid))
	evt.SetField("gid", fmt.Sprintf("%d", fe.Gid))
	evt.SetField("inode", fmt.Sprintf("%d", fe.Inode))
	evt.SetField("comm", comm)
	evt.SetField("ktime_ns", fmt.Sprintf("%d", fe.StartTs))

	if fe.InContainer == 1 {
		evt.SetField("in_container", "true")
	}

	// Path mode: dentry_walk gives partial path, basename gives only filename
	if fe.PathMode == pathModeBasename {
		evt.SetField("path_mode", "basename")
	}

	// Event-specific fields
	switch fe.EventType {
	case bpfEventFileOpen:
		evt.SetField("open_flags", fmt.Sprintf("0x%x", fe.OpenFlags))
	case bpfEventFileRename:
		newPath := dentryPathString(fe.Filepath2[:])
		evt.SetField("new_path", newPath)
	case bpfEventFileChmod:
		evt.SetField("new_mode", fmt.Sprintf("%04o", fe.NewMode))
	}

	return evt, nil
}

// ----- Shared helpers -----

// sendEvent sends an event to the channel, non-blocking. Drops on full channel.
func (c *ebpfCollector) sendEvent(ctx context.Context, evt *event.Event) {
	select {
	case c.eventCh <- evt:
	case <-ctx.Done():
	default:
		c.logger.Warn("event channel full, dropping event",
			zap.String("event_type", string(evt.EventType)),
		)
	}
}

// dentrySlotSize must match DENTRY_SLOT_SIZE in bpf/common.h.
const dentrySlotSize = 32

// dentryPathString converts BPF fixed-slot dentry walk output to a filesystem path.
//
// BPF output layout: 8 slots of 32 bytes each (256 bytes total).
//
//	slot[i] = [NUL-terminated name, up to 31 bytes][\xff at byte 31]
//	slot[0] = filename, slot[1] = parent, slot[2] = grandparent, ...
//	Empty slot (name[0] == NUL) marks the end.
//
// Result: "/grandparent/parent/filename" (reversed, '/' joined)
func dentryPathString(b []byte) string {
	// Read NUL-terminated name from each fixed slot
	var parts []string
	for i := 0; i+dentrySlotSize <= len(b); i += dentrySlotSize {
		slot := b[i : i+dentrySlotSize]
		name := cString(slot[:dentrySlotSize-1]) // first 31 bytes, NUL-terminated
		if name == "" {
			break
		}
		parts = append(parts, name)
	}

	if len(parts) == 0 {
		return ""
	}

	// Reverse components (BPF writes child→parent, we want parent→child)
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}

	var sb strings.Builder
	for _, p := range parts {
		sb.WriteByte('/')
		sb.WriteString(p)
	}
	return sb.String()
}

// cString extracts a NUL-terminated string from a byte slice.
func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

// cmdlineString converts a NUL-separated cmdline byte slice into a space-separated string.
// Kernel cmdline args are stored as: "arg0\0arg1\0arg2\0"
func cmdlineString(b []byte) string {
	end := len(b)
	if i := bytes.IndexByte(b, 0); i >= 0 {
		for end > 0 && b[end-1] == 0 {
			end--
		}
	}
	if end == 0 {
		return ""
	}
	result := make([]byte, end)
	copy(result, b[:end])
	for i := range result {
		if result[i] == 0 {
			result[i] = ' '
		}
	}
	return strings.TrimSpace(string(result))
}

// String implements fmt.Stringer for logging.
func (c *ebpfCollector) String() string {
	return fmt.Sprintf("ebpf-collector(mode=%s)", c.Mode())
}
