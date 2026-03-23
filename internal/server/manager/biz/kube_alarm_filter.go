package biz

import (
	"regexp"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// KubeAlarmService 告警服务，负责告警创建与白名单过滤
type KubeAlarmService struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewKubeAlarmService 创建告警服务
func NewKubeAlarmService(db *gorm.DB, logger *zap.Logger) *KubeAlarmService {
	return &KubeAlarmService{db: db, logger: logger}
}

// CreateAlarmWithFilter 创建告警前检查白名单，命中则跳过并更新 hit_count
// 返回 (created bool, err error)
func (s *KubeAlarmService) CreateAlarmWithFilter(alarm *model.KubeAlarm) (bool, error) {
	if s.matchWhitelist(alarm) {
		s.logger.Info("告警命中白名单，已跳过",
			zap.String("alarm_type", string(alarm.AlarmType)),
			zap.String("namespace", alarm.Namespace),
			zap.String("pod_name", alarm.PodName),
			zap.Uint("cluster_id", alarm.ClusterID),
		)
		return false, nil
	}

	if err := s.db.Create(alarm).Error; err != nil {
		s.logger.Error("创建告警失败", zap.Error(err))
		return false, err
	}
	return true, nil
}

// BatchCreateAlarmsWithFilter 批量创建告警（带白名单过滤）
func (s *KubeAlarmService) BatchCreateAlarmsWithFilter(alarms []model.KubeAlarm) (created int, filtered int, err error) {
	for i := range alarms {
		ok, e := s.CreateAlarmWithFilter(&alarms[i])
		if e != nil {
			return created, filtered, e
		}
		if ok {
			created++
		} else {
			filtered++
		}
	}
	return created, filtered, nil
}

// matchWhitelist 检查告警是否命中任一白名单规则
func (s *KubeAlarmService) matchWhitelist(alarm *model.KubeAlarm) bool {
	var rules []model.KubeWhitelist
	query := s.db.Where("status = ?", model.KubeWhitelistStatusEnabled)
	if err := query.Find(&rules).Error; err != nil {
		s.logger.Error("查询白名单失败", zap.Error(err))
		return false
	}

	for _, rule := range rules {
		if s.ruleMatches(&rule, alarm) {
			// 更新命中计数
			s.db.Model(&rule).UpdateColumn("hit_count", gorm.Expr("hit_count + 1"))
			return true
		}
	}
	return false
}

// ruleMatches 判断单条白名单规则是否匹配告警
func (s *KubeAlarmService) ruleMatches(rule *model.KubeWhitelist, alarm *model.KubeAlarm) bool {
	// 集群匹配：rule.ClusterID 为 nil 表示全局规则
	if rule.ClusterID != nil && *rule.ClusterID != alarm.ClusterID {
		return false
	}

	// 告警类型匹配：空列表表示匹配所有类型
	if len(rule.AlarmTypes) > 0 {
		matched := false
		for _, t := range rule.AlarmTypes {
			if strings.EqualFold(t, string(alarm.AlarmType)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Namespace 匹配：空字符串表示匹配所有
	if rule.Namespace != "" && !strings.EqualFold(rule.Namespace, alarm.Namespace) {
		return false
	}

	// Pod 名称模式匹配：支持通配符 * 和正则
	if rule.PodPattern != "" && alarm.PodName != "" {
		if !matchPattern(rule.PodPattern, alarm.PodName) {
			return false
		}
	}

	return true
}

// matchPattern 支持简单通配符（*）和正则表达式匹配
func matchPattern(pattern, value string) bool {
	// 如果包含 * 但不是正则，转换为正则
	if strings.Contains(pattern, "*") && !strings.HasPrefix(pattern, "^") {
		regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
		pattern = regexPattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(value)
}
