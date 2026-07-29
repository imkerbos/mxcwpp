// Package api 态势感知大屏聚合接口。
// GetOverview 一次返回全部大屏板块数据（复用现有告警/行为/异常/漏洞/基线/主机数据）；
// GetAlertStream 以 SSE 推送新告警，供大屏实时告警流。
package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/matrixplusio/mxcwpp/internal/server/model"
)

// ScreenHandler 态势大屏聚合处理器。
type ScreenHandler struct {
	db     *gorm.DB
	ch     chdriver.Conn
	logger *zap.Logger
}

// NewScreenHandler 创建大屏处理器。
func NewScreenHandler(db *gorm.DB, ch chdriver.Conn, logger *zap.Logger) *ScreenHandler {
	return &ScreenHandler{db: db, ch: ch, logger: logger}
}

// screenTactics 大屏 ATT&CK 覆盖固定 12 战术（顺序与前端一致）。
var screenTactics = []struct{ ID, Name string }{
	{"TA0001", "初始访问"}, {"TA0002", "执行"}, {"TA0003", "持久化"}, {"TA0004", "提权"},
	{"TA0005", "防御绕过"}, {"TA0006", "凭据访问"}, {"TA0007", "发现"}, {"TA0008", "横向移动"},
	{"TA0009", "收集"}, {"TA0010", "数据渗出"}, {"TA0011", "命令控制"}, {"TA0040", "影响"},
}

// categoryLabel 告警类目 → 主机评分榜的简短问题标签。
var categoryLabel = map[string]string{
	"lateral_movement": "横向移动", "attack_chain": "攻击链", "port_scan": "端口扫描",
	"privilege_escalation": "提权", "credential_access": "凭据访问", "defense_evasion": "防御绕过",
	"persistence": "持久化", "exfiltration": "数据渗出", "execution": "执行", "ransomware": "勒索特征",
	"memory_threat": "内存马", "ioc_hit": "IOC 命中",
}

