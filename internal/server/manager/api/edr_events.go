package api

import (
	"context"
	"fmt"
	"strconv"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// EDREventsHandler EDR 事件查询处理器（数据源：ClickHouse ebpf_events）
type EDREventsHandler struct {
	chConn chdriver.Conn
	logger *zap.Logger
}

// NewEDREventsHandler 创建 EDR 事件处理器
// chConn 为 nil 时返回空数据（EDR 事件仅存储在 ClickHouse）
func NewEDREventsHandler(logger *zap.Logger, chConn chdriver.Conn) *EDREventsHandler {
	return &EDREventsHandler{chConn: chConn, logger: logger}
}

// chEDREvent ClickHouse ebpf_events 行映射
type chEDREvent struct {
	Timestamp  time.Time `json:"timestamp"`
	HostID     string    `json:"host_id"`
	Hostname   string    `json:"hostname"`
	EventType  string    `json:"event_type"`
	DataType   int32     `json:"data_type"`
	PID        string    `json:"pid"`
	PPID       string    `json:"ppid"`
	Exe        string    `json:"exe"`
	Cmdline    string    `json:"cmdline"`
	ParentExe  string    `json:"parent_exe"`
	FilePath   string    `json:"file_path"`
	RemoteAddr string    `json:"remote_addr"`
	RemotePort string    `json:"remote_port"`
	LocalAddr  string    `json:"local_addr"`
	LocalPort  string    `json:"local_port"`
	Protocol   string    `json:"protocol"`
	UID        string    `json:"uid"`
	GID        string    `json:"gid"`
	ReturnCode string    `json:"return_code"`
}

// ListEDREvents 获取 EDR 事件列表
// GET /api/v1/edr/events
func (h *EDREventsHandler) ListEDREvents(c *gin.Context) {
	if h.chConn == nil {
		SuccessPaginated(c, 0, []chEDREvent{})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// 构建 WHERE 子句
	where := "1=1"
	args := []interface{}{}

	if hostID := c.Query("host_id"); hostID != "" {
		where += " AND host_id = ?"
		args = append(args, hostID)
	}
	if hostname := c.Query("hostname"); hostname != "" {
		where += " AND hostname LIKE ?"
		args = append(args, "%"+hostname+"%")
	}
	if eventType := c.Query("event_type"); eventType != "" {
		where += " AND event_type = ?"
		args = append(args, eventType)
	}
	if dataType := c.Query("data_type"); dataType != "" {
		dt, err := strconv.Atoi(dataType)
		if err == nil {
			where += " AND data_type = ?"
			args = append(args, int32(dt))
		}
	}
	if exe := c.Query("exe"); exe != "" {
		where += " AND exe LIKE ?"
		args = append(args, "%"+exe+"%")
	}
	if cmdline := c.Query("cmdline"); cmdline != "" {
		where += " AND cmdline LIKE ?"
		args = append(args, "%"+cmdline+"%")
	}
	if filePath := c.Query("file_path"); filePath != "" {
		where += " AND file_path LIKE ?"
		args = append(args, "%"+filePath+"%")
	}
	if remoteAddr := c.Query("remote_addr"); remoteAddr != "" {
		where += " AND remote_addr = ?"
		args = append(args, remoteAddr)
	}
	if pid := c.Query("pid"); pid != "" {
		where += " AND pid = ?"
		args = append(args, pid)
	}
	// 通用关键词搜索（exe/cmdline/file_path）
	if keyword := c.Query("keyword"); keyword != "" {
		where += " AND (exe LIKE ? OR cmdline LIKE ? OR file_path LIKE ?)"
		kw := "%" + keyword + "%"
		args = append(args, kw, kw, kw)
	}
	if dateFrom := c.Query("date_from"); dateFrom != "" {
		where += " AND timestamp >= ?"
		args = append(args, dateFrom)
	}
	if dateTo := c.Query("date_to"); dateTo != "" {
		where += " AND timestamp <= ?"
		args = append(args, dateTo+" 23:59:59")
	}

	// 查总数
	countSQL := fmt.Sprintf("SELECT count() FROM ebpf_events WHERE %s", where)
	var total uint64
	if err := h.chConn.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		h.logger.Error("ClickHouse 查询 EDR 事件总数失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 查数据
	offset := (page - 1) * pageSize
	dataSQL := fmt.Sprintf(`
		SELECT timestamp, host_id, hostname, event_type, data_type,
		       pid, ppid, exe, cmdline, parent_exe,
		       file_path, remote_addr, remote_port, local_addr, local_port,
		       protocol, uid, gid, return_code
		FROM ebpf_events
		WHERE %s
		ORDER BY timestamp DESC
		LIMIT %d OFFSET %d`, where, pageSize, offset)

	rows, err := h.chConn.Query(ctx, dataSQL, args...)
	if err != nil {
		h.logger.Error("ClickHouse 查询 EDR 事件列表失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}
	defer rows.Close()

	events := make([]chEDREvent, 0, pageSize)
	for rows.Next() {
		var ev chEDREvent
		if err := rows.Scan(
			&ev.Timestamp, &ev.HostID, &ev.Hostname, &ev.EventType, &ev.DataType,
			&ev.PID, &ev.PPID, &ev.Exe, &ev.Cmdline, &ev.ParentExe,
			&ev.FilePath, &ev.RemoteAddr, &ev.RemotePort, &ev.LocalAddr, &ev.LocalPort,
			&ev.Protocol, &ev.UID, &ev.GID, &ev.ReturnCode,
		); err != nil {
			continue
		}
		events = append(events, ev)
	}

	SuccessPaginated(c, int64(total), events)
}

// EDREventStats EDR 事件统计
type EDREventStats struct {
	Total uint64 `json:"total"`
	// 按事件类型统计
	ProcessExec    uint64 `json:"process_exec"`
	FileOpen       uint64 `json:"file_open"`
	NetworkConnect uint64 `json:"network_connect"`
	// 按 DataType 统计
	ByDataType map[int32]uint64 `json:"by_data_type"`
	// Top 10 主机
	TopHosts []EDRHostEventCount `json:"top_hosts"`
	// Top 10 可执行文件
	TopExes []EDRExeCount `json:"top_exes"`
	// 趋势（按小时）
	Trend []EDREventTrendPoint `json:"trend"`
}

// EDRHostEventCount 主机事件数
type EDRHostEventCount struct {
	HostID   string `json:"host_id"`
	Hostname string `json:"hostname"`
	Count    uint64 `json:"count"`
}

// EDRExeCount 可执行文件事件数
type EDRExeCount struct {
	Exe   string `json:"exe"`
	Count uint64 `json:"count"`
}

// EDREventTrendPoint 趋势数据点
type EDREventTrendPoint struct {
	Time  string `json:"time"`
	Count uint64 `json:"count"`
}

// GetEDREventStats 获取 EDR 事件统计
// GET /api/v1/edr/events/stats
func (h *EDREventsHandler) GetEDREventStats(c *gin.Context) {
	if h.chConn == nil {
		Success(c, EDREventStats{
			ByDataType: map[int32]uint64{},
			TopHosts:   []EDRHostEventCount{},
			TopExes:    []EDRExeCount{},
			Trend:      []EDREventTrendPoint{},
		})
		return
	}

	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	if hours < 1 || hours > 720 {
		hours = 24
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	stats := EDREventStats{
		ByDataType: make(map[int32]uint64),
	}

	// 1. 总数 + 按事件类型统计
	row := h.chConn.QueryRow(ctx, `
		SELECT
			count()                                    AS total,
			countIf(event_type = 'process_exec')       AS process_exec,
			countIf(event_type = 'file_open')          AS file_open,
			countIf(event_type = 'tcp_connect' OR event_type = 'udp_send') AS network_connect
		FROM ebpf_events
		WHERE timestamp >= subtractHours(now(), ?)`, hours)
	if err := row.Scan(&stats.Total, &stats.ProcessExec, &stats.FileOpen, &stats.NetworkConnect); err != nil {
		h.logger.Error("ClickHouse EDR 统计查询失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 2. 按 DataType 统计
	dtRows, err := h.chConn.Query(ctx, `
		SELECT data_type, count() AS cnt
		FROM ebpf_events
		WHERE timestamp >= subtractHours(now(), ?)
		GROUP BY data_type`, hours)
	if err == nil {
		defer dtRows.Close()
		for dtRows.Next() {
			var dt int32
			var cnt uint64
			if scanErr := dtRows.Scan(&dt, &cnt); scanErr == nil {
				stats.ByDataType[dt] = cnt
			}
		}
	}

	// 3. Top 10 主机
	hostRows, err := h.chConn.Query(ctx, `
		SELECT host_id, hostname, count() AS cnt
		FROM ebpf_events
		WHERE timestamp >= subtractHours(now(), ?)
		GROUP BY host_id, hostname
		ORDER BY cnt DESC
		LIMIT 10`, hours)
	if err == nil {
		defer hostRows.Close()
		for hostRows.Next() {
			var hc EDRHostEventCount
			if scanErr := hostRows.Scan(&hc.HostID, &hc.Hostname, &hc.Count); scanErr == nil {
				stats.TopHosts = append(stats.TopHosts, hc)
			}
		}
	}
	if stats.TopHosts == nil {
		stats.TopHosts = []EDRHostEventCount{}
	}

	// 4. Top 10 可执行文件
	exeRows, err := h.chConn.Query(ctx, `
		SELECT exe, count() AS cnt
		FROM ebpf_events
		WHERE timestamp >= subtractHours(now(), ?) AND exe != ''
		GROUP BY exe
		ORDER BY cnt DESC
		LIMIT 10`, hours)
	if err == nil {
		defer exeRows.Close()
		for exeRows.Next() {
			var ec EDRExeCount
			if scanErr := exeRows.Scan(&ec.Exe, &ec.Count); scanErr == nil {
				stats.TopExes = append(stats.TopExes, ec)
			}
		}
	}
	if stats.TopExes == nil {
		stats.TopExes = []EDRExeCount{}
	}

	// 5. 趋势（按小时）
	trendRows, err := h.chConn.Query(ctx, `
		SELECT toString(toStartOfHour(timestamp)) AS hour, count() AS cnt
		FROM ebpf_events
		WHERE timestamp >= subtractHours(now(), ?)
		GROUP BY hour
		ORDER BY hour ASC`, hours)
	if err == nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var tp EDREventTrendPoint
			if scanErr := trendRows.Scan(&tp.Time, &tp.Count); scanErr == nil {
				stats.Trend = append(stats.Trend, tp)
			}
		}
	}
	if stats.Trend == nil {
		stats.Trend = []EDREventTrendPoint{}
	}

	Success(c, stats)
}
