// Package biz 提供业务逻辑层
package biz

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// NotificationService 通知服务
type NotificationService struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewNotificationService 创建通知服务
func NewNotificationService(db *gorm.DB, logger *zap.Logger) *NotificationService {
	return &NotificationService{
		db:     db,
		logger: logger,
	}
}

// AlertData 告警数据（基线安全告警）
type AlertData struct {
	// 主机信息
	HostID       string
	Hostname     string
	IP           string
	OSFamily     string
	OSVersion    string
	BusinessLine string // 业务线

	// 规则信息
	RuleID        string
	RuleName      string
	Category      string
	Severity      string
	Title         string
	Description   string
	Actual        string
	Expected      string
	FixSuggestion string

	// 任务信息
	TaskID    string
	PolicyID  string
	CheckedAt time.Time

	// 前端地址（用于构建跳转链接）
	FrontendURL string
	ResultID    string // 结果ID，用于构建详情链接
}

// AgentOfflineData Agent 离线告警数据
type AgentOfflineData struct {
	HostID       string
	Hostname     string
	IP           string
	OSFamily     string
	OSVersion    string
	LastOnlineAt time.Time
	OfflineAt    time.Time
}

// AgentOnlineData Agent 上线恢复数据
type AgentOnlineData struct {
	HostID      string
	Hostname    string
	IP          string
	OSFamily    string
	OSVersion   string
	OnlineAt    time.Time
	OfflineSince time.Time // 上次离线时间
}

// AlertResolvedData 告警恢复数据
type AlertResolvedData struct {
	// 主机信息
	HostID    string
	Hostname  string
	IP        string
	OSFamily  string
	OSVersion string

	// 规则信息
	RuleID   string
	RuleName string
	Category string
	Severity string
	Title    string

	// 时间信息
	FirstSeenAt time.Time // 告警首次发现时间
	ResolvedAt  time.Time // 告警恢复时间

	// 前端地址
	FrontendURL string
	ResultID    string
}

// SendAlertNotification 发送告警通知（用于新告警）
// 注：此方法只在新告警创建时调用，已存在的告警由定期调度器处理
// 返回值：是否成功发送了至少一个通知，以及错误信息
func (s *NotificationService) SendAlertNotification(alertData *AlertData) (bool, error) {
	// 查询所有启用的、类别为 baseline_alert 的通知配置
	var notifications []model.Notification
	if err := s.db.Where("enabled = ? AND notify_category = ?", true, model.NotifyCategoryBaselineAlert).Find(&notifications).Error; err != nil {
		s.logger.Error("查询通知配置失败", zap.Error(err))
		return false, err
	}

	// 过滤出配置了 severities 的通知
	var baselineNotifications []model.Notification
	for _, n := range notifications {
		if len(n.Severities) > 0 {
			baselineNotifications = append(baselineNotifications, n)
		}
	}

	if len(baselineNotifications) == 0 {
		s.logger.Debug("没有找到配置了告警等级的基线告警通知配置")
		return false, nil
	}

	// 过滤匹配的通知配置
	matchedNotifications := s.filterNotifications(baselineNotifications, alertData)

	if len(matchedNotifications) == 0 {
		s.logger.Debug("没有匹配的通知配置",
			zap.String("host_id", alertData.HostID),
			zap.String("severity", alertData.Severity),
		)
		return false, nil
	}

	// 发送通知
	sentCount := 0
	for _, notification := range matchedNotifications {
		if err := s.sendNotification(&notification, alertData); err != nil {
			s.logger.Error("发送通知失败",
				zap.Uint("notification_id", notification.ID),
				zap.String("type", string(notification.Type)),
				zap.Error(err),
			)
			// 继续发送其他通知，不中断
		} else {
			sentCount++
		}
	}

	return sentCount > 0, nil
}