// GetOverview 返回大屏全部板块聚合数据。
// GET /api/v1/screen/overview
func (h *ScreenHandler) GetOverview(c *gin.Context) {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	since24 := now.Add(-24 * time.Hour)

	// ---------- KPI ----------
	var activeThreats, blockedToday, agentsOnline, agentsTotal, ignoredCnt, totalHandled int64
	h.db.Model(&model.Alert{}).Where("status = ?", model.AlertStatusActive).Count(&activeThreats)
	h.db.Model(&model.Alert{}).Where("status IN ? AND updated_at >= ?", []string{"ignored", "resolved"}, dayStart).Count(&blockedToday)
	h.db.Model(&model.Host{}).Where("status = ?", model.HostStatusOnline).Count(&agentsOnline)
	h.db.Model(&model.Host{}).Count(&agentsTotal)
	h.db.Model(&model.Alert{}).Where("status = ?", model.AlertStatusIgnored).Count(&ignoredCnt)
	h.db.Model(&model.Alert{}).Count(&totalHandled)
	fpRate := 0
	if totalHandled > 0 {
		fpRate = int(ignoredCnt * 100 / totalHandled)
	}

	// ---------- 告警 severity（活跃）----------
	sev := map[string]int64{"critical": 0, "high": 0, "medium": 0, "low": 0}
	var sevRows []struct {
		Severity string
		C        int64
	}
	h.db.Model(&model.Alert{}).Select("severity, count(*) as c").
		Where("status = ?", model.AlertStatusActive).Group("severity").Scan(&sevRows)
	for _, r := range sevRows {
		sev[r.Severity] = r.C
	}

	// ---------- 漏洞（未修去重 CVE）----------
	var vulnRows []struct {
		Severity string
		C        int64
	}
	h.db.Table("host_vulnerabilities hv").
		Select("v.severity as severity, count(distinct hv.vuln_id) as c").
		Joins("join vulnerabilities v on hv.vuln_id = v.id").
		Where("hv.status = ?", "unpatched").Group("v.severity").Scan(&vulnRows)
	critVuln, highVuln := int64(0), int64(0)
	for _, r := range vulnRows {
		switch r.Severity {
		case "critical":
			critVuln = r.C
		case "high":
			highVuln = r.C
		}
	}

	// ---------- 态势评分（越高越安全，简单加权后钳制）----------
	risk := sev["critical"]*6 + sev["high"]*1 + critVuln*2
	score := 100 - int(risk)
	if score < 20 {
		score = 20
	}
	if score > 99 {
		score = 99
	}

	// ---------- 各引擎 ----------
	var bdeOpen, mlCrit, kubeActive int64
	h.db.Model(&model.BehaviorAlert{}).Where("status = ?", "open").Count(&bdeOpen)
	h.db.Model(&model.AnomalyAlert{}).Where("severity = ?", "critical").Count(&mlCrit)
	h.db.Model(&model.KubeBaselineAlert{}).Where("status = ?", "active").Count(&kubeActive)
	var fimCount uint64
	if h.ch != nil {
		if row := h.ch.QueryRow(context.Background(), "SELECT count() FROM mxcwpp.fim_events WHERE timestamp >= now() - INTERVAL 24 HOUR"); row != nil {
			_ = row.Scan(&fimCount)
		}
	}
	engineStatus := func(warn bool) string {
		if warn {
			return "warn"
		}
		return "healthy"
	}
	engines := []gin.H{
		{"key": "edr", "name": "EDR 检测", "count": activeThreats, "unit": "活跃告警", "status": engineStatus(activeThreats > 500)},
		{"key": "bde", "name": "行为引擎", "count": bdeOpen, "unit": "待处置", "status": engineStatus(bdeOpen > 5000)},
		{"key": "ml", "name": "ML 异常", "count": mlCrit, "unit": "critical", "status": engineStatus(mlCrit > 1000)},
		{"key": "fim", "name": "文件完整性", "count": fimCount, "unit": "24h 事件", "status": "healthy"},
		{"key": "kube", "name": "K8s 基线", "count": kubeActive, "unit": "活跃项", "status": engineStatus(kubeActive > 100)},
		{"key": "ac", "name": "接入中心", "count": agentsOnline, "unit": "在线探针", "status": engineStatus(agentsOnline < agentsTotal)},
	}

	// ---------- 24h 趋势（12 个 2 小时桶）----------
	trend := gin.H{
		"hours": h.trendHours(since24),
		"edr":   h.bucket24h("alerts", since24),
		"bde":   h.bucket24h("behavior_alerts", since24),
		"ml":    h.bucket24h("anomaly_alerts", since24),
	}

	// ---------- 主机安全评分榜 ----------
	hostRank := h.hostRank()

	// ---------- ATT&CK 战术覆盖 ----------
	tacticCount := map[string]int64{}
	var tacticRows []struct {
		Tactic string `gorm:"column:attck_tactic"`
		C      int64
	}
	h.db.Model(&model.Alert{}).Select("attck_tactic, count(*) as c").
		Where("status = ?", model.AlertStatusActive).Group("attck_tactic").Scan(&tacticRows)
	for _, r := range tacticRows {
		tacticCount[r.Tactic] = r.C
	}
	attck := make([]gin.H, 0, len(screenTactics))
	for _, t := range screenTactics {
		attck = append(attck, gin.H{"id": t.ID, "name": t.Name, "count": tacticCount[t.ID]})
	}

	// ---------- 基线合规 ----------
	var scanPass, scanTotal int64
	h.db.Model(&model.ScanResult{}).Where("status = ?", "pass").Count(&scanPass)
	h.db.Model(&model.ScanResult{}).Count(&scanTotal)
	baselineRate := 0
	if scanTotal > 0 {
		baselineRate = int(scanPass * 100 / scanTotal)
	}

	// ---------- 实时告警流（最新 12）----------
	feed := h.latestAlerts(12)

	Success(c, gin.H{
		"kpi": gin.H{
			"blockedToday":   blockedToday,
			"activeThreats":  activeThreats,
			"agentsOnline":   agentsOnline,
			"agentsTotal":    agentsTotal,
			"fpSuppressRate": fpRate,
			"postureScore":   score,
		},
		"engines":  engines,
		"severity": gin.H{"critical": sev["critical"], "high": sev["high"], "medium": sev["medium"], "low": sev["low"]},
		"trend":    trend,
		"hostRank": hostRank,
		"attck":    attck,
		"compliance": gin.H{
			"criticalVuln": critVuln, "highVuln": highVuln,
			"baselineRate": baselineRate, "kubeBaseline": kubeActive,
		},
		"feed": feed,
	})
}

// bucket24h 将某表 created_at 近 24h 分入 12 个 2 小时桶。
func (h *ScreenHandler) bucket24h(table string, since time.Time) []int {
	out := make([]int, 12)
	var rows []struct {
		B int
		C int
	}
	sql := fmt.Sprintf("SELECT FLOOR(TIMESTAMPDIFF(HOUR, ?, created_at)/2) as b, count(*) as c FROM %s WHERE created_at >= ? GROUP BY b", table)
	h.db.Raw(sql, since, since).Scan(&rows)
	for _, r := range rows {
		if r.B >= 0 && r.B < 12 {
			out[r.B] = r.C
		}
	}
	return out
}

// trendHours 生成 12 个 2 小时刻度标签。
func (h *ScreenHandler) trendHours(since time.Time) []string {
	hours := make([]string, 12)
	for i := 0; i < 12; i++ {
		hours[i] = since.Add(time.Duration(i*2) * time.Hour).Format("15:04")
	}
	return hours
}

