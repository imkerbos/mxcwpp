// Package migration 提供数据库初始化数据功能
package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/config"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
	"github.com/imkerbos/mxsec-platform/plugins/baseline/engine"
)

// DefaultPolicyGroupID 默认策略组ID
const DefaultPolicyGroupID = "system-baseline"

type managedPluginBootstrap struct {
	Name         string
	Type         model.PluginType
	RuntimeTypes model.StringArray
	Description  string
	Detail       string
}

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

	// 初始化默认组件（始终执行，确保组件列表完整）
	if err := initDefaultComponents(db, logger); err != nil {
		return fmt.Errorf("初始化默认组件失败: %w", err)
	}

	// 迁移 data/deps/ 下的旧依赖文件到 Component 表（仅运行一次）
	if err := migrateDepFiles(db, logger); err != nil {
		logger.Warn("迁移依赖文件失败", zap.Error(err))
	}

	// 初始化默认插件配置（始终执行，确保插件配置存在）
	if err := initDefaultPluginConfigs(db, logger, pluginsCfg); err != nil {
		return fmt.Errorf("初始化默认插件配置失败: %w", err)
	}

	// 初始化默认 FIM 策略（始终执行，仅在表为空时插入）
	if err := initDefaultFIMPolicies(db, logger); err != nil {
		return fmt.Errorf("初始化默认 FIM 策略失败: %w", err)
	}

	// 初始化内置检测规则（仅在表为空时插入）
	if err := initBuiltinDetectionRules(db, logger); err != nil {
		logger.Warn("初始化内置检测规则失败", zap.Error(err))
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
	err := db.Where("`key` = ? AND category = ?", "data_initialized", "system").First(&cfg).Error
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
	managedPlugins := []managedPluginBootstrap{
		{
			Name:         "baseline",
			Type:         model.PluginTypeBaseline,
			RuntimeTypes: model.StringArray{"vm"},
			Description:  "Linux 基线安全检查插件，执行操作系统安全配置检查",
			Detail:       `{"check_interval": 3600}`,
		},
		{
			Name:         "collector",
			Type:         model.PluginTypeCollector,
			RuntimeTypes: model.StringArray{"vm", "docker", "k8s"},
			Description:  "资产采集插件，采集主机进程、端口、用户等信息",
			Detail:       `{"collect_interval": 300}`,
		},
		{
			Name:         "fim",
			Type:         model.PluginTypeFIM,
			RuntimeTypes: model.StringArray{"vm"},
			Description:  "文件完整性监控插件，基于 AIDE 检测文件变更",
			Detail:       `{"check_timeout_minutes": 30}`,
		},
		{
			Name:         "scanner",
			Type:         model.PluginTypeScanner,
			RuntimeTypes: model.StringArray{"vm"},
			Description:  "病毒查杀插件，基于 ClamAV + YARA-X 双引擎检测恶意文件",
			Detail:       `{"quarantine_dir": "/var/mxsec/quarantine", "yara_rules_dir": "/var/mxsec/yara-rules"}`,
		},
		{
			Name:         "sensor",
			Type:         model.PluginTypeSensor,
			RuntimeTypes: model.StringArray{"vm"},
			Description:  "eBPF 实时监控插件，基于 Tetragon 采集进程/文件/网络事件",
			Detail:       `{"tetragon_socket": "/var/run/tetragon/tetragon.sock"}`,
		},
	}

	for _, plugin := range managedPlugins {
		if err := ensureManagedPluginConfig(db, logger, pluginsCfg, plugin); err != nil {
			return err
		}
	}

	return nil
}

