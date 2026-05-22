package celengine

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/manager/biz"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// notifyThrottleWindow 通知节流窗口：同一告警在此时间内不重复发送通知
const notifyThrottleWindow = 30 * time.Minute

// AlertGenerator 负责将 CEL 引擎匹配结果写入 alerts 表（去重模式）
type AlertGenerator struct {
	db  *gorm.DB
	log *zap.Logger
}

// NewAlertGenerator 创建 AlertGenerator
func NewAlertGenerator(db *gorm.DB, logger *zap.Logger) *AlertGenerator {
	return &AlertGenerator{
		db:  db,
		log: logger,
	}
}

// Generate 根据匹配的规则和事件字段生成或更新告警
// 去重策略：同一规则 + 同一主机合并为一条告警，累加 HitCount
func (g *AlertGenerator) Generate(hostID string, matchedRules []model.DetectionRule, fields map[string]string) {
	for _, rule := range matchedRules {
		if err := g.upsertAlert(hostID, &rule, fields); err != nil {
			g.log.Error("CEL 检测告警 upsert 失败",
				zap.Uint("rule_id", rule.ID),
				zap.String("rule_name", rule.Name),
				zap.String("host_id", hostID),
				zap.Error(err),
			)
		}
	}
}

// upsertAlert 查找已有告警并更新，不存在则创建
// ResultID = cel-{ruleID}-{hostID}（固定，不含 timestamp）
func (g *AlertGenerator) upsertAlert(hostID string, rule *model.DetectionRule, fields map[string]string) error {
	detail, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("序列化告警详情失败: %w", err)
	}

	resultID := fmt.Sprintf("cel-%d-%s", rule.ID, hostID)
	now := model.ToLocalTime(time.Now())

	// 尝试查找已有告警
	var existing model.Alert
	err = g.db.Where("result_id = ?", resultID).First(&existing).Error

	if err == nil {
		// 已存在 → 更新 LastSeenAt, HitCount++, 更新最新 Actual
		updates := map[string]any{
			"last_seen_at": now,
			"hit_count":    gorm.Expr("hit_count + 1"),
			"actual":       string(detail),
		}

		// 如果已解决/忽略，重新激活
		if existing.Status != model.AlertStatusActive {
			updates["status"] = model.AlertStatusActive
			g.log.Info("CEL 告警重新激活",
				zap.String("result_id", resultID),
				zap.String("prev_status", string(existing.Status)),
			)
		}

		if err := g.db.Model(&existing).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新告警失败: %w", err)
		}

		// 节流通知：距上次通知超过窗口才再次发送
		if g.shouldNotify(&existing) {
			g.db.Model(&existing).Updates(map[string]any{
				"last_notified_at": now,
				"notify_count":     gorm.Expr("notify_count + 1"),
			})
			g.sendNotification(&existing)
		}

		return nil
	}

	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询告警失败: %w", err)
	}

	// 不存在 → 创建新告警
	alert := model.Alert{
		ResultID:    resultID,
		HostID:      hostID,
		RuleID:      fmt.Sprintf("cel-%d", rule.ID),
		Source:      model.AlertSourceDetection,
		Severity:    rule.Severity,
		Category:    categorize(rule),
		Title:       rule.Name,
		Description: rule.Description,
		Actual:      string(detail),
		Status:      model.AlertStatusActive,
		HitCount:    1,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}

	if err := g.db.Create(&alert).Error; err != nil {
		return fmt.Errorf("写入 alerts 表失败: %w", err)
	}

	g.log.Info("CEL 检测告警生成",
		zap.Uint("rule_id", rule.ID),
		zap.String("rule_name", rule.Name),
		zap.String("host_id", hostID),
		zap.String("severity", rule.Severity),
	)

	g.sendNotification(&alert)

	return nil
}

// shouldNotify 判断是否应发送通知（节流）
func (g *AlertGenerator) shouldNotify(alert *model.Alert) bool {
	if alert.LastNotifiedAt == nil {
		return true
	}
	return time.Since(alert.LastNotifiedAt.Time()) > notifyThrottleWindow
}

// sendNotification 异步发送检测告警通知
func (g *AlertGenerator) sendNotification(alert *model.Alert) {
	go func(a *model.Alert) {
		var host model.Host
		hostname, ip := "", ""
		if g.db.Select("hostname, ipv4").First(&host, "host_id = ?", a.HostID).Error == nil {
			hostname = host.Hostname
			if len(host.IPv4) > 0 {
				ip = host.IPv4[0]
			}
		}
		ns := biz.NewNotificationService(g.db, g.log)
		if err := ns.SendDetectionAlertNotification(&biz.DetectionAlertData{
			HostID:      a.HostID,
			Hostname:    hostname,
			IP:          ip,
			RuleName:    a.Title,
			Severity:    a.Severity,
			Category:    a.Category,
			Description: a.Description,
			DetectedAt:  a.FirstSeenAt.Time(),
		}); err != nil {
			g.log.Error("发送检测告警通知失败", zap.Error(err))
		}
	}(alert)
}

// categorize 根据规则信息确定告警分类
func categorize(rule *model.DetectionRule) string {
	if rule.Category != "" {
		return rule.Category
	}
	if rule.MitreID != "" {
		return "mitre:" + rule.MitreID
	}
	return "cel-detection"
}
