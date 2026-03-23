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
// 首次启动时创建默认策略组和策略数据，后续启动不再重建用户已删除的数据
func InitDefaultData(db *gorm.DB, logger *zap.Logger, policyDir string, pluginsCfg *config.PluginsConfig) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	logger.Info("开始初始化默认数据", zap.String("policy_dir", policyDir))

	// 初始化默认用户（始终执行，确保admin用户存在）
	if err := initDefaultUsers(db, logger); err != nil {
		return fmt.Errorf("初始化默认用户失败: %w", err)
	}

	// 初始化默认插件配置（始终执行，确保插件配置存在）
	if err := initDefaultPluginConfigs(db, logger, pluginsCfg); err != nil {
		return fmt.Errorf("初始化默认插件配置失败: %w", err)
	}

	// 初始化默认 FIM 策略（始终执行，仅在表为空时插入）
	if err := initDefaultFIMPolicies(db, logger); err != nil {
		return fmt.Errorf("初始化默认 FIM 策略失败: %w", err)
	}

	// 检查是否已完成首次数据初始化
	if isDataInitialized(db) {
		logger.Info("默认数据已初始化过，跳过策略组和策略重建")
		return nil
	}

	// 首次初始化：创建默认策略组
	if err := initDefaultPolicyGroup(db, logger); err != nil {
		return fmt.Errorf("初始化默认策略组失败: %w", err)
	}

	// 首次初始化：加载策略数据
	if policyDir == "" {
		if _, err := os.Stat("/opt/mxsec-platform/policies"); err == nil {
			policyDir = "/opt/mxsec-platform/policies"
		} else {
			policyDir = "plugins/baseline/config/examples"
		}
	}

	policies, err := loadPoliciesFromDir(policyDir, logger)
	if err != nil {
		logger.Warn("加载策略文件失败，跳过策略初始化", zap.Error(err), zap.String("policy_dir", policyDir))
	} else {
		for _, policy := range policies {
			if err := savePolicyToDB(db, policy, DefaultPolicyGroupID, logger); err != nil {
				return fmt.Errorf("保存策略 %s 失败: %w", policy.ID, err)
			}
			logger.Info("策略初始化成功", zap.String("policy_id", policy.ID), zap.String("name", policy.Name))
		}
		logger.Info("默认数据初始化完成", zap.Int("policy_count", len(policies)))
	}

	// 标记数据已初始化
	markDataInitialized(db, logger)

	return nil
}

// isDataInitialized 检查默认数据是否已完成首次初始化
func isDataInitialized(db *gorm.DB) bool {
	var cfg model.SystemConfig
	err := db.Where("key = ? AND category = ?", "data_initialized", "system").First(&cfg).Error
	return err == nil && cfg.Value == "true"
}