// SendAlertNotificationForAlert 为指定告警发送通知（用于定期告警调度器）
// 返回是否成功发送通知
func (s *NotificationService) SendAlertNotificationForAlert(alert *model.Alert) (bool, error) {
	// 查询主机信息
	var host model.Host
	if err := s.db.First(&host, "host_id = ?", alert.HostID).Error; err != nil {
		return false, fmt.Errorf("查询主机信息失败: %w", err)
	}

	// 查询规则信息
	var rule model.Rule
	if err := s.db.First(&rule, "rule_id = ?", alert.RuleID).Error; err != nil {
		s.logger.Warn("查询规则信息失败", zap.String("rule_id", alert.RuleID), zap.Error(err))
	}

	// 获取主机 IP
	hostIP := ""
	if len(host.IPv4) > 0 {
		hostIP = strings.Join(host.IPv4, ",")
	}

	// 构建告警数据
	alertData := &AlertData{
		HostID:        alert.HostID,
		Hostname:      host.Hostname,
		IP:            hostIP,
		OSFamily:      host.OSFamily,
		OSVersion:     host.OSVersion,
		BusinessLine:  host.BusinessLine, // 添加业务线
		RuleID:        alert.RuleID,
		RuleName:      rule.Title,
		Category:      alert.Category,
		Severity:      alert.Severity,
		Title:         alert.Title,
		Description:   rule.Description,
		Actual:        alert.Actual,
		Expected:      alert.Expected,
		FixSuggestion: alert.FixSuggestion,
		PolicyID:      alert.PolicyID,
		CheckedAt:     alert.LastSeenAt.Time(),
		ResultID:      alert.ResultID,
	}

	// 查询所有启用的、配置了 severities 的通知配置（用于基线告警）
	var notifications []model.Notification
	if err := s.db.Where("enabled = ?", true).Find(&notifications).Error; err != nil {
		return false, fmt.Errorf("查询通知配置失败: %w", err)
	}

	// 过滤出配置了 severities 的通知
	var baselineNotifications []model.Notification
	for _, n := range notifications {
		if len(n.Severities) > 0 {
			baselineNotifications = append(baselineNotifications, n)
		}
	}

	if len(baselineNotifications) == 0 {
		return false, nil
	}

	// 过滤匹配的通知配置
	matchedNotifications := s.filterNotifications(baselineNotifications, alertData)

	sent := false
	for _, notification := range matchedNotifications {
		if err := s.sendNotification(&notification, alertData); err != nil {
			s.logger.Error("发送通知失败",
				zap.Uint("notification_id", notification.ID),
				zap.Error(err),
			)
		} else {
			sent = true
		}
	}

	return sent, nil
}

// filterNotifications 过滤匹配的通知配置
func (s *NotificationService) filterNotifications(
	notifications []model.Notification,
	alertData *AlertData,
) []model.Notification {
	var matched []model.Notification

	for _, notification := range notifications {
		// 检查通知等级
		if !s.matchSeverity(notification.Severities, alertData.Severity) {
			continue
		}

		// 检查主机范围
		if !s.matchScope(&notification, alertData) {
			continue
		}

		matched = append(matched, notification)
	}

	return matched
}

// matchSeverity 检查严重级别是否匹配
func (s *NotificationService) matchSeverity(notificationSeverities []string, alertSeverity string) bool {
	for _, sev := range notificationSeverities {
		if sev == alertSeverity {
			return true
		}
	}
	return false
}

