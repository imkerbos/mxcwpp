// Package migration 提供数据库迁移功能
package migration

import (
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
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

	// 执行数据迁移：扩展资产表 ID 列（GORM AutoMigrate 不一定会自动扩展已有列的长度）
	if err := migrateAssetTableIDColumns(db, logger); err != nil {
		logger.Warn("资产表ID列扩展处理", zap.Error(err))
	}

	// 执行数据迁移：为现有数据设置默认的运行时类型
	if err := migrateRuntimeTypes(db, logger); err != nil {
		logger.Warn("运行时类型迁移处理", zap.Error(err))
	}

	// 执行数据迁移：更新策略组名称为主机系统基线组
	if err := migratePolicyGroupName(db, logger); err != nil {
		logger.Warn("策略组名称迁移处理", zap.Error(err))
	}

	// 执行数据迁移：为通知配置设置 notify_category
	if err := migrateNotificationCategory(db, logger); err != nil {
		logger.Warn("通知类别迁移处理", zap.Error(err))
	}

	logger.Info("数据库迁移完成")
	return nil
}

// migrateNotificationCategory 为现有通知配置设置 notify_category
func migrateNotificationCategory(db *gorm.DB, logger *zap.Logger) error {
	// 1. 将名称包含 "离线" 的通知设置为 agent_offline
	result := db.Model(&model.Notification{}).
		Where("(notify_category IS NULL OR notify_category = '')").
		Where("name LIKE ?", "%离线%").
		Update("notify_category", model.NotifyCategoryAgentOffline)
	if result.Error != nil {
		logger.Warn("更新 Agent 离线通知类别失败", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		logger.Info("已更新 Agent 离线通知类别",
			zap.Int64("count", result.RowsAffected))
	}

	// 2. 将其他通知设置为 baseline_alert（默认）
	result = db.Model(&model.Notification{}).
		Where("notify_category IS NULL OR notify_category = ''").
		Update("notify_category", model.NotifyCategoryBaselineAlert)
	if result.Error != nil {
		logger.Warn("更新基线告警通知类别失败", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		logger.Info("已更新基线告警通知类别",
			zap.Int64("count", result.RowsAffected))
	}

	// 3. 清空 Agent 离线通知的 severities（不需要等级配置）
	result = db.Model(&model.Notification{}).
		Where("notify_category = ?", model.NotifyCategoryAgentOffline).
		Update("severities", model.StringArray{})
	if result.Error != nil {
		logger.Warn("清空 Agent 离线通知的 severities 失败", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		logger.Info("已清空 Agent 离线通知的 severities",
			zap.Int64("count", result.RowsAffected))
	}

	return nil
}

// migrateRuntimeTypes 为现有数据设置默认的运行时类型
func migrateRuntimeTypes(db *gorm.DB, logger *zap.Logger) error {
	// 1. 更新现有主机的 runtime_type
	// 如果 is_container = true，设置为 docker；否则设置为 vm
	result := db.Model(&model.Host{}).
		Where("runtime_type IS NULL OR runtime_type = ''").
		Where("is_container = ?", true).
		Update("runtime_type", model.RuntimeTypeDocker)
	if result.Error != nil {
		logger.Warn("更新容器主机的 runtime_type 失败", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		logger.Info("已更新容器主机的 runtime_type",
			zap.Int64("count", result.RowsAffected),
			zap.String("runtime_type", string(model.RuntimeTypeDocker)))
	}

	result = db.Model(&model.Host{}).
		Where("runtime_type IS NULL OR runtime_type = ''").
		Where("is_container = ? OR is_container IS NULL", false).
		Update("runtime_type", model.RuntimeTypeVM)
	if result.Error != nil {
		logger.Warn("更新虚拟机主机的 runtime_type 失败", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		logger.Info("已更新虚拟机主机的 runtime_type",
			zap.Int64("count", result.RowsAffected),
			zap.String("runtime_type", string(model.RuntimeTypeVM)))
	}

	// 2. 更新所有策略的 runtime_types 为 ["vm"]
	// 这里强制更新所有策略，确保所有策略都有默认的运行时类型
	result = db.Model(&model.Policy{}).
		Where("runtime_types IS NULL OR runtime_types = '[]' OR runtime_types = '' OR runtime_types = 'null'").
		Update("runtime_types", model.StringArray{"vm"})
	if result.Error != nil {
		logger.Warn("更新策略的 runtime_types 失败", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		logger.Info("已更新策略的 runtime_types",
			zap.Int64("count", result.RowsAffected),
			zap.Strings("runtime_types", []string{"vm"}))
	}

	// 2.1 额外检查：强制更新那些 runtime_types 可能包含无效值的记录
	// 使用 JSON 包含检查，如果不包含有效值则更新
	result = db.Exec(`
		UPDATE policies 
		SET runtime_types = '["vm"]' 
		WHERE runtime_types NOT LIKE '%"vm"%' 
		  AND runtime_types NOT LIKE '%"docker"%' 
		  AND runtime_types NOT LIKE '%"k8s"%'
	`)
	if result.Error != nil {
		logger.Warn("强制更新策略的 runtime_types 失败", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		logger.Info("强制更新了无效的策略 runtime_types",
			zap.Int64("count", result.RowsAffected))
	}

	// 3. 清空现有规则的 runtime_types，让它们继承策略的设置
	// 规则默认继承策略的 RuntimeTypes，不需要单独设置
	result = db.Model(&model.Rule{}).
		Where("runtime_types IS NOT NULL AND runtime_types != '[]' AND runtime_types != ''").
		Update("runtime_types", model.StringArray{})
	if result.Error != nil {
		logger.Warn("清空规则的 runtime_types 失败", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		logger.Info("已清空规则的 runtime_types（规则将继承策略的设置）",
			zap.Int64("count", result.RowsAffected))
	}

	return nil
}

// migrateAssetTableIDColumns 扩展资产表的 ID 列从 varchar(64) 到 varchar(128)
// GORM AutoMigrate 不保证会扩展已有列的长度，需要显式 ALTER TABLE
func migrateAssetTableIDColumns(db *gorm.DB, logger *zap.Logger) error {
	// 所有需要扩展 id 列的资产表（ID 格式为 "{host_id}-{xxx}"，host_id 是 64 字符 SHA256）
	tables := []string{
		"processes", "ports", "asset_users", "software",
		"containers", "apps", "net_interfaces", "volumes",
		"kmods", "services", "crons",
	}

	for _, table := range tables {
		// 先检查表是否存在
		var exists bool
		if err := db.Raw(
			"SELECT COUNT(*) > 0 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			table,
		).Scan(&exists).Error; err != nil {
			logger.Warn("检查表是否存在失败", zap.String("table", table), zap.Error(err))
			continue
		}
		if !exists {
			continue
		}

		// 检查当前列长度
		var columnType string
		if err := db.Raw(
			"SELECT COLUMN_TYPE FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = 'id'",
			table,
		).Scan(&columnType).Error; err != nil {
			logger.Warn("查询列类型失败", zap.String("table", table), zap.Error(err))
			continue
		}

		// 如果已经是 varchar(128) 或更大则跳过
		if columnType == "varchar(128)" {
			continue
		}

		// 执行 ALTER TABLE
		sql := fmt.Sprintf("ALTER TABLE `%s` MODIFY COLUMN `id` varchar(128) NOT NULL", table)
		if err := db.Exec(sql).Error; err != nil {
			logger.Error("扩展资产表ID列失败", zap.String("table", table), zap.String("old_type", columnType), zap.Error(err))
		} else {
			logger.Info("扩展资产表ID列成功", zap.String("table", table), zap.String("old_type", columnType), zap.String("new_type", "varchar(128)"))
		}
	}

	return nil
}

// migratePolicyGroupName 更新策略组名称为"主机系统基线组"
func migratePolicyGroupName(db *gorm.DB, logger *zap.Logger) error {
	// 更新默认策略组的名称（从"系统基线组"改为"主机系统基线组"）
	result := db.Model(&model.PolicyGroup{}).
		Where("id = ?", "system-baseline").
		Where("name = ?", "系统基线组").
		Updates(map[string]interface{}{
			"name":        "主机系统基线组",
			"description": "系统内置的基线检查策略组，包含 Linux 主机操作系统安全基线检查策略（仅适用于主机/虚拟机，不适用于容器）",
			"icon":        "🖥",
		})
	if result.Error != nil {
		logger.Warn("更新策略组名称失败", zap.Error(result.Error))
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Info("已更新策略组名称",
			zap.String("old_name", "系统基线组"),
			zap.String("new_name", "主机系统基线组"))
	}

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