func ensureManagedPluginConfig(db *gorm.DB, logger *zap.Logger, pluginsCfg *config.PluginsConfig, plugin managedPluginBootstrap) error {
	var existing model.PluginConfig
	err := db.Where("name = ?", plugin.Name).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return fmt.Errorf("检查插件配置 %s 失败: %w", plugin.Name, err)
	}
	hasExisting := err == nil

	version, pkg, found, err := findLatestUploadedPluginPackage(db, plugin.Name)
	if err != nil {
		return fmt.Errorf("查询插件 %s 上传包失败: %w", plugin.Name, err)
	}

	if !found {
		if hasExisting {
			updates := map[string]interface{}{
				"enabled":       false,
				"download_urls": model.StringArray{},
				"sha256":        "",
				"runtime_types": plugin.RuntimeTypes,
				"description":   plugin.Description,
				"detail":        plugin.Detail,
			}
			if err := db.Model(&existing).Updates(updates).Error; err != nil {
				return fmt.Errorf("禁用未上传插件配置 %s 失败: %w", plugin.Name, err)
			}
			logger.Info("插件尚未上传，已禁用历史插件配置",
				zap.String("name", plugin.Name),
				zap.String("current_version", existing.Version),
			)
		} else {
			logger.Debug("插件尚未上传，跳过创建默认插件配置",
				zap.String("name", plugin.Name),
			)
		}
		return nil
	}

	downloadURL := buildManagedPluginDownloadURL(pluginsCfg, plugin.Name)
	detail := fmt.Sprintf(`{"source":"component_upload","updated_at":"%s"}`, time.Now().Format(time.RFC3339))

	if !hasExisting {
		pluginConfig := model.PluginConfig{
			Name:         plugin.Name,
			Type:         plugin.Type,
			Version:      version.Version,
			SHA256:       pkg.SHA256,
			DownloadURLs: model.StringArray{downloadURL},
			RuntimeTypes: plugin.RuntimeTypes,
			Detail:       detail,
			Enabled:      true,
			Description:  plugin.Description,
		}
		if err := db.Create(&pluginConfig).Error; err != nil {
			return fmt.Errorf("创建插件配置 %s 失败: %w", plugin.Name, err)
		}
		logger.Info("根据已上传组件包创建插件配置",
			zap.String("name", plugin.Name),
			zap.String("version", version.Version),
			zap.String("download_url", downloadURL),
		)
		return nil
	}

	updates := map[string]interface{}{
		"type":          plugin.Type,
		"version":       version.Version,
		"sha256":        pkg.SHA256,
		"download_urls": model.StringArray{downloadURL},
		"runtime_types": plugin.RuntimeTypes,
		"detail":        detail,
		"enabled":       true,
		"description":   plugin.Description,
	}
	if err := db.Model(&existing).Updates(updates).Error; err != nil {
		return fmt.Errorf("更新插件配置 %s 失败: %w", plugin.Name, err)
	}
	logger.Info("根据已上传组件包同步插件配置",
		zap.String("name", plugin.Name),
		zap.String("version", version.Version),
		zap.String("download_url", downloadURL),
	)
	return nil
}

func findLatestUploadedPluginPackage(db *gorm.DB, pluginName string) (model.ComponentVersion, model.ComponentPackage, bool, error) {
	var component model.Component
	if err := db.Where("name = ? AND category = ?", pluginName, model.ComponentCategoryPlugin).First(&component).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.ComponentVersion{}, model.ComponentPackage{}, false, nil
		}
		return model.ComponentVersion{}, model.ComponentPackage{}, false, err
	}

	var version model.ComponentVersion
	if err := db.Where("component_id = ? AND is_latest = ?", component.ID, true).First(&version).Error; err != nil {
		if err := db.Where("component_id = ?", component.ID).Order("created_at DESC").First(&version).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return model.ComponentVersion{}, model.ComponentPackage{}, false, nil
			}
			return model.ComponentVersion{}, model.ComponentPackage{}, false, err
		}
	}

	var pkg model.ComponentPackage
	if err := db.Where("version_id = ? AND pkg_type = ? AND arch = ? AND enabled = ?",
		version.ID, model.PackageTypeBinary, "amd64", true).First(&pkg).Error; err != nil {
		if err := db.Where("version_id = ? AND pkg_type = ? AND enabled = ?",
			version.ID, model.PackageTypeBinary, true).First(&pkg).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return model.ComponentVersion{}, model.ComponentPackage{}, false, nil
			}
			return model.ComponentVersion{}, model.ComponentPackage{}, false, err
		}
	}

	info, err := os.Stat(pkg.FilePath)
	if err != nil || info.IsDir() {
		return model.ComponentVersion{}, model.ComponentPackage{}, false, nil
	}

	return version, pkg, true, nil
}

