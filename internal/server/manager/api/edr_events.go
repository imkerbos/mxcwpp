package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/server/metrics"
)

// EDR 查询端到端超时策略：
//
//   - Go 端 context 超时:  edrQueryCtxTimeout = 60s  (HTTP handler 上限)
//   - CH 端 max_execution_time: 50s                  (比 Go 端早 10s 停，让 CH 主动报错而非 Go 端取消)
//   - 慢查询告警阈值:        3s                       (>3s 走 Warn 日志 + Prom 慢查询桶)
//
// 商业 EDR 列表查询正常路径在 Projection 命中后 < 200ms；保留 60s 上限是为大窗口/复杂过滤兜底，
// 避免一刀切 10s 把合法慢查询误杀。最终失败仍要明确返回 500 + 引导用户缩窄时间范围。
const (
	edrQueryCtxTimeout = 60 * time.Second
	edrCHMaxExec       = 50 // 秒
	edrSlowQueryThresh = 3 * time.Second
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

// chQueryCtx 给 ClickHouse 查询附加 max_execution_time 兜底超时。
// 让 CH 在 Go ctx 超时前主动停止，能区分 "CH 慢查询" vs "Go ctx 取消"。
func chQueryCtx(parent context.Context) context.Context {
	return clickhouse.Context(parent, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": edrCHMaxExec,
	}))
}

// recordCHQuery 记录 ClickHouse 查询延迟到 Prom + 慢查询告警日志。
func (h *EDREventsHandler) recordCHQuery(op, table string, start time.Time, err error) {
	dur := time.Since(start)
	status := "ok"
	if err != nil {
		status = "error"
		// CH max_execution_time / Go ctx deadline 都归为 timeout
		msg := err.Error()
		if strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "max_execution_time") || strings.Contains(msg, "TIMEOUT_EXCEEDED") {
			status = "timeout"
		}
	}
	metrics.RecordCHQueryDuration(op, table, status, dur.Seconds())
	if dur >= edrSlowQueryThresh {
		h.logger.Warn("ClickHouse 慢查询",
			zap.String("op", op),
			zap.String("table", table),
			zap.String("status", status),
			zap.Duration("duration", dur),
		)
	}
}