// hostRank 主机安全评分榜（按活跃告警加权风险，取最危险 6 台）。
func (h *ScreenHandler) hostRank() []gin.H {
	var rows []struct {
		HostID   string `gorm:"column:host_id"`
		Hostname string
		Risk     int64
	}
	h.db.Table("alerts a").
		Select("a.host_id, h.hostname, SUM(CASE a.severity WHEN 'critical' THEN 40 WHEN 'high' THEN 15 WHEN 'medium' THEN 5 ELSE 1 END) as risk").
		Joins("left join hosts h on a.host_id = h.host_id").
		Where("a.status = ?", model.AlertStatusActive).
		Group("a.host_id, h.hostname").Order("risk desc").Limit(6).Scan(&rows)
	if len(rows) == 0 {
		return []gin.H{}
	}
	// 每台主机最高频类目 → 问题标签
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.HostID)
	}
	var catRows []struct {
		HostID   string `gorm:"column:host_id"`
		Category string
		C        int64
	}
	h.db.Model(&model.Alert{}).Select("host_id, category, count(*) as c").
		Where("status = ? AND host_id IN ?", model.AlertStatusActive, ids).
		Group("host_id, category").Order("c desc").Scan(&catRows)
	topCat := map[string]string{}
	for _, cr := range catRows {
		if _, ok := topCat[cr.HostID]; !ok {
			if lbl, ok := categoryLabel[cr.Category]; ok {
				topCat[cr.HostID] = lbl
			} else {
				topCat[cr.HostID] = cr.Category
			}
		}
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		score := 100 - int(r.Risk)
		if score < 5 {
			score = 5
		}
		if score > 95 {
			score = 95
		}
		name := r.Hostname
		if name == "" {
			name = r.HostID
		}
		out = append(out, gin.H{"name": name, "score": score, "issues": topCat[r.HostID]})
	}
	return out
}

// latestAlerts 最新 n 条告警（供告警流初始 + 结构与 SSE 一致）。
func (h *ScreenHandler) latestAlerts(n int) []gin.H {
	var rows []struct {
		ID        uint
		CreatedAt time.Time
		Severity  string
		Title     string
		HostID    string `gorm:"column:host_id"`
		Hostname  string
	}
	h.db.Table("alerts a").
		Select("a.id, a.created_at, a.severity, a.title, a.host_id, h.hostname").
		Joins("left join hosts h on a.host_id = h.host_id").
		Order("a.id desc").Limit(n).Scan(&rows)
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		host := r.Hostname
		if host == "" {
			host = r.HostID
		}
		out = append(out, gin.H{
			"id":       fmt.Sprintf("%d", r.ID),
			"time":     r.CreatedAt.Format("15:04:05"),
			"severity": r.Severity,
			"title":    r.Title,
			"host":     host,
		})
	}
	return out
}

// GetAlertStream 以 SSE 推送新告警（id 大于上次）。
// GET /api/v1/screen/alerts/stream
func (h *ScreenHandler) GetAlertStream(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// 基线：当前最大 id，只推此后新增。
	var lastID uint
	h.db.Model(&model.Alert{}).Select("COALESCE(MAX(id),0)").Scan(&lastID)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	deadline := time.After(15 * time.Minute)

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-deadline:
			fmt.Fprintf(c.Writer, ": stream timeout, please reconnect\n\n")
			c.Writer.Flush()
			return
		case <-heartbeat.C:
			fmt.Fprintf(c.Writer, ": heartbeat\n\n")
			c.Writer.Flush()
		case <-ticker.C:
			var rows []struct {
				ID        uint
				CreatedAt time.Time
				Severity  string
				Title     string
				HostID    string `gorm:"column:host_id"`
				Hostname  string
			}
			h.db.Table("alerts a").
				Select("a.id, a.created_at, a.severity, a.title, a.host_id, h.hostname").
				Joins("left join hosts h on a.host_id = h.host_id").
				Where("a.id > ?", lastID).Order("a.id asc").Limit(20).Scan(&rows)
			for _, r := range rows {
				host := r.Hostname
				if host == "" {
					host = r.HostID
				}
				payload := fmt.Sprintf(
					`{"id":%q,"time":%q,"severity":%q,"title":%q,"host":%q}`,
					fmt.Sprintf("%d", r.ID), r.CreatedAt.Format("15:04:05"), r.Severity, jsonEscape(r.Title), host,
				)
				fmt.Fprintf(c.Writer, "event: alert\ndata: %s\n\n", payload)
				if f, ok := c.Writer.(http.Flusher); ok {
					f.Flush()
				}
				lastID = r.ID
			}
		}
	}
}

// jsonEscape 转义告警标题中的双引号/反斜杠，防止破坏 SSE JSON。
func jsonEscape(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n', '\r':
			out = append(out, ' ')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