func buildManagedPluginDownloadURL(pluginsCfg *config.PluginsConfig, pluginName string) string {
	// 始终使用相对路径，由 AC 端根据 backend_url 动态拼接完整地址
	return fmt.Sprintf("/api/v1/plugins/download/%s", pluginName)
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

// builtinRuleYAML 内置规则 YAML 定义结构
type builtinRuleYAML struct {
	Rules []struct {
		Name        string   `mapstructure:"name"`
		Expression  string   `mapstructure:"expression"`
		Severity    string   `mapstructure:"severity"`
		Category    string   `mapstructure:"category"`
		MitreID     string   `mapstructure:"mitre_id"`
		DataTypes   []string `mapstructure:"data_types"`
		Description string   `mapstructure:"description"`
	} `mapstructure:"rules"`
}

// initBuiltinDetectionRules 初始化内置 CEL 检测规则
// 增量导入：按 name 去重，已存在的跳过，新规则自动补入
func initBuiltinDetectionRules(db *gorm.DB, logger *zap.Logger) error {
	// 查找 YAML 文件
	yamlPaths := []string{
		"configs/rules/builtin-rules.yaml",
		"/opt/mxsec-platform/configs/rules/builtin-rules.yaml",
	}

	var yamlData []byte
	var loadErr error
	for _, p := range yamlPaths {
		yamlData, loadErr = os.ReadFile(p)
		if loadErr == nil {
			logger.Info("加载内置规则文件", zap.String("path", p))
			break
		}
	}

	if yamlData == nil {
		return fmt.Errorf("未找到内置规则文件: %v", loadErr)
	}

	// 使用 viper 解析 YAML
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(yamlData))); err != nil {
		return fmt.Errorf("解析内置规则 YAML 失败: %w", err)
	}

	var rulesFile builtinRuleYAML
	if err := v.Unmarshal(&rulesFile); err != nil {
		return fmt.Errorf("反序列化内置规则失败: %w", err)
	}

	// 查询已存在的规则名称，用于跳过
	var existingNames []string
	db.Model(&model.DetectionRule{}).Pluck("name", &existingNames)
	nameSet := make(map[string]struct{}, len(existingNames))
	for _, n := range existingNames {
		nameSet[n] = struct{}{}
	}

	// 增量写入：仅插入不存在的规则
	imported := 0
	for _, r := range rulesFile.Rules {
		if _, exists := nameSet[r.Name]; exists {
			continue
		}
		rule := model.DetectionRule{
			Name:        r.Name,
			Expression:  r.Expression,
			Severity:    r.Severity,
			Category:    r.Category,
			MitreID:     r.MitreID,
			Description: r.Description,
			DataTypes:   model.StringArray(r.DataTypes),
			Enabled:     true,
		}
		if err := db.Create(&rule).Error; err != nil {
			logger.Warn("导入内置规则失败", zap.String("name", r.Name), zap.Error(err))
			continue
		}
		imported++
	}

	if imported > 0 {
		logger.Info("内置检测规则增量导入完成", zap.Int("new", imported), zap.Int("existing", len(existingNames)))
	} else {
		logger.Debug("内置检测规则已是最新", zap.Int("total", len(existingNames)))
	}
	return nil
}

