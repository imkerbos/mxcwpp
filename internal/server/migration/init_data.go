// Package migration 提供数据库初始化数据功能
package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/config"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
	"github.com/imkerbos/mxsec-platform/plugins/baseline/engine"
)

// DefaultPolicyGroupID 默认策略组ID
const DefaultPolicyGroupID = "system-baseline"

// InitDefaultData 初始化默认数据（策略和规则）
func InitDefaultData(db *gorm.DB, logger *zap.Logger, policyDir string, pluginsCfg *config.PluginsConfig) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	logger.Info("开始初始化默认数据", zap.String("policy_dir", policyDir))

	// 初始化默认用户（优先执行，确保admin用户始终存在）
	if err := initDefaultUsers(db, logger); err != nil {
		return fmt.Errorf("初始化默认用户失败: %w", err)
	}

	// 初始化默认策略组（始终执行，确保策略组存在）
	if err := initDefaultPolicyGroup(db, logger); err != nil {
		return fmt.Errorf("初始化默认策略组失败: %w", err)
	}

	// 初始化默认插件配置（始终执行，确保插件配置存在）
	if err := initDefaultPluginConfigs(db, logger, pluginsCfg); err != nil {
		return fmt.Errorf("初始化默认插件配置失败: %w", err)
	}

	// 检查是否已有策略数据
	var count int64
	if err := db.Model(&model.Policy{}).Count(&count).Error; err != nil {
		return fmt.Errorf("检查现有数据失败: %w", err)
	}

	if count > 0 {
		logger.Info("数据库中已存在策略数据，跳过策略初始化", zap.Int64("count", count))
		// 检查并更新已存在策略的 group_id（如果为空）
		if err := associateExistingPoliciesWithGroup(db, logger); err != nil {
			logger.Warn("关联已存在策略到默认策略组失败", zap.Error(err))
		}
	} else {

		// 从示例策略文件加载
		if policyDir == "" {
			// 优先使用生产环境路径，回退到开发环境路径
			if _, err := os.Stat("/opt/mxsec-platform/baseline-policies"); err == nil {
				policyDir = "/opt/mxsec-platform/baseline-policies"
			} else {
				policyDir = "plugins/baseline/config/examples"
			}
		}

		policies, err := loadPoliciesFromDir(policyDir, logger)
		if err != nil {
			// 策略文件加载失败不阻止启动，只记录警告
			logger.Warn("加载策略文件失败，跳过策略初始化", zap.Error(err), zap.String("policy_dir", policyDir))
			return nil
		}

		// 保存到数据库（关联到默认策略组）
		for _, policy := range policies {
			if err := savePolicyToDB(db, policy, DefaultPolicyGroupID, logger); err != nil {
				return fmt.Errorf("保存策略 %s 失败: %w", policy.ID, err)
			}
			logger.Info("策略初始化成功", zap.String("policy_id", policy.ID), zap.String("name", policy.Name))
		}

		logger.Info("默认数据初始化完成", zap.Int("policy_count", len(policies)))
	}

	return nil
}

// initDefaultUsers 初始化默认用户
func initDefaultUsers(db *gorm.DB, logger *zap.Logger) error {
	// 检查admin用户是否存在
	var adminUser model.User
	err := db.Where("username = ?", "admin").First(&adminUser).Error

	if err == nil {
		// admin用户已存在，检查状态并确保为active
		if adminUser.Status != model.UserStatusActive {
			adminUser.Status = model.UserStatusActive
			if err := db.Save(&adminUser).Error; err != nil {
				return fmt.Errorf("更新admin用户状态失败: %w", err)
			}
			logger.Info("admin用户状态已更新为active", zap.String("username", adminUser.Username))
		} else {
			logger.Info("admin用户已存在且状态正常", zap.String("username", adminUser.Username))
		}
		return nil
	}

	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("检查admin用户失败: %w", err)
	}

	// admin用户不存在，创建默认管理员用户（admin/admin123）
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("加密密码失败: %w", err)
	}

	defaultUser := &model.User{
		Username: "admin",
		Password: string(hashedPassword),
		Email:    "admin@example.com",
		Role:     model.UserRoleAdmin,
		Status:   model.UserStatusActive,
	}

	if err := db.Create(defaultUser).Error; err != nil {
		return fmt.Errorf("创建默认用户失败: %w", err)
	}

	logger.Info("默认用户初始化成功", zap.String("username", defaultUser.Username))
	return nil
}

