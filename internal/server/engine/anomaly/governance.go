package anomaly

import (
	"strings"
	"sync"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/matrixplusio/mxcwpp/internal/server/model"
)

// 误报治理：绝对下限 + 证据分级 + 抑制缓存 + 反馈闭环。
// 与 EDR(celengine 白名单/保真分级)、BDE(baseline tuning 反馈)对齐，补齐 ML 异常引擎此前的裸奔状态。

// metricFloor 各指标"算作 elevated"的绝对下限（对齐 MetricNames 索引）。
//
// 根因：correlation 只看 值/主机均值 的比值，空闲主机基线趋近 0（如 net_connect 均值 0.06），
// 做 1 个连接比值就 16 倍爆表 → 基础设施偶发流量误报洪水。加绝对下限后，
// 指标绝对值本身不够高就不计入 elevated，从源头掐掉"除以极小基线"的假阳性。
// 计数类给有意义的量级；比率类(net_external_ratio/dns_nx_ratio)给 0.3。
var metricFloor = [featureCount]float64{
	10,  // 0 proc_exec_count
	5,   // 1 proc_unique_exe
	5,   // 2 proc_fork_rate
	20,  // 3 file_write_count
	10,  // 4 file_unique_path
	3,   // 5 file_sensitive_hits（敏感，下限低）
	20,  // 6 net_connect_count
	10,  // 7 net_unique_ip
	10,  // 8 net_unique_port
	0.3, // 9 net_external_ratio（比率）
	20,  // 10 dns_query_count
	10,  // 11 dns_unique_domain
	0.3, // 12 dns_nx_ratio（比率）
}

// severityRank 用于取严重度上限（数值大=更严重）。
var severityRank = map[string]int{"low": 0, "medium": 1, "high": 2, "critical": 3}

func capSeverity(band, ceiling string) string {
	if severityRank[band] > severityRank[ceiling] {
		return ceiling
	}
	return band
}

// correlationSeverity 按证据强度给 correlation 告警定级，不再一律 pattern 硬编码。
// 覆盖率(命中指标占比) + 平均比值 决定档位，且以 pattern 声明的 severity 为上限。
// → 直接消除 c2_beacon 一律 critical 的假 critical 洪水。
func correlationSeverity(ceiling string, elevated []model.ElevatedMetric, total int) string {
	if total == 0 || len(elevated) == 0 {
		return "low"
	}
	coverage := float64(len(elevated)) / float64(total)
	var sum float64
	for _, e := range elevated {
		sum += e.Ratio
	}
	avg := sum / float64(len(elevated))

	band := "medium"
	switch {
	case coverage >= 0.99 && avg >= 8:
		band = "critical"
	case coverage >= 0.8 && avg >= 5:
		band = "high"
	}
	return capSeverity(band, ceiling)
}

// forestSeverity 重校准 IForest 严重度，并封顶 high。
// IForest 是舰队级粗粒度联合离群信号，与更成熟的 BDE 行为引擎同源重叠（P2 收敛），
// 单独不足以判 critical；作为补充信号封顶 high，避免与 BDE 冗余刷 critical。
func forestSeverity(score float64) string {
	if score >= 0.85 {
		return "high"
	}
	return "medium"
}

// suppressCache 周期性刷新的抑制集合：DB 白名单主机 + 反馈自动抑制的 (host|pattern)。
// 热路径零 DB 查询（对齐 celengine 白名单 reload 模式）。
type suppressCache struct {
	mu       sync.RWMutex
	hosts    map[string]struct{} // alert_whitelists 里配置的豁免主机
	autoSupp map[string]struct{} // 反馈闭环：被反复标 false_positive 的 host|pattern
}

func newSuppressCache() *suppressCache {
	return &suppressCache{hosts: map[string]struct{}{}, autoSupp: map[string]struct{}{}}
}

// autoSuppressFPThreshold 同 (host,pattern) 被标 false_positive 达此次数 → 自动抑制后续告警。
const autoSuppressFPThreshold = 3

// reload 从 DB 重建抑制集合。db=nil 空转。
func (s *suppressCache) reload(db *gorm.DB, logger *zap.Logger) {
	if db == nil {
		return
	}
	// 1. 运维配置的白名单主机（复用 alert_whitelists.host_id）。
	var hostRows []string
	if err := db.Model(&model.AlertWhitelist{}).
		Where("host_id <> ''").Distinct().Pluck("host_id", &hostRows).Error; err != nil {
		logger.Warn("anomaly 抑制：加载白名单主机失败", zap.Error(err))
	}
	hosts := make(map[string]struct{}, len(hostRows))
	for _, h := range hostRows {
		hosts[h] = struct{}{}
	}

	// 2. 反馈闭环：analyst 反复标 false_positive 的 (host,pattern) 自动抑制。
	//    处置结果不再是死数据 —— 对齐 BDE reloadTuning 的反馈精神。
	type fpRow struct {
		HostID      string `gorm:"column:host_id"`
		PatternName string `gorm:"column:pattern_name"`
		AlertType   string `gorm:"column:alert_type"`
		C           int64
	}
	var fps []fpRow
	if err := db.Model(&model.AnomalyAlert{}).
		Select("host_id, pattern_name, alert_type, count(*) as c").
		Where("status = ?", "false_positive").
		Group("host_id, pattern_name, alert_type").
		Having("count(*) >= ?", autoSuppressFPThreshold).
		Scan(&fps).Error; err != nil {
		logger.Warn("anomaly 抑制：加载反馈 false_positive 失败", zap.Error(err))
	}
	auto := make(map[string]struct{}, len(fps))
	for _, f := range fps {
		auto[suppressKey(f.HostID, f.AlertType, f.PatternName)] = struct{}{}
	}

	s.mu.Lock()
	s.hosts = hosts
	s.autoSupp = auto
	s.mu.Unlock()
	logger.Debug("anomaly 抑制集合已刷新",
		zap.Int("whitelist_hosts", len(hosts)), zap.Int("auto_suppressed", len(auto)))
}

func suppressKey(hostID, alertType, pattern string) string {
	return hostID + "|" + alertType + "|" + pattern
}

// suppressed 判断该告警是否应被抑制（白名单主机 或 反馈自动抑制）。
func (s *suppressCache) suppressed(hostID, alertType, pattern string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.hosts[hostID]; ok {
		return true
	}
	_, ok := s.autoSupp[suppressKey(hostID, alertType, pattern)]
	return ok
}

// isInfraHostname 识别典型基础设施主机名（CDN/ZK/大数据/消息队列/数据库），
// 这类主机有合法周期性流量，是 correlation 误报大户，命中则降级不消音（仍入库可查）。
func isInfraHostname(hostname string) bool {
	h := strings.ToLower(hostname)
	for _, kw := range []string{"cdn", "zookeeper", "-zk-", "kafka", "rocketmq", "bigdata", "cdh", "hadoop", "datasvr", "-db-", "mysql", "redis", "mongo", "clickhouse"} {
		if strings.Contains(h, kw) {
			return true
		}
	}
	return false
}

// downgradeForInfra 基础设施主机的告警降一级（不消音，保留内网横向可见性）。
func downgradeForInfra(sev string) string {
	switch sev {
	case "critical":
		return "high"
	case "high":
		return "medium"
	case "medium":
		return "low"
	default:
		return sev
	}
}
