// Package migration 提供数据库迁移功能
package migration

import (
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// Migrate 执行数据库迁移
func Migrate(db *gorm.DB, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	logger.Info("开始数据库迁移")

	// 处理组件表的迁移问题（旧数据可能没有有效的外键）
	if err := migrateComponentTables(db, logger); err != nil {
		logger.Warn("组件表迁移处理", zap.Error(err))
	}

	// 执行自动迁移
	for _, m := range model.AllModels {
		if err := db.AutoMigrate(m); err != nil {
			logger.Error("数据库迁移失败", zap.Error(err), zap.String("model", fmt.Sprintf("%T", m)))
			return fmt.Errorf("迁移模型 %T 失败: %w", m, err)
		}
		logger.Info("模型迁移成功", zap.String("model", fmt.Sprintf("%T", m)))
	}

	logger.Info("数据库迁移完成")
	return nil
}

// migrateComponentTables 处理组件相关表的迁移
// 由于数据模型从扁平结构改为层级结构（Component → Version → Package），
// 旧的 component_packages 表可能有数据但没有有效的 version_id 外键
func migrateComponentTables(db *gorm.DB, logger *zap.Logger) error {
	// 检查 component_packages 表是否存在
	var packagesExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'component_packages'").Scan(&packagesExists).Error; err != nil {
		return err
	}

	if !packagesExists {
		return nil // 表不存在，无需处理
	}

	// 检查 component_versions 表是否存在
	var versionsExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'component_versions'").Scan(&versionsExists).Error; err != nil {
		return err
	}

	// 如果 component_packages 存在但 component_versions 不存在，说明是旧结构
	// 需要清理旧数据，让迁移重新创建表
	if !versionsExists {
		logger.Info("检测到旧的组件包表结构，清理旧数据以便迁移")

		// 删除旧表（按依赖顺序）
		tables := []string{"component_packages", "component_versions", "components"}
		for _, table := range tables {
			if err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table)).Error; err != nil {
				logger.Warn("删除旧组件表失败", zap.String("table", table), zap.Error(err))
			} else {
				logger.Info("删除旧组件表成功", zap.String("table", table))
			}
		}
		return nil
	}

	// 检查 component_packages 中是否有孤立数据（version_id 不在 component_versions 中）
	var orphanCount int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM component_packages cp
		LEFT JOIN component_versions cv ON cp.version_id = cv.id
		WHERE cv.id IS NULL AND cp.version_id IS NOT NULL
	`).Scan(&orphanCount).Error; err != nil {
		// 查询失败可能是因为表结构不同，尝试清理
		logger.Warn("检查孤立数据失败，尝试清理组件表", zap.Error(err))
		cleanupComponentTables(db, logger)
		return nil
	}

	if orphanCount > 0 {
		logger.Info("检测到孤立的组件包数据，清理旧数据", zap.Int64("orphan_count", orphanCount))
		cleanupComponentTables(db, logger)
	}

	return nil
}

// cleanupComponentTables 清理组件相关表
func cleanupComponentTables(db *gorm.DB, logger *zap.Logger) {
	// 先删除外键约束
	db.Exec("SET FOREIGN_KEY_CHECKS = 0")
	defer db.Exec("SET FOREIGN_KEY_CHECKS = 1")

	tables := []string{"component_packages", "component_versions", "components"}
	for _, table := range tables {
		if err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table)).Error; err != nil {
			logger.Warn("删除组件表失败", zap.String("table", table), zap.Error(err))
		} else {
			logger.Info("删除组件表成功", zap.String("table", table))
		}
	}
}

// Rollback 回滚数据库（谨慎使用）
func Rollback(db *gorm.DB, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	logger.Warn("开始数据库回滚（删除所有表）")

	// 先禁用外键检查
	db.Exec("SET FOREIGN_KEY_CHECKS = 0")
	defer db.Exec("SET FOREIGN_KEY_CHECKS = 1")

	// 删除所有表（按依赖顺序）
	tables := []string{
		// 组件相关表
		"component_packages",
		"component_versions",
		"components",
		// 插件配置
		"plugin_configs",
		// 检测和任务
		"scan_results",
		"scan_tasks",
		"rules",
		"policies",
		"policy_groups",
		// 资产表
		"processes",
		"ports",
		"asset_users",
		"software",
		"containers",
		"apps",
		"net_interfaces",
		"volumes",
		"kmods",
		"services",
		"crons",
		// 监控数据
		"host_metrics",
		"host_metrics_hourly",
		// 系统配置
		"alerts",
		"notifications",
		"system_configs",
		"business_lines",
		// 核心表
		"hosts",
		"users",
	}

	for _, table := range tables {
		if err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table)).Error; err != nil {
			logger.Error("删除表失败", zap.Error(err), zap.String("table", table))
			return fmt.Errorf("删除表 %s 失败: %w", table, err)
		}
		logger.Info("删除表成功", zap.String("table", table))
	}

	logger.Info("数据库回滚完成")
	return nil
}