// matchScope 检查主机范围是否匹配
func (s *NotificationService) matchScope(notification *model.Notification, alertData *AlertData) bool {
	switch notification.Scope {
	case model.NotificationScopeGlobal:
		return true

	case model.NotificationScopeHostTags:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		// TODO: 需要查询主机的标签，这里暂时返回 true
		return true

	case model.NotificationScopeBusinessLine:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		// 查询主机的业务线
		var host model.Host
		if err := s.db.First(&host, "host_id = ?", alertData.HostID).Error; err != nil {
			return false
		}
		for _, bl := range scopeValue.BusinessLines {
			if host.BusinessLine == bl {
				return true
			}
		}
		return false

	case model.NotificationScopeSpecified:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		for _, hostID := range scopeValue.HostIDs {
			if hostID == alertData.HostID {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// sendNotification 发送单个通知
func (s *NotificationService) sendNotification(
	notification *model.Notification,
	alertData *AlertData,
) error {
	var message map[string]interface{}

	if notification.Type == model.NotificationTypeLark {
		// Lark 使用卡片消息
		message = s.buildLarkAlertCard(notification, alertData)
	} else {
		// Webhook 使用 JSON 格式
		message = s.buildWebhookAlert(alertData)
	}

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %v", err)
	}

	// 发送 HTTP 请求
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Post(notification.Config.WebhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return fmt.Errorf("服务器返回状态码: %d，响应: %s", resp.StatusCode, bodyStr)
	}

	return nil
}

// BuildLarkAlertCardForTest 构建 Lark 告警卡片消息（用于测试，公开方法）
func (s *NotificationService) BuildLarkAlertCardForTest(
	notification *model.Notification,
	alertData *AlertData,
) map[string]interface{} {
	return s.buildLarkAlertCard(notification, alertData)
}

// buildLarkAlertCard 构建 Lark 告警卡片消息（参考 Elkeid 模板）
func (s *NotificationService) buildLarkAlertCard(
	notification *model.Notification,
	alertData *AlertData,
) map[string]interface{} {
	// 构建业务线显示文本
	businessLineText := alertData.BusinessLine
	if businessLineText == "" {
		businessLineText = "未设置"
	}

	// 构建告警描述（包含 IP 和业务线）
	description := fmt.Sprintf(
		"矩阵云安全平台检测到您的资产存在疑似【%s】基线风险，请及时处理。\n\n"+
			"**主机名称：** %s\n"+
			"**主机 IP：** %s\n"+
			"**业务线：** %s\n"+
			"**告警时间：** %s",
		alertData.RuleName,
		alertData.Hostname,
		alertData.IP,
		businessLineText,
		alertData.CheckedAt.Format("2006-01-02 15:04:05"),
	)

	// 构建原始数据（参考 Elkeid 格式）
	rawData := map[string]interface{}{
		"alert_type":     "基线安全告警",
		"hostname":       alertData.Hostname,
		"host_id":        alertData.HostID,
		"ip":             alertData.IP,
		"business_line":  alertData.BusinessLine,
		"os":             alertData.OSFamily + " " + alertData.OSVersion,
		"rule_id":        alertData.RuleID,
		"rule_name":      alertData.RuleName,
		"category":       alertData.Category,
		"severity":       alertData.Severity,
		"actual":         alertData.Actual,
		"expected":       alertData.Expected,
		"fix_suggestion": alertData.FixSuggestion,
		"time":           alertData.CheckedAt.Format(time.RFC3339),
	}

	// 构建原始数据文本（按固定顺序显示）
	rawDataLines := []string{
		fmt.Sprintf(`"alert_type": "%v"`, rawData["alert_type"]),
		fmt.Sprintf(`"hostname": "%v"`, rawData["hostname"]),
		fmt.Sprintf(`"host_id": "%v"`, rawData["host_id"]),
		fmt.Sprintf(`"ip": "%v"`, rawData["ip"]),
		fmt.Sprintf(`"business_line": "%v"`, rawData["business_line"]),
		fmt.Sprintf(`"os": "%v"`, rawData["os"]),
		fmt.Sprintf(`"rule_id": "%v"`, rawData["rule_id"]),
		fmt.Sprintf(`"rule_name": "%v"`, rawData["rule_name"]),
		fmt.Sprintf(`"category": "%v"`, rawData["category"]),
		fmt.Sprintf(`"severity": "%v"`, rawData["severity"]),
		fmt.Sprintf(`"actual": "%v"`, rawData["actual"]),
		fmt.Sprintf(`"expected": "%v"`, rawData["expected"]),
		fmt.Sprintf(`"fix_suggestion": "%v"`, rawData["fix_suggestion"]),
		fmt.Sprintf(`"time": "%v"`, rawData["time"]),
	}
	rawDataText := "原始数据如下:\n" + strings.Join(rawDataLines, "\n")

	// 构建跳转 URL
	alertURL := ""
	if notification.FrontendURL != "" {
		// 构建告警详情页面的 URL
		// 格式：前端地址 + /alerts/{alert_id} 或 /alerts?result_id={result_id}
		resultID := alertData.ResultID
		if resultID == "" {
			resultID = alertData.RuleID // 如果没有 result_id，使用 rule_id
		}
		// 优先使用 result_id 查询告警，如果没有则使用 host_id 和 rule_id
		alertURL = fmt.Sprintf("%s/alerts?result_id=%s",
			strings.TrimSuffix(notification.FrontendURL, "/"),
			resultID,
		)
	}

	// 构建卡片元素
	elements := []map[string]interface{}{
		{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": description,
			},
		},
		{
			"tag": "hr",
		},
		{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": rawDataText,
			},
		},
	}

	// 如果有前端地址，添加跳转按钮
	if alertURL != "" {
		elements = append(elements, map[string]interface{}{
			"tag": "action",
			"actions": []map[string]interface{}{
				{
					"tag": "button",
					"text": map[string]interface{}{
						"tag":     "plain_text",
						"content": "查看详情",
					},
					"type": "primary",
					"multi_url": map[string]interface{}{
						"url":         alertURL,
						"android_url": alertURL,
						"ios_url":     alertURL,
						"pc_url":      alertURL,
					},
				},
			},
		})
	}

	// 构建卡片消息
	card := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": "矩阵云安全平台告警通知",
			},
			"template": s.getSeverityTemplate(alertData.Severity), // 根据严重级别选择模板颜色
		},
		"elements": elements,
	}

	message := map[string]interface{}{
		"msg_type": "interactive",
		"card":     card,
	}

	// Lark 需要签名
	if notification.Config.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		sign, err := s.generateLarkSign(notification.Config.Secret, timestamp)
		if err == nil {
			message["timestamp"] = timestamp
			message["sign"] = sign
		}
	}

	return message
}

