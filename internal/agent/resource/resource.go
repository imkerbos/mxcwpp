// Package resource 提供资源监控功能（CPU、内存、磁盘、网络）
package resource

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// Monitor 是资源监控器
type Monitor struct {
	logger     *zap.Logger
	lastCPU    CPUStat
	lastNet    NetStat
	lastUpdate time.Time
}

// CPUStat 是 CPU 统计信息
type CPUStat struct {
	User   uint64
	Nice   uint64
	System uint64
	Idle   uint64
	Iowait uint64
	Total  uint64
}

// MemStat 是内存统计信息
type MemStat struct {
	Total     uint64  // 总内存（KB）
	Available uint64  // 可用内存（KB）
	Used      uint64  // 已使用内存（KB）
	Free      uint64  // 空闲内存（KB）
	Usage     float64 // 使用率（%）
}

// DiskStat 是磁盘统计信息
type DiskStat struct {
	Total     uint64  // 总容量（字节）
	Available uint64  // 可用容量（字节）
	Used      uint64  // 已使用容量（字节）
	Usage     float64 // 使用率（%）
}

// NetStat 是网络统计信息
type NetStat struct {
	BytesSent   uint64 // 发送字节数
	BytesRecv   uint64 // 接收字节数
	PacketsSent uint64 // 发送包数
	PacketsRecv uint64 // 接收包数
}

// ResourceMetrics 是资源指标
type ResourceMetrics struct {
	CPUUsage     float64 // CPU 使用率（%）
	MemUsage     float64 // 内存使用率（%）
	DiskUsage    float64 // 磁盘使用率（%）
	NetBytesSent uint64  // 网络发送字节数（累计）
	NetBytesRecv uint64  // 网络接收字节数（累计）
	Timestamp    int64   // 时间戳
}

// NewMonitor 创建新的资源监控器
func NewMonitor(logger *zap.Logger) *Monitor {
	return &Monitor{
		logger:     logger,
		lastUpdate: time.Now(),
	}
}

// Collect 采集资源指标
func (m *Monitor) Collect() (*ResourceMetrics, error) {
	metrics := &ResourceMetrics{
		Timestamp: time.Now().Unix(),
	}

	// 采集 CPU 使用率
	cpuUsage, err := m.collectCPU()
	if err != nil {
		m.logger.Warn("failed to collect CPU usage", zap.Error(err))
	} else {
		metrics.CPUUsage = cpuUsage
	}

	// 采集内存使用率
	memUsage, err := m.collectMemory()
	if err != nil {
		m.logger.Warn("failed to collect memory usage", zap.Error(err))
	} else {
		metrics.MemUsage = memUsage
	}

	// 采集磁盘使用率
	diskUsage, err := m.collectDisk()
	if err != nil {
		m.logger.Warn("failed to collect disk usage", zap.Error(err))
	} else {
		metrics.DiskUsage = diskUsage
	}

	// 采集网络统计
	netBytesSent, netBytesRecv, err := m.collectNetwork()
	if err != nil {
		m.logger.Warn("failed to collect network stats", zap.Error(err))
	} else {
		metrics.NetBytesSent = netBytesSent
		metrics.NetBytesRecv = netBytesRecv
	}

	return metrics, nil
}

// collectCPU 采集 CPU 使用率
func (m *Monitor) collectCPU() (float64, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, fmt.Errorf("failed to read /proc/stat")
	}

	line := scanner.Text()
	fields := strings.Fields(line)
	if len(fields) < 8 || fields[0] != "cpu" {
		return 0, fmt.Errorf("invalid /proc/stat format")
	}

	// 解析 CPU 统计
	var stat CPUStat
	stat.User, _ = strconv.ParseUint(fields[1], 10, 64)
	stat.Nice, _ = strconv.ParseUint(fields[2], 10, 64)
	stat.System, _ = strconv.ParseUint(fields[3], 10, 64)
	stat.Idle, _ = strconv.ParseUint(fields[4], 10, 64)
	stat.Iowait, _ = strconv.ParseUint(fields[5], 10, 64)
	stat.Total = stat.User + stat.Nice + stat.System + stat.Idle + stat.Iowait

	// 计算 CPU 使用率
	now := time.Now()
	elapsed := now.Sub(m.lastUpdate).Seconds()
	if elapsed < 1.0 {
		elapsed = 1.0 // 避免除零
	}

	if m.lastCPU.Total > 0 {
		totalDiff := stat.Total - m.lastCPU.Total
		idleDiff := stat.Idle - m.lastCPU.Idle

		if totalDiff > 0 {
			usage := 100.0 * (1.0 - float64(idleDiff)/float64(totalDiff))
			m.lastCPU = stat
			m.lastUpdate = now
			return usage, nil
		}
	}

	m.lastCPU = stat
	m.lastUpdate = now
	return 0, nil
}

// collectMemory 采集内存使用率
func (m *Monitor) collectMemory() (float64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var mem MemStat
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "MemTotal":
			mem.Total = value
		case "MemAvailable":
			mem.Available = value
		case "MemFree":
			mem.Free = value
		}
	}

	if mem.Total == 0 {
		return 0, fmt.Errorf("failed to read memory info")
	}

	if mem.Available > 0 {
		mem.Used = mem.Total - mem.Available
		mem.Usage = 100.0 * float64(mem.Used) / float64(mem.Total)
	} else {
		mem.Used = mem.Total - mem.Free
		mem.Usage = 100.0 * float64(mem.Used) / float64(mem.Total)
	}

	return mem.Usage, nil
}

// collectDisk 采集磁盘使用率（根分区）
func (m *Monitor) collectDisk() (float64, error) {
	// 使用 statfs 系统调用获取根分区信息
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, err
	}

	total := stat.Blocks * uint64(stat.Bsize)
	available := stat.Bavail * uint64(stat.Bsize)
	used := total - available

	usage := 100.0 * float64(used) / float64(total)
	return usage, nil
}

// collectNetwork 采集网络统计（累计值）
func (m *Monitor) collectNetwork() (uint64, uint64, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	var bytesSent, bytesRecv uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// 跳过标题行
		if strings.Contains(line, "bytes") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		// 跳过回环接口
		iface := strings.TrimSuffix(fields[0], ":")
		if strings.HasPrefix(iface, "lo") {
			continue
		}

		// 字段顺序：interface | bytes_recv | packets_recv | ... | bytes_sent | packets_sent | ...
		recv, _ := strconv.ParseUint(fields[1], 10, 64)
		sent, _ := strconv.ParseUint(fields[9], 10, 64)

		bytesRecv += recv
		bytesSent += sent
	}

	// 更新累计值
	m.lastNet.BytesSent = bytesSent
	m.lastNet.BytesRecv = bytesRecv

	return bytesSent, bytesRecv, nil
}