// loadPoliciesFromDir 从目录加载所有策略文件
func loadPoliciesFromDir(dir string, logger *zap.Logger) ([]*engine.Policy, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	var policies []*engine.Policy

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// 只处理 JSON 文件
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		logger.Info("加载策略文件", zap.String("file", filePath))

		data, err := os.ReadFile(filePath)
		if err != nil {
			logger.Warn("读取策略文件失败", zap.Error(err), zap.String("file", filePath))
			continue
		}

		var policy engine.Policy
		if err := json.Unmarshal(data, &policy); err != nil {
			logger.Warn("解析策略文件失败", zap.Error(err), zap.String("file", filePath))
			continue
		}

		policies = append(policies, &policy)
	}

	return policies, nil
}

// savePolicyToDB 保存策略到数据库
func savePolicyToDB(db *gorm.DB, policy *engine.Policy, groupID string, logger *zap.Logger) error {
	// 转换 Policy 模型
	// 默认设置 RuntimeTypes 为 ["vm"]（仅虚拟机适用）
	// 这样确保 Linux 系统基线规则不会应用于 Docker 容器
	dbPolicy := &model.Policy{
		ID:           policy.ID,
		Name:         policy.Name,
		Version:      policy.Version,
		Description:  policy.Description,
		OSFamily:     model.StringArray(policy.OSFamily),
		OSVersion:    policy.OSVersion,
		RuntimeTypes: model.StringArray{"vm"}, // 默认仅适用于虚拟机
		Enabled:      policy.Enabled,
		GroupID:      groupID, // 关联到策略组
	}

	// 创建策略
	if err := db.Create(dbPolicy).Error; err != nil {
		return fmt.Errorf("创建策略失败: %w", err)
	}

	// 转换并创建规则
	for _, rule := range policy.Rules {
		// 转换 Check 配置
		checkConfig := model.CheckConfig{
			Condition: rule.Check.Condition,
			Rules:     make([]model.CheckRule, len(rule.Check.Rules)),
		}
		for i, cr := range rule.Check.Rules {
			checkRule := model.CheckRule{
				Type:  cr.Type,
				Param: cr.Param,
			}
			// Result 字段可能为空，需要检查
			if cr.Result != "" {
				checkRule.Result = cr.Result
			}
			checkConfig.Rules[i] = checkRule
		}

		// 转换 Fix 配置
		fixConfig := model.FixConfig{
			Suggestion: rule.Fix.Suggestion,
			Command:    rule.Fix.Command,
		}

		dbRule := &model.Rule{
			RuleID:      rule.RuleID,
			PolicyID:    policy.ID,
			Category:    rule.Category,
			Title:       rule.Title,
			Description: rule.Description,
			Severity:    rule.Severity,
			// RuntimeTypes 为空，表示继承策略的设置
			// 策略已设置为 ["vm"]，规则自动继承
			CheckConfig: checkConfig,
			FixConfig:   fixConfig,
		}

		if err := db.Create(dbRule).Error; err != nil {
			return fmt.Errorf("创建规则 %s 失败: %w", rule.RuleID, err)
		}
	}

	return nil
}

// initDefaultPolicyGroup 初始化默认策略组
func initDefaultPolicyGroup(db *gorm.DB, logger *zap.Logger) error {
	// 检查默认策略组是否存在
	var group model.PolicyGroup
	err := db.Where("id = ?", DefaultPolicyGroupID).First(&group).Error

	if err == nil {
		// 默认策略组已存在
		logger.Info("默认策略组已存在", zap.String("group_id", DefaultPolicyGroupID), zap.String("name", group.Name))
		return nil
	}

	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("检查默认策略组失败: %w", err)
	}

	// 创建默认策略组
	defaultGroup := &model.PolicyGroup{
		ID:          DefaultPolicyGroupID,
		Name:        "主机系统基线组",
		Description: "系统内置的基线检查策略组，包含 Linux 主机操作系统安全基线检查策略（仅适用于主机/虚拟机，不适用于容器）",
		Icon:        "🖥",
		Color:       "#1890ff",
		SortOrder:   0,
		Enabled:     true,
	}

	if err := db.Create(defaultGroup).Error; err != nil {
		return fmt.Errorf("创建默认策略组失败: %w", err)
	}

	logger.Info("默认策略组初始化成功",
		zap.String("group_id", defaultGroup.ID),
		zap.String("name", defaultGroup.Name),
	)
	return nil
}