// buildWebhookAlert 构建 Webhook 告警消息
func (s *NotificationService) buildWebhookAlert(alertData *AlertData) map[string]interface{} {
	return map[string]interface{}{
		"alert_type":     "baseline_risk",
		"status":         "firing", // firing 或 resolved
		"host_id":        alertData.HostID,
		"hostname":       alertData.Hostname,
		"ip":             alertData.IP,
		"business_line":  alertData.BusinessLine, // 业务线
		"os_family":      alertData.OSFamily,
		"os_version":     alertData.OSVersion,
		"rule_id":        alertData.RuleID,
		"rule_name":      alertData.RuleName,
		"category":       alertData.Category,
		"severity":       alertData.Severity,
		"title":          alertData.Title,
		"actual":         alertData.Actual,
		"expected":       alertData.Expected,
		"fix_suggestion": alertData.FixSuggestion,
		"checked_at":     alertData.CheckedAt.Format(time.RFC3339),
		"url":            alertData.FrontendURL,
	}
}

// getSeverityTemplate 根据严重级别获取模板颜色
func (s *NotificationService) getSeverityTemplate(severity string) string {
	switch severity {
	case "critical":
		return "red" // 红色
	case "high":
		return "orange" // 橙色
	case "medium":
		return "blue" // 蓝色
	case "low":
		return "grey" // 灰色
	default:
		return "blue"
	}
}

// generateLarkSign 生成 Lark Webhook 签名
func (s *NotificationService) generateLarkSign(secret, timestamp string) (string, error) {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	_, err := mac.Write([]byte(stringToSign))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// SendAlertResolvedNotification 发送告警恢复通知
func (s *NotificationService) SendAlertResolvedNotification(resolvedData *AlertResolvedData) error {
	// 查询所有启用的、配置了 severities 的通知配置（用于基线告警）
	var notifications []model.Notification
	if err := s.db.Where("enabled = ?", true).Find(&notifications).Error; err != nil {
		s.logger.Error("查询通知配置失败", zap.Error(err))
		return err
	}

	// 过滤出配置了 severities 的通知
	var baselineNotifications []model.Notification
	for _, n := range notifications {
		if len(n.Severities) > 0 {
			baselineNotifications = append(baselineNotifications, n)
		}
	}

	if len(baselineNotifications) == 0 {
		s.logger.Debug("没有找到配置了告警等级的通知配置")
		return nil
	}

	// 过滤匹配的通知配置
	matchedNotifications := s.filterResolvedNotifications(baselineNotifications, resolvedData)

	// 发送通知
	for _, notification := range matchedNotifications {
		if err := s.sendResolvedNotification(&notification, resolvedData); err != nil {
			s.logger.Error("发送告警恢复通知失败",
				zap.Uint("notification_id", notification.ID),
				zap.String("type", string(notification.Type)),
				zap.Error(err),
			)
		}
	}

	return nil
}

// filterResolvedNotifications 过滤匹配的恢复通知配置
func (s *NotificationService) filterResolvedNotifications(
	notifications []model.Notification,
	resolvedData *AlertResolvedData,
) []model.Notification {
	var matched []model.Notification

	for _, notification := range notifications {
		// 检查严重级别是否匹配
		if !s.matchSeverity(notification.Severities, resolvedData.Severity) {
			continue
		}

		// 检查主机范围
		alertData := &AlertData{HostID: resolvedData.HostID}
		if !s.matchScope(&notification, alertData) {
			continue
		}

		matched = append(matched, notification)
	}

	return matched
}

// sendResolvedNotification 发送单个告警恢复通知
func (s *NotificationService) sendResolvedNotification(
	notification *model.Notification,
	resolvedData *AlertResolvedData,
) error {
	var message map[string]interface{}

	if notification.Type == model.NotificationTypeLark {
		message = s.buildLarkResolvedCard(notification, resolvedData)
	} else {
		message = s.buildWebhookResolved(resolvedData)
	}

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %v", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Post(notification.Config.WebhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return fmt.Errorf("服务器返回状态码: %d，响应: %s", resp.StatusCode, bodyStr)
	}

	s.logger.Info("告警恢复通知发送成功",
		zap.String("host_id", resolvedData.HostID),
		zap.String("rule_id", resolvedData.RuleID),
	)

	return nil
}

