// Package migration 提供数据库迁移功能
package migration

import (
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AddPerformanceIndexes 添加性能优化索引
func AddPerformanceIndexes(db *gorm.DB, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	logger.Info("开始添加性能优化索引")

	// 1. scan_results 表：添加复合索引 (host_id, rule_id, checked_at)
	// 用于优化"获取每个规则的最新结果"查询
	indexSQL := `
		CREATE INDEX IF NOT EXISTS idx_scan_results_host_rule_checked 
		ON scan_results(host_id, rule_id, checked_at DESC)
	`
	if err := db.Exec(indexSQL).Error; err != nil {
		logger.Error("创建索引失败", zap.Error(err), zap.String("index", "idx_scan_results_host_rule_checked"))
		return fmt.Errorf("创建索引失败: %w", err)
	}
	logger.Info("索引创建成功", zap.String("index", "idx_scan_results_host_rule_checked"))

	// 2. scan_results 表：添加复合索引 (host_id, checked_at)
	// 用于优化"获取主机最新检测结果"查询
	indexSQL2 := `
		CREATE INDEX IF NOT EXISTS idx_scan_results_host_checked 
		ON scan_results(host_id, checked_at DESC)
	`
	if err := db.Exec(indexSQL2).Error; err != nil {
		logger.Error("创建索引失败", zap.Error(err), zap.String("index", "idx_scan_results_host_checked"))
		return fmt.Errorf("创建索引失败: %w", err)
	}
	logger.Info("索引创建成功", zap.String("index", "idx_scan_results_host_checked"))

	// 3. scan_tasks 表：添加索引 (status, created_at)
	// 用于优化"查询待执行任务"查询
	indexSQL3 := `
		CREATE INDEX IF NOT EXISTS idx_scan_tasks_status_created 
		ON scan_tasks(status, created_at)
	`
	if err := db.Exec(indexSQL3).Error; err != nil {
		logger.Error("创建索引失败", zap.Error(err), zap.String("index", "idx_scan_tasks_status_created"))
		return fmt.Errorf("创建索引失败: %w", err)
	}
	logger.Info("索引创建成功", zap.String("index", "idx_scan_tasks_status_created"))

	// 4. scan_results 表：添加复合索引 (host_id, status, severity)
	// 用于优化主机风险统计查询（GetHostRiskStatistics）
	indexSQL4 := `
		CREATE INDEX IF NOT EXISTS idx_scan_results_host_status_severity
		ON scan_results(host_id, status, severity)
	`
	if err := db.Exec(indexSQL4).Error; err != nil {
		logger.Error("创建索引失败", zap.Error(err), zap.String("index", "idx_scan_results_host_status_severity"))
		return fmt.Errorf("创建索引失败: %w", err)
	}
	logger.Info("索引创建成功", zap.String("index", "idx_scan_results_host_status_severity"))

	logger.Info("性能优化索引添加完成")
	return nil
}