// markDataInitialized 标记默认数据已完成首次初始化
func markDataInitialized(db *gorm.DB, logger *zap.Logger) {
	cfg := model.SystemConfig{
		Key:         "data_initialized",
		Value:       "true",
		Category:    "system",
		Description: "默认数据是否已完成首次初始化（策略组、策略等）",
	}
	if err := db.Create(&cfg).Error; err != nil {
		logger.Warn("标记数据初始化状态失败", zap.Error(err))
	}
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
			Suggestion:      rule.Fix.Suggestion,
			Command:         rule.Fix.Command,
			RestartServices: rule.Fix.RestartServices,
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
	var baselineURL, collectorURL, fimURL string
	if pluginsCfg != nil && pluginsCfg.BaseURL != "" {
		// 生产环境：使用 HTTP URL
		baselineURL = pluginsCfg.BaseURL + "/baseline"
		collectorURL = pluginsCfg.BaseURL + "/collector"
		fimURL = pluginsCfg.BaseURL + "/fim"
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
		fimURL = "file://" + pluginDir + "/fim"
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
			RuntimeTypes: model.StringArray{"vm"},
			Detail:       `{"check_interval": 3600}`,
			Enabled:      true,
			Description:  "Linux 基线安全检查插件，执行操作系统安全配置检查",
		},
		{
			Name:    "collector",
			Type:    model.PluginTypeCollector,
			Version: "1.0.2",
			SHA256:  "",
			DownloadURLs: model.StringArray{
				collectorURL,
			},
			RuntimeTypes: model.StringArray{"vm", "docker", "k8s"},
			Detail:       `{"collect_interval": 300}`,
			Enabled:      true,
			Description:  "资产采集插件，采集主机进程、端口、用户等信息",
		},
		{
			Name:    "fim",
			Type:    model.PluginTypeFIM,
			Version: "1.0.0",
			SHA256:  "",
			DownloadURLs: model.StringArray{
				fimURL,
			},
			RuntimeTypes: model.StringArray{"vm"},
			Detail:       `{"check_timeout_minutes": 30}`,
			Enabled:      true,
			Description:  "文件完整性监控插件，基于 AIDE 检测文件变更",
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

// initDefaultFIMPolicies 初始化默认 FIM 策略
// 仅在 fim_policies 表为空时插入，避免重复创建
func initDefaultFIMPolicies(db *gorm.DB, logger *zap.Logger) error {
	var count int64
	if err := db.Model(&model.FIMPolicy{}).Count(&count).Error; err != nil {
		// 表可能不存在（首次启动 AutoMigrate 之前），静默跳过
		logger.Debug("FIM 策略表查询失败，跳过初始化", zap.Error(err))
		return nil
	}

	if count > 0 {
		logger.Debug("FIM 策略已存在，跳过默认策略初始化", zap.Int64("count", count))
		return nil
	}

	defaultPolicies := []model.FIMPolicy{
		{
			PolicyID:    "fim-default-general",
			Name:        "通用文件完整性策略",
			Description: "监控关键系统二进制文件、认证配置文件和SSH配置等，适用于所有主机",
			WatchPaths: model.WatchPaths{
				{Path: "/bin", Level: "NORMAL", Comment: "系统命令"},
				{Path: "/sbin", Level: "NORMAL", Comment: "系统管理命令"},
				{Path: "/usr/bin", Level: "NORMAL", Comment: "用户态命令"},
				{Path: "/usr/sbin", Level: "NORMAL", Comment: "用户态管理命令"},
				{Path: "/etc/passwd", Level: "NORMAL", Comment: "用户文件"},
				{Path: "/etc/shadow", Level: "NORMAL", Comment: "密码文件"},
				{Path: "/etc/group", Level: "NORMAL", Comment: "组文件"},
				{Path: "/etc/gshadow", Level: "NORMAL", Comment: "组密码文件"},
				{Path: "/etc/sudoers", Level: "NORMAL", Comment: "提权配置"},
				{Path: "/etc/ssh/sshd_config", Level: "NORMAL", Comment: "SSH 服务配置"},
				{Path: "/etc/ssh/ssh_config", Level: "NORMAL", Comment: "SSH 客户端配置"},
				{Path: "/etc/crontab", Level: "NORMAL", Comment: "定时任务"},
				{Path: "/etc/pam.d", Level: "NORMAL", Comment: "PAM 认证配置"},
			},
			ExcludePaths: model.StringArray{
				"/usr/src",
				"/usr/tmp",
				"/var/log",
				"/tmp",
				"/boot/grub2/grubenv",
			},
			CheckIntervalHours: 24,
			TargetType:         "all",
			Enabled:            true,
		},
		{
			PolicyID:    "fim-default-database",
			Name:        "数据库服务器策略",
			Description: "监控 MySQL/MariaDB、Redis、PostgreSQL 的配置文件和认证文件，防止数据库配置被篡改",
			WatchPaths: model.WatchPaths{
				{Path: "/etc/my.cnf", Level: "NORMAL", Comment: "MySQL 主配置"},
				{Path: "/etc/my.cnf.d", Level: "NORMAL", Comment: "MySQL 配置目录"},
				{Path: "/etc/mysql", Level: "NORMAL", Comment: "MySQL/MariaDB 配置目录"},
				{Path: "/etc/redis.conf", Level: "NORMAL", Comment: "Redis 主配置"},
				{Path: "/etc/redis", Level: "NORMAL", Comment: "Redis 配置目录"},
				{Path: "/etc/redis-sentinel.conf", Level: "NORMAL", Comment: "Redis Sentinel 配置"},
				{Path: "/var/lib/pgsql/data/pg_hba.conf", Level: "NORMAL", Comment: "PostgreSQL 认证配置"},
				{Path: "/var/lib/pgsql/data/postgresql.conf", Level: "NORMAL", Comment: "PostgreSQL 主配置"},
			},
			ExcludePaths: model.StringArray{
				"/var/lib/mysql",
				"/var/lib/redis",
				"/var/lib/pgsql/data/base",
				"/var/lib/pgsql/data/pg_wal",
			},
			CheckIntervalHours: 24,
			TargetType:         "all",
			Enabled:            false,
		},
		{
			PolicyID:    "fim-default-webserver",
			Name:        "Web 服务器策略",
			Description: "监控 Nginx/Apache/OpenResty 的配置文件和 SSL 证书，防止 Web 配置和证书被篡改",
			WatchPaths: model.WatchPaths{
				{Path: "/etc/nginx", Level: "NORMAL", Comment: "Nginx 配置目录"},
				{Path: "/usr/local/nginx/conf", Level: "NORMAL", Comment: "Nginx 自编译配置"},
				{Path: "/usr/local/openresty/nginx/conf", Level: "NORMAL", Comment: "OpenResty 配置"},
				{Path: "/etc/httpd/conf", Level: "NORMAL", Comment: "Apache 主配置"},
				{Path: "/etc/httpd/conf.d", Level: "NORMAL", Comment: "Apache 扩展配置"},
				{Path: "/etc/pki/tls/certs", Level: "NORMAL", Comment: "TLS 证书"},
				{Path: "/etc/pki/tls/private", Level: "NORMAL", Comment: "TLS 私钥"},
				{Path: "/etc/ssl/certs", Level: "NORMAL", Comment: "SSL 证书"},
				{Path: "/etc/ssl/private", Level: "NORMAL", Comment: "SSL 私钥"},
			},
			ExcludePaths: model.StringArray{
				"/usr/local/openresty/nginx/logs",
				"/usr/local/nginx/logs",
				"/var/log/nginx",
				"/var/log/httpd",
			},
			CheckIntervalHours: 24,
			TargetType:         "all",
			Enabled:            false,
		},
		{
			PolicyID:    "fim-default-container",
			Name:        "容器宿主机策略",
			Description: "监控 Docker/containerd 守护进程配置和运行时关键文件，防止容器运行环境被篡改",
			WatchPaths: model.WatchPaths{
				{Path: "/etc/docker/daemon.json", Level: "NORMAL", Comment: "Docker 守护进程配置"},
				{Path: "/etc/containerd", Level: "NORMAL", Comment: "containerd 配置目录"},
				{Path: "/usr/lib/systemd/system/docker.service", Level: "NORMAL", Comment: "Docker 服务单元"},
				{Path: "/usr/lib/systemd/system/containerd.service", Level: "NORMAL", Comment: "containerd 服务单元"},
				{Path: "/etc/crictl.yaml", Level: "NORMAL", Comment: "CRI 工具配置"},
			},
			ExcludePaths: model.StringArray{
				"/var/lib/docker",
				"/var/lib/containerd",
			},
			CheckIntervalHours: 24,
			TargetType:         "all",
			Enabled:            false,
		},
		{
			PolicyID:    "fim-default-middleware",
			Name:        "中间件与应用服务器策略",
			Description: "监控 Tomcat、Kafka、Zookeeper 等中间件的配置文件和启动脚本",
			WatchPaths: model.WatchPaths{
				{Path: "/etc/tomcat", Level: "NORMAL", Comment: "Tomcat 配置目录"},
				{Path: "/etc/kafka", Level: "NORMAL", Comment: "Kafka 配置目录"},
				{Path: "/etc/zookeeper", Level: "NORMAL", Comment: "Zookeeper 配置目录"},
				{Path: "/etc/elasticsearch", Level: "NORMAL", Comment: "Elasticsearch 配置目录"},
				{Path: "/usr/lib/systemd/system", Level: "NORMAL", Comment: "systemd 服务单元"},
				{Path: "/etc/init.d", Level: "NORMAL", Comment: "SysV 启动脚本"},
				{Path: "/etc/systemd/system", Level: "NORMAL", Comment: "自定义 systemd 服务"},
				{Path: "/etc/ld.so.conf", Level: "NORMAL", Comment: "动态链接库配置"},
				{Path: "/etc/ld.so.conf.d", Level: "NORMAL", Comment: "动态链接库配置目录"},
			},
			ExcludePaths: model.StringArray{
				"/var/log",
				"/var/lib/elasticsearch",
				"/var/lib/kafka-logs",
			},
			CheckIntervalHours: 24,
			TargetType:         "all",
			Enabled:            false,
		},
	}

	for _, policy := range defaultPolicies {
		wantEnabled := policy.Enabled
		if err := db.Create(&policy).Error; err != nil {
			return fmt.Errorf("创建默认 FIM 策略 %s 失败: %w", policy.PolicyID, err)
		}
		// GORM 对 bool 零值（false）会跳过并走 DB default(1)，需要显式更新
		if !wantEnabled {
			db.Model(&model.FIMPolicy{}).Where("policy_id = ?", policy.PolicyID).Update("enabled", false)
		}
		logger.Info("默认 FIM 策略初始化成功",
			zap.String("policy_id", policy.PolicyID),
			zap.String("name", policy.Name),
			zap.Bool("enabled", wantEnabled),
		)
	}

	return nil
}