// buildLarkResolvedCard 构建 Lark 告警恢复卡片消息
func (s *NotificationService) buildLarkResolvedCard(
	notification *model.Notification,
	resolvedData *AlertResolvedData,
) map[string]interface{} {
	// 计算持续时间
	duration := resolvedData.ResolvedAt.Sub(resolvedData.FirstSeenAt)
	durationStr := formatDuration(duration)

	// 构建恢复描述
	description := fmt.Sprintf(
		"✅ **告警已恢复**\n\n"+
			"矩阵云安全平台检测到您的资产【%s】的基线风险已修复。\n\n"+
			"**规则名称：** %s\n"+
			"**风险等级：** %s\n"+
			"**首次发现：** %s\n"+
			"**恢复时间：** %s\n"+
			"**持续时长：** %s",
		resolvedData.Hostname,
		resolvedData.RuleName,
		getSeverityLabel(resolvedData.Severity),
		resolvedData.FirstSeenAt.Format("2006-01-02 15:04:05"),
		resolvedData.ResolvedAt.Format("2006-01-02 15:04:05"),
		durationStr,
	)

	// 构建卡片元素
	elements := []map[string]interface{}{
		{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": description,
			},
		},
	}

	// 构建卡片消息
	card := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": "✅ 基线告警恢复通知",
			},
			"template": "green", // 绿色表示恢复
		},
		"elements": elements,
	}

	message := map[string]interface{}{
		"msg_type": "interactive",
		"card":     card,
	}

	// Lark 需要签名
	if notification.Config.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		sign, err := s.generateLarkSign(notification.Config.Secret, timestamp)
		if err == nil {
			message["timestamp"] = timestamp
			message["sign"] = sign
		}
	}

	return message
}

// buildWebhookResolved 构建 Webhook 告警恢复消息
func (s *NotificationService) buildWebhookResolved(resolvedData *AlertResolvedData) map[string]interface{} {
	return map[string]interface{}{
		"alert_type":    "baseline_risk",
		"status":        "resolved",
		"host_id":       resolvedData.HostID,
		"hostname":      resolvedData.Hostname,
		"ip":            resolvedData.IP,
		"os_family":     resolvedData.OSFamily,
		"os_version":    resolvedData.OSVersion,
		"rule_id":       resolvedData.RuleID,
		"rule_name":     resolvedData.RuleName,
		"category":      resolvedData.Category,
		"severity":      resolvedData.Severity,
		"title":         resolvedData.Title,
		"first_seen_at": resolvedData.FirstSeenAt.Format(time.RFC3339),
		"resolved_at":   resolvedData.ResolvedAt.Format(time.RFC3339),
	}
}

// formatDuration 格式化持续时间
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d分钟", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%d小时%d分钟", hours, minutes)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%d天%d小时", days, hours)
}

// getSeverityLabel 获取严重级别标签
func getSeverityLabel(severity string) string {
	switch severity {
	case "critical":
		return "严重"
	case "high":
		return "高危"
	case "medium":
		return "中危"
	case "low":
		return "低危"
	default:
		return severity
	}
}