// normalizeDateBound 把日期字符串规整为 ClickHouse 可比较的 DateTime 字符串。
// 输入含时分秒（":"）则原样使用；否则按 upper=true 补 23:59:59，upper=false 补 00:00:00。
// 避免前端只传日期时，下界丢失 24h（"2026-06-03" 解析为 00:00:00 没问题；上界则要补 23:59:59）。
func normalizeDateBound(s string, upper bool) string {
	if strings.Contains(s, ":") {
		return s
	}
	if upper {
		return s + " 23:59:59"
	}
	return s + " 00:00:00"
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), edrQueryCtxTimeout)
	defer cancel()
	chCtx := chQueryCtx(ctx)

	// 构建 WHERE 子句
	//
	// 默认时间窗：date_from / date_to 都未传时，自动加 last 24h。
	// ebpf_events 表 PARTITION BY YYYYMMDD(timestamp)，无时间过滤会扫全部 part →
	// 配合 proj_time_desc projection 才能保证 ORDER BY timestamp DESC LIMIT N 走主键反向扫描。
	where := "1=1"
	args := []interface{}{}

	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	if dateFrom == "" && dateTo == "" {
		// 默认 24h：用精确时分秒，让 CH 命中 partition 裁剪 + projection 反向主键
		dateFrom = time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	}

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
	if dateFrom != "" {
		where += " AND timestamp >= ?"
		args = append(args, normalizeDateBound(dateFrom, false))
	}
	if dateTo != "" {
		where += " AND timestamp <= ?"
		args = append(args, normalizeDateBound(dateTo, true))
	}

	// 查总数（ClickHouse MergeTree count() 是 metadata 操作，通常 < 100ms）
	countSQL := fmt.Sprintf("SELECT count() FROM ebpf_events WHERE %s", where)
	var total uint64
	{
		start := time.Now()
		err := h.chConn.QueryRow(chCtx, countSQL, args...).Scan(&total)
		h.recordCHQuery("list_count", "ebpf_events", start, err)
		if err != nil {
			h.logger.Error("ClickHouse 查询 EDR 事件总数失败", zap.Error(err))
			InternalError(c, "查询失败：数据量过大或时间窗口过宽，请缩窄过滤条件")
			return
		}
	}

	// 查数据：依赖 ebpf_events.proj_time_desc projection 让 ORDER BY timestamp DESC 走主键反向。
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

	start := time.Now()
	rows, err := h.chConn.Query(chCtx, dataSQL, args...)
	if err != nil {
		h.recordCHQuery("list_data", "ebpf_events", start, err)
		h.logger.Error("ClickHouse 查询 EDR 事件列表失败", zap.Error(err))
		InternalError(c, "查询失败：数据量过大或时间窗口过宽，请缩窄过滤条件")
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
			h.logger.Warn("ClickHouse 单行扫描失败，跳过", zap.Error(err))
			continue
		}
		events = append(events, ev)
	}
	// 关键：rows.Next() 在 ctx 超时 / CH 错误时静默返回 false，必须用 rows.Err() 判定真实结果。
	if err := rows.Err(); err != nil {
		h.recordCHQuery("list_data", "ebpf_events", start, err)
		h.logger.Error("ClickHouse rows 迭代失败", zap.Error(err))
		InternalError(c, "查询失败：数据量过大或时间窗口过宽，请缩窄过滤条件")
		return
	}
	h.recordCHQuery("list_data", "ebpf_events", start, nil)

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

	ctx, cancel := context.WithTimeout(c.Request.Context(), edrQueryCtxTimeout)
	defer cancel()
	chCtx := chQueryCtx(ctx)

	stats := EDREventStats{
		ByDataType: make(map[int32]uint64),
	}

	// 1. 总数 + 按事件类型统计
	{
		start := time.Now()
		row := h.chConn.QueryRow(chCtx, `
			SELECT
				count()                                    AS total,
				countIf(event_type = 'process_exec')       AS process_exec,
				countIf(event_type = 'file_open')          AS file_open,
				countIf(event_type = 'tcp_connect' OR event_type = 'udp_send') AS network_connect
			FROM ebpf_events
			WHERE timestamp >= subtractHours(now(), ?)`, hours)
		err := row.Scan(&stats.Total, &stats.ProcessExec, &stats.FileOpen, &stats.NetworkConnect)
		h.recordCHQuery("stats_total", "ebpf_events", start, err)
		if err != nil {
			h.logger.Error("ClickHouse EDR 统计查询失败", zap.Error(err))
			InternalError(c, "查询失败")
			return
		}
	}

	// 2. 按 DataType 统计
	{
		start := time.Now()
		dtRows, err := h.chConn.Query(chCtx, `
			SELECT data_type, count() AS cnt
			FROM ebpf_events
			WHERE timestamp >= subtractHours(now(), ?)
			GROUP BY data_type`, hours)
		if err == nil {
			for dtRows.Next() {
				var dt int32
				var cnt uint64
				if scanErr := dtRows.Scan(&dt, &cnt); scanErr == nil {
					stats.ByDataType[dt] = cnt
				}
			}
			err = dtRows.Err()
			dtRows.Close()
		}
		h.recordCHQuery("stats_by_data_type", "ebpf_events", start, err)
	}

	// 3. Top 10 主机
	{
		start := time.Now()
		hostRows, err := h.chConn.Query(chCtx, `
			SELECT host_id, hostname, count() AS cnt
			FROM ebpf_events
			WHERE timestamp >= subtractHours(now(), ?)
			GROUP BY host_id, hostname
			ORDER BY cnt DESC
			LIMIT 10`, hours)
		if err == nil {
			for hostRows.Next() {
				var hc EDRHostEventCount
				if scanErr := hostRows.Scan(&hc.HostID, &hc.Hostname, &hc.Count); scanErr == nil {
					stats.TopHosts = append(stats.TopHosts, hc)
				}
			}
			err = hostRows.Err()
			hostRows.Close()
		}
		h.recordCHQuery("stats_top_hosts", "ebpf_events", start, err)
	}
	if stats.TopHosts == nil {
		stats.TopHosts = []EDRHostEventCount{}
	}

	// 4. Top 10 可执行文件
	{
		start := time.Now()
		exeRows, err := h.chConn.Query(chCtx, `
			SELECT exe, count() AS cnt
			FROM ebpf_events
			WHERE timestamp >= subtractHours(now(), ?) AND exe != ''
			GROUP BY exe
			ORDER BY cnt DESC
			LIMIT 10`, hours)
		if err == nil {
			for exeRows.Next() {
				var ec EDRExeCount
				if scanErr := exeRows.Scan(&ec.Exe, &ec.Count); scanErr == nil {
					stats.TopExes = append(stats.TopExes, ec)
				}
			}
			err = exeRows.Err()
			exeRows.Close()
		}
		h.recordCHQuery("stats_top_exes", "ebpf_events", start, err)
	}
	if stats.TopExes == nil {
		stats.TopExes = []EDRExeCount{}
	}

	// 5. 趋势（按小时）
	{
		start := time.Now()
		trendRows, err := h.chConn.Query(chCtx, `
			SELECT toString(toStartOfHour(timestamp)) AS hour, count() AS cnt
			FROM ebpf_events
			WHERE timestamp >= subtractHours(now(), ?)
			GROUP BY hour
			ORDER BY hour ASC`, hours)
		if err == nil {
			for trendRows.Next() {
				var tp EDREventTrendPoint
				if scanErr := trendRows.Scan(&tp.Time, &tp.Count); scanErr == nil {
					stats.Trend = append(stats.Trend, tp)
				}
			}
			err = trendRows.Err()
			trendRows.Close()
		}
		h.recordCHQuery("stats_trend", "ebpf_events", start, err)
	}
	if stats.Trend == nil {
		stats.Trend = []EDREventTrendPoint{}
	}

	Success(c, stats)
}