// initDefaultComponents 初始化默认组件列表
// 确保所有预期组件在 components 表中存在，不存在则创建
func initDefaultComponents(db *gorm.DB, logger *zap.Logger) error {
	type componentDef struct {
		Name        string
		Category    model.ComponentCategory
		Description string
	}

	components := []componentDef{
		{Name: "agent", Category: model.ComponentCategoryAgent, Description: "矩阵云安全平台主机安全 Agent"},
		{Name: "baseline", Category: model.ComponentCategoryPlugin, Description: "Linux 基线安全检查插件，执行操作系统安全配置检查"},
		{Name: "collector", Category: model.ComponentCategoryPlugin, Description: "资产采集插件，采集主机进程、端口、用户等信息"},
		{Name: "fim", Category: model.ComponentCategoryPlugin, Description: "文件完整性监控插件，基于 AIDE 检测文件变更"},
		{Name: "scanner", Category: model.ComponentCategoryPlugin, Description: "病毒查杀插件，基于 ClamAV + YARA-X 双引擎检测恶意文件"},
		{Name: "sensor", Category: model.ComponentCategoryPlugin, Description: "eBPF 实时监控插件，基于 Tetragon 采集进程/文件/网络事件"},
		{Name: "virus-database", Category: model.ComponentCategoryPlugin, Description: "ClamAV 病毒特征库，由 freshclam 自动更新"},
		{Name: "tetragon", Category: model.ComponentCategoryDependency, Description: "Cilium Tetragon eBPF 运行时安全引擎"},
	}

	for _, c := range components {
		var existing model.Component
		err := db.Where("name = ?", c.Name).First(&existing).Error
		if err == nil {
			// 已存在，跳过
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("检查组件 %s 失败: %w", c.Name, err)
		}

		comp := model.Component{
			Name:        c.Name,
			Category:    c.Category,
			Description: c.Description,
			CreatedBy:   "system",
		}
		if err := db.Create(&comp).Error; err != nil {
			return fmt.Errorf("创建组件 %s 失败: %w", c.Name, err)
		}
		logger.Info("初始化组件成功", zap.String("name", c.Name), zap.String("category", string(c.Category)))
	}

	return nil
}

// depFileNamePattern 解析 {name}-v{version}-{arch}.tar.gz
var depFileNamePattern = regexp.MustCompile(`^([a-zA-Z0-9_-]+)-v([0-9][0-9a-zA-Z.\-]*)-(amd64|arm64)\.tar\.gz$`)