// associateExistingPoliciesWithGroup 将没有分组的策略关联到默认策略组
func associateExistingPoliciesWithGroup(db *gorm.DB, logger *zap.Logger) error {
	// 查找没有分组的策略
	result := db.Model(&model.Policy{}).
		Where("group_id IS NULL OR group_id = ''").
		Update("group_id", DefaultPolicyGroupID)

	if result.Error != nil {
		return fmt.Errorf("更新策略分组失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Info("已将未分组策略关联到默认策略组",
			zap.Int64("count", result.RowsAffected),
			zap.String("group_id", DefaultPolicyGroupID),
		)
	}

	return nil
}

// initDefaultPluginConfigs 初始化默认插件配置
func initDefaultPluginConfigs(db *gorm.DB, logger *zap.Logger, pluginsCfg *config.PluginsConfig) error {
	// 构建插件下载 URL
	// 如果配置了 base_url，使用 HTTP 下载
	// 否则使用 file:// 协议（仅限开发环境）
	var baselineURL, collectorURL string
	if pluginsCfg != nil && pluginsCfg.BaseURL != "" {
		// 生产环境：使用 HTTP URL
		baselineURL = pluginsCfg.BaseURL + "/baseline"
		collectorURL = pluginsCfg.BaseURL + "/collector"
		logger.Info("使用 HTTP 插件下载 URL",
			zap.String("base_url", pluginsCfg.BaseURL),
		)
	} else {
		// 开发环境：使用 file:// 协议
		pluginDir := "/workspace/dist/plugins"
		if pluginsCfg != nil && pluginsCfg.Dir != "" {
			pluginDir = pluginsCfg.Dir
		}
		baselineURL = "file://" + pluginDir + "/baseline"
		collectorURL = "file://" + pluginDir + "/collector"
		logger.Info("使用 file:// 插件下载 URL（开发环境）",
			zap.String("plugin_dir", pluginDir),
		)
	}

	// 定义默认插件配置
	defaultPlugins := []model.PluginConfig{
		{
			Name:    "baseline",
			Type:    model.PluginTypeBaseline,
			Version: "1.0.2", // 版本更新，触发 URL 更新
			SHA256:  "",      // 暂时为空，后续可以添加校验
			DownloadURLs: model.StringArray{
				baselineURL,
			},
			Detail:      `{"check_interval": 3600}`,
			Enabled:     true,
			Description: "Linux 基线安全检查插件，执行操作系统安全配置检查",
		},
		{
			Name:    "collector",
			Type:    model.PluginTypeCollector,
			Version: "1.0.2",
			SHA256:  "",
			DownloadURLs: model.StringArray{
				collectorURL,
			},
			Detail:      `{"collect_interval": 300}`,
			Enabled:     true,
			Description: "资产采集插件，采集主机进程、端口、用户等信息",
		},
	}

	for _, plugin := range defaultPlugins {
		// 检查插件是否已存在
		var existing model.PluginConfig
		err := db.Where("name = ?", plugin.Name).First(&existing).Error

		if err == nil {
			// 插件已存在，跳过（不覆盖已有配置）
			// 版本应该由组件管理系统（component_versions）统一管理
			logger.Debug("插件配置已存在，跳过初始化",
				zap.String("name", plugin.Name),
				zap.String("current_version", existing.Version),
			)
			continue
		}

		if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("检查插件配置 %s 失败: %w", plugin.Name, err)
		}

		// 创建新的插件配置（仅在不存在时）
		if err := db.Create(&plugin).Error; err != nil {
			return fmt.Errorf("创建插件配置 %s 失败: %w", plugin.Name, err)
		}
		logger.Info("插件配置初始化成功",
			zap.String("name", plugin.Name),
			zap.String("type", string(plugin.Type)),
			zap.String("version", plugin.Version),
		)
	}

	return nil
}