// SendAgentOfflineNotification 发送 Agent 离线通知
func (s *NotificationService) SendAgentOfflineNotification(offlineData *AgentOfflineData) error {
	// 查询所有启用的、类别为 agent_offline 的通知配置
	var notifications []model.Notification
	if err := s.db.Where("enabled = ? AND notify_category = ?", true, model.NotifyCategoryAgentOffline).Find(&notifications).Error; err != nil {
		s.logger.Error("查询通知配置失败", zap.Error(err))
		return err
	}

	if len(notifications) == 0 {
		s.logger.Debug("没有找到启用的 Agent 离线通知配置")
		return nil
	}

	// 过滤匹配的通知配置（检查主机范围）
	matchedNotifications := s.filterAgentOfflineNotifications(notifications, offlineData)

	// 发送通知
	for _, notification := range matchedNotifications {
		if err := s.sendAgentOfflineNotification(&notification, offlineData); err != nil {
			s.logger.Error("发送 Agent 离线通知失败",
				zap.Uint("notification_id", notification.ID),
				zap.String("type", string(notification.Type)),
				zap.Error(err),
			)
			// 继续发送其他通知，不中断
		}
	}

	return nil
}

// filterAgentOfflineNotifications 过滤匹配的 Agent 离线通知配置
func (s *NotificationService) filterAgentOfflineNotifications(
	notifications []model.Notification,
	offlineData *AgentOfflineData,
) []model.Notification {
	var matched []model.Notification

	for _, notification := range notifications {
		// 检查主机范围（Agent 离线通知不检查严重级别）
		if !s.matchAgentOfflineScope(&notification, offlineData) {
			continue
		}

		matched = append(matched, notification)
	}

	return matched
}

// matchAgentOfflineScope 检查主机范围是否匹配（Agent 离线场景）
func (s *NotificationService) matchAgentOfflineScope(notification *model.Notification, offlineData *AgentOfflineData) bool {
	switch notification.Scope {
	case model.NotificationScopeGlobal:
		return true

	case model.NotificationScopeHostTags:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		// TODO: 需要查询主机的标签，这里暂时返回 true
		return true

	case model.NotificationScopeBusinessLine:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		// 查询主机的业务线
		var host model.Host
		if err := s.db.First(&host, "host_id = ?", offlineData.HostID).Error; err != nil {
			return false
		}
		for _, bl := range scopeValue.BusinessLines {
			if host.BusinessLine == bl {
				return true
			}
		}
		return false

	case model.NotificationScopeSpecified:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		for _, hostID := range scopeValue.HostIDs {
			if hostID == offlineData.HostID {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// sendAgentOfflineNotification 发送单个 Agent 离线通知
func (s *NotificationService) sendAgentOfflineNotification(
	notification *model.Notification,
	offlineData *AgentOfflineData,
) error {
	var message map[string]interface{}

	if notification.Type == model.NotificationTypeLark {
		// Lark 使用卡片消息
		message = s.buildLarkAgentOfflineCard(notification, offlineData)
	} else {
		// Webhook 使用 JSON 格式
		message = s.buildWebhookAgentOffline(offlineData)
	}

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %v", err)
	}

	// 发送 HTTP 请求
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Post(notification.Config.WebhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return fmt.Errorf("服务器返回状态码: %d，响应: %s", resp.StatusCode, bodyStr)
	}

	s.logger.Info("Agent 离线通知发送成功",
		zap.String("host_id", offlineData.HostID),
		zap.String("hostname", offlineData.Hostname),
	)

	return nil
}

// buildLarkAgentOfflineCard 构建 Lark Agent 离线卡片消息
func (s *NotificationService) buildLarkAgentOfflineCard(
	notification *model.Notification,
	offlineData *AgentOfflineData,
) map[string]interface{} {
	// 构建告警描述
	description := fmt.Sprintf(
		"矩阵云安全平台检测到您的主机 Agent 已离线。\n\n"+
			"**主机名称：** %s\n"+
			"**主机 IP：** %s\n"+
			"**操作系统：** %s %s\n"+
			"**离线时间：** %s\n\n"+
			"请及时检查主机网络连接或 Agent 服务状态。",
		offlineData.Hostname,
		offlineData.IP,
		offlineData.OSFamily,
		offlineData.OSVersion,
		offlineData.OfflineAt.Format("2006-01-02 15:04:05"),
	)

	// 构建跳转 URL
	hostURL := ""
	if notification.FrontendURL != "" {
		hostURL = fmt.Sprintf("%s/assets/hosts?host_id=%s",
			strings.TrimSuffix(notification.FrontendURL, "/"),
			offlineData.HostID,
		)
	}

	// 构建卡片元素
	elements := []map[string]interface{}{
		{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": description,
			},
		},
	}

	// 如果有前端地址，添加跳转按钮
	if hostURL != "" {
		elements = append(elements, map[string]interface{}{
			"tag": "hr",
		})
		elements = append(elements, map[string]interface{}{
			"tag": "action",
			"actions": []map[string]interface{}{
				{
					"tag": "button",
					"text": map[string]interface{}{
						"tag":     "plain_text",
						"content": "查看主机详情",
					},
					"type": "primary",
					"multi_url": map[string]interface{}{
						"url":         hostURL,
						"android_url": hostURL,
						"ios_url":     hostURL,
						"pc_url":      hostURL,
					},
				},
			},
		})
	}

	// 构建卡片消息
	card := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": "⚠️ Agent 离线告警",
			},
			"template": "orange", // 橙色警告
		},
		"elements": elements,
	}

	message := map[string]interface{}{
		"msg_type": "interactive",
		"card":     card,
	}

	// Lark 需要签名
	if notification.Config.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		sign, err := s.generateLarkSign(notification.Config.Secret, timestamp)
		if err == nil {
			message["timestamp"] = timestamp
			message["sign"] = sign
		}
	}

	return message
}