// migrateDepFiles 将 data/deps/ 下的旧依赖 tar.gz 文件迁移到 Component/Version/Package 模型
// 幂等：仅当 DB 中该版本的包记录不存在时才插入
func migrateDepFiles(db *gorm.DB, logger *zap.Logger) error {
	depsRoot := filepath.Join("data", "deps")
	if _, err := os.Stat(depsRoot); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(depsRoot)
	if err != nil {
		return fmt.Errorf("读取 data/deps 失败: %w", err)
	}

	uploadsRoot := "uploads"
	migrated := 0

	for _, dirEntry := range entries {
		if !dirEntry.IsDir() {
			continue
		}
		depName := dirEntry.Name()

		// 查找对应的依赖组件
		var component model.Component
		if err := db.Where("name = ? AND category = ?", depName, model.ComponentCategoryDependency).First(&component).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				logger.Info("跳过未登记的依赖目录", zap.String("name", depName))
				continue
			}
			logger.Warn("查询依赖组件失败", zap.String("name", depName), zap.Error(err))
			continue
		}

		depDir := filepath.Join(depsRoot, depName)
		files, err := os.ReadDir(depDir)
		if err != nil {
			logger.Warn("读取依赖目录失败", zap.String("dir", depDir), zap.Error(err))
			continue
		}

		// 按版本号组织 {version} -> []{arch, srcPath, fileName}
		type pkgItem struct {
			arch     string
			srcPath  string
			fileName string
		}
		versionMap := make(map[string][]pkgItem)

		for _, f := range files {
			if f.IsDir() {
				continue
			}
			m := depFileNamePattern.FindStringSubmatch(f.Name())
			if m == nil {
				continue
			}
			name, ver, arch := m[1], m[2], m[3]
			if name != depName {
				continue
			}
			versionMap[ver] = append(versionMap[ver], pkgItem{
				arch:     arch,
				srcPath:  filepath.Join(depDir, f.Name()),
				fileName: f.Name(),
			})
		}

		if len(versionMap) == 0 {
			continue
		}

		// 创建版本和包记录
		for ver, items := range versionMap {
			var compVersion model.ComponentVersion
			err := db.Where("component_id = ? AND version = ?", component.ID, ver).First(&compVersion).Error
			if err == gorm.ErrRecordNotFound {
				compVersion = model.ComponentVersion{
					ComponentID: component.ID,
					Version:     ver,
					Changelog:   "从 data/deps 自动迁移",
					IsLatest:    true,
					CreatedBy:   "system",
				}
				if err := db.Create(&compVersion).Error; err != nil {
					logger.Warn("创建依赖版本失败",
						zap.String("name", depName), zap.String("version", ver), zap.Error(err))
					continue
				}
			} else if err != nil {
				logger.Warn("查询依赖版本失败", zap.Error(err))
				continue
			}

			// 准备目标目录
			dstDir := filepath.Join(uploadsRoot, "packages", depName, ver)
			if err := os.MkdirAll(dstDir, 0755); err != nil {
				logger.Warn("创建目标目录失败", zap.String("dir", dstDir), zap.Error(err))
				continue
			}

			for _, it := range items {
				// 检查是否已存在相同包记录
				var existing model.ComponentPackage
				err := db.Where("version_id = ? AND pkg_type = ? AND arch = ?",
					compVersion.ID, string(model.PackageTypeTGZ), it.arch).First(&existing).Error
				if err == nil {
					// 已有记录：确保文件路径存在即可，不重复操作
					continue
				}
				if err != gorm.ErrRecordNotFound {
					logger.Warn("查询依赖包失败", zap.Error(err))
					continue
				}

				dstFile := filepath.Join(dstDir, it.fileName)

				// 移动文件
				if err := os.Rename(it.srcPath, dstFile); err != nil {
					// 跨设备失败时尝试复制
					if copyErr := copyFile(it.srcPath, dstFile); copyErr != nil {
						logger.Warn("迁移文件失败", zap.String("src", it.srcPath), zap.Error(copyErr))
						continue
					}
					_ = os.Remove(it.srcPath)
				}

				// 计算 SHA256 与 大小
				sum, size, err := hashAndSize(dstFile)
				if err != nil {
					logger.Warn("计算文件哈希失败", zap.String("file", dstFile), zap.Error(err))
					continue
				}

				pkg := model.ComponentPackage{
					VersionID:  compVersion.ID,
					OS:         "linux",
					Arch:       it.arch,
					PkgType:    model.PackageTypeTGZ,
					FilePath:   dstFile,
					FileName:   it.fileName,
					FileSize:   size,
					SHA256:     sum,
					Enabled:    true,
					UploadedBy: "system",
				}
				if err := db.Create(&pkg).Error; err != nil {
					logger.Warn("创建依赖包记录失败", zap.Error(err))
					continue
				}
				migrated++
				logger.Info("迁移依赖包成功",
					zap.String("name", depName),
					zap.String("version", ver),
					zap.String("arch", it.arch),
					zap.String("dst", dstFile),
				)
			}
		}

		// 尝试清理空的目录
		if remain, _ := os.ReadDir(depDir); len(remain) == 0 {
			_ = os.Remove(depDir)
		}
	}

	// 如果 data/deps 整个空了，顺便清理
	if remain, _ := os.ReadDir(depsRoot); len(remain) == 0 {
		_ = os.Remove(depsRoot)
	}

	if migrated > 0 {
		logger.Info("依赖文件迁移完成", zap.Int("migrated_packages", migrated))
	}
	return nil
}

// hashAndSize 计算文件 SHA256 和大小
func hashAndSize(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// copyFile 复制文件内容
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
