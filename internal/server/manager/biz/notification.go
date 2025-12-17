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

// AlertData 告警数据
type AlertData struct {
	// 主机信息
	HostID    string
	Hostname  string
	IP        string
	OSFamily  string
	OSVersion string

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

// SendAlertNotification 发送告警通知
func (s *NotificationService) SendAlertNotification(alertData *AlertData) error {
	// 查询所有启用的通知配置
	var notifications []model.Notification
	if err := s.db.Where("enabled = ?", true).Find(&notifications).Error; err != nil {
		s.logger.Error("查询通知配置失败", zap.Error(err))
		return err
	}

	// 过滤匹配的通知配置
	matchedNotifications := s.filterNotifications(notifications, alertData)

	// 发送通知
	for _, notification := range matchedNotifications {
		if err := s.sendNotification(&notification, alertData); err != nil {
			s.logger.Error("发送通知失败",
				zap.Uint("notification_id", notification.ID),
				zap.String("type", string(notification.Type)),
				zap.Error(err),
			)
			// 继续发送其他通知，不中断
		}
	}

	return nil
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
	// 构建告警描述（参考 Elkeid 格式）
	description := fmt.Sprintf(
		"矩阵云安全平台检测到您的资产(资产名称:%s)存在疑似【%s】基线风险,本次告警发生时间为:%s,请及时登录矩阵云安全平台控制台进行处理。",
		alertData.Hostname,
		alertData.RuleName,
		alertData.CheckedAt.Format("2006-01-02 15:04:05"),
	)

	// 构建原始数据（参考 Elkeid 格式）
	rawData := map[string]interface{}{
		"alert_type_us": notification.Name,
		"hostname":      alertData.Hostname,
		"host_id":       alertData.HostID,
		"rule_id":       alertData.RuleID,
		"rule_name":     alertData.RuleName,
		"category":      alertData.Category,
		"severity":      alertData.Severity,
		"actual":        alertData.Actual,
		"expected":      alertData.Expected,
		"time":          alertData.CheckedAt.Format(time.RFC3339),
	}

	// 构建原始数据文本
	rawDataLines := []string{}
	for k, v := range rawData {
		rawDataLines = append(rawDataLines, fmt.Sprintf(`"%s": "%v"`, k, v))
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
		"alert_type": "baseline_risk",
		"host_id":    alertData.HostID,
		"hostname":   alertData.Hostname,
		"rule_id":    alertData.RuleID,
		"rule_name":  alertData.RuleName,
		"category":   alertData.Category,
		"severity":   alertData.Severity,
		"title":      alertData.Title,
		"actual":     alertData.Actual,
		"expected":   alertData.Expected,
		"checked_at": alertData.CheckedAt.Format(time.RFC3339),
		"url":        alertData.FrontendURL,
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