// buildWebhookAgentOffline 构建 Webhook Agent 离线消息
func (s *NotificationService) buildWebhookAgentOffline(offlineData *AgentOfflineData) map[string]interface{} {
	return map[string]interface{}{
		"alert_type":     "agent_offline",
		"host_id":        offlineData.HostID,
		"hostname":       offlineData.Hostname,
		"ip":             offlineData.IP,
		"os_family":      offlineData.OSFamily,
		"os_version":     offlineData.OSVersion,
		"last_online_at": offlineData.LastOnlineAt.Format(time.RFC3339),
		"offline_at":     offlineData.OfflineAt.Format(time.RFC3339),
	}
}

// SendAgentOnlineNotification 发送 Agent 上线恢复通知
func (s *NotificationService) SendAgentOnlineNotification(onlineData *AgentOnlineData) error {
	// 查询所有启用的、类别为 agent_offline 的通知配置
	var notifications []model.Notification
	if err := s.db.Where("enabled = ? AND notify_category = ?", true, model.NotifyCategoryAgentOffline).Find(&notifications).Error; err != nil {
		s.logger.Error("查询通知配置失败", zap.Error(err))
		return err
	}

	if len(notifications) == 0 {
		s.logger.Debug("没有找到启用的 Agent 离线通知配置，跳过上线恢复通知")
		return nil
	}

	// 过滤匹配的通知配置（检查主机范围）
	matchedNotifications := s.filterAgentOnlineNotifications(notifications, onlineData)

	// 发送通知
	for _, notification := range matchedNotifications {
		if err := s.sendAgentOnlineNotification(&notification, onlineData); err != nil {
			s.logger.Error("发送 Agent 上线恢复通知失败",
				zap.Uint("notification_id", notification.ID),
				zap.String("type", string(notification.Type)),
				zap.Error(err),
			)
			// 继续发送其他通知，不中断
		}
	}

	return nil
}

// filterAgentOnlineNotifications 过滤匹配的 Agent 上线恢复通知配置
func (s *NotificationService) filterAgentOnlineNotifications(
	notifications []model.Notification,
	onlineData *AgentOnlineData,
) []model.Notification {
	var matched []model.Notification

	for _, notification := range notifications {
		// 检查主机范围
		if !s.matchAgentOnlineScope(&notification, onlineData) {
			continue
		}

		matched = append(matched, notification)
	}

	return matched
}

// matchAgentOnlineScope 检查主机范围是否匹配（Agent 上线场景）
func (s *NotificationService) matchAgentOnlineScope(notification *model.Notification, onlineData *AgentOnlineData) bool {
	switch notification.Scope {
	case model.NotificationScopeGlobal:
		return true

	case model.NotificationScopeBusinessLine:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		// 需要查询主机的业务线
		var host model.Host
		if err := s.db.First(&host, "host_id = ?", onlineData.HostID).Error; err != nil {
			return false
		}
		for _, bl := range scopeValue.BusinessLines {
			if bl == host.BusinessLine {
				return true
			}
		}
		return false

	case model.NotificationScopeHostTags:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		// 需要查询主机的标签
		var host model.Host
		if err := s.db.First(&host, "host_id = ?", onlineData.HostID).Error; err != nil {
			return false
		}
		for _, tag := range scopeValue.Tags {
			for _, hostTag := range host.Tags {
				if tag == hostTag {
					return true
				}
			}
		}
		return false

	case model.NotificationScopeSpecified:
		// 解析 scope_value
		var scopeValue model.ScopeValueData
		if err := json.Unmarshal([]byte(notification.ScopeValue), &scopeValue); err != nil {
			return false
		}
		for _, hostID := range scopeValue.HostIDs {
			if hostID == onlineData.HostID {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// sendAgentOnlineNotification 发送单个 Agent 上线恢复通知
func (s *NotificationService) sendAgentOnlineNotification(
	notification *model.Notification,
	onlineData *AgentOnlineData,
) error {
	var message map[string]interface{}

	switch notification.Type {
	case model.NotificationTypeLark:
		message = s.buildLarkAgentOnlineCard(notification, onlineData)
	case model.NotificationTypeWebhook:
		message = s.buildWebhookAgentOnline(onlineData)
	default:
		return fmt.Errorf("不支持的通知类型: %s", notification.Type)
	}

	// 发送 HTTP 请求
	jsonData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	resp, err := http.Post(notification.Config.WebhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return fmt.Errorf("服务器返回状态码: %d，响应: %s", resp.StatusCode, bodyStr)
	}

	s.logger.Info("Agent 上线恢复通知发送成功",
		zap.String("host_id", onlineData.HostID),
		zap.String("hostname", onlineData.Hostname),
	)

	return nil
}

// buildLarkAgentOnlineCard 构建 Lark Agent 上线恢复卡片消息
func (s *NotificationService) buildLarkAgentOnlineCard(
	notification *model.Notification,
	onlineData *AgentOnlineData,
) map[string]interface{} {
	// 计算离线时长
	offlineDuration := onlineData.OnlineAt.Sub(onlineData.OfflineSince)
	durationStr := formatDuration(offlineDuration)

	// 构建告警描述
	description := fmt.Sprintf(
		"矩阵云安全平台检测到您的主机 Agent 已恢复上线。\n\n"+
			"**主机名称：** %s\n"+
			"**主机 IP：** %s\n"+
			"**操作系统：** %s %s\n"+
			"**上线时间：** %s\n"+
			"**离线时长：** %s",
		onlineData.Hostname,
		onlineData.IP,
		onlineData.OSFamily,
		onlineData.OSVersion,
		onlineData.OnlineAt.Format("2006-01-02 15:04:05"),
		durationStr,
	)

	elements := []map[string]interface{}{
		{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": description,
			},
		},
		{
			"tag": "hr",
		},
	}

	// 添加查看详情按钮
	if notification.FrontendURL != "" {
		elements = append(elements, map[string]interface{}{
			"tag": "action",
			"actions": []map[string]interface{}{
				{
					"tag": "button",
					"text": map[string]interface{}{
						"tag":     "plain_text",
						"content": "查看主机详情",
					},
					"type": "primary",
					"url":  fmt.Sprintf("%s/hosts/%s", strings.TrimSuffix(notification.FrontendURL, "/"), onlineData.HostID),
				},
			},
		})
	}

	card := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": "✅ Agent 恢复上线",
			},
			"template": "green", // 绿色表示恢复
		},
		"elements": elements,
	}

	message := map[string]interface{}{
		"msg_type": "interactive",
		"card":     card,
	}

	// Lark 需要签名
	if notification.Config.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		sign, err := s.generateLarkSign(notification.Config.Secret, timestamp)
		if err == nil {
			message["timestamp"] = timestamp
			message["sign"] = sign
		}
	}

	return message
}

// buildWebhookAgentOnline 构建 Webhook Agent 上线恢复消息
func (s *NotificationService) buildWebhookAgentOnline(onlineData *AgentOnlineData) map[string]interface{} {
	return map[string]interface{}{
		"alert_type":    "agent_online",
		"host_id":       onlineData.HostID,
		"hostname":      onlineData.Hostname,
		"ip":            onlineData.IP,
		"os_family":     onlineData.OSFamily,
		"os_version":    onlineData.OSVersion,
		"online_at":     onlineData.OnlineAt.Format(time.RFC3339),
		"offline_since": onlineData.OfflineSince.Format(time.RFC3339),
	}
}
