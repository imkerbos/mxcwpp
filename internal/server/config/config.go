// Package config 提供 Server 配置管理
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Config 是 Server 配置结构
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	MTLS     MTLSConfig     `mapstructure:"mtls"`
	Log      LogConfig      `mapstructure:"log"`
	Agent    AgentConfig    `mapstructure:"agent"`
	Metrics  MetricsConfig  `mapstructure:"metrics"`
	Plugins  PluginsConfig  `mapstructure:"plugins"`
}

// PluginsConfig 是插件配置
type PluginsConfig struct {
	// 插件存放目录（本地文件路径）
	Dir string `mapstructure:"dir"`
	// 插件下载基础 URL（Agent 用于下载插件的 URL 前缀）
	// 例如: http://192.168.8.140:8080/api/v1/plugins/download
	// 如果为空，则使用 file:// 协议（仅限开发环境）
	BaseURL string `mapstructure:"base_url"`
}

// ServerConfig 是服务器配置
type ServerConfig struct {
	GRPC      GRPCConfig `mapstructure:"grpc"`
	HTTP      HTTPConfig `mapstructure:"http"`
	JWTSecret string     `mapstructure:"jwt_secret"` // JWT 密钥，用于生成和验证 Token
}

// GRPCConfig 是 gRPC 服务配置
type GRPCConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// Address 返回 gRPC 服务地址
func (c GRPCConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// HTTPConfig 是 HTTP 服务配置
type HTTPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// Address 返回 HTTP 服务地址
func (c HTTPConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// DatabaseConfig 是数据库配置
type DatabaseConfig struct {
	Type     string         `mapstructure:"type"`
	MySQL    MySQLConfig    `mapstructure:"mysql"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

// MySQLConfig 是 MySQL 配置
type MySQLConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Database        string        `mapstructure:"database"`
	Charset         string        `mapstructure:"charset"`
	ParseTime       bool          `mapstructure:"parse_time"`
	Loc             string        `mapstructure:"loc"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// DSN 返回 MySQL DSN
func (c MySQLConfig) DSN() string {
	// URL 编码 loc 参数（如 Asia/Shanghai 需要编码为 Asia%2FShanghai）
	loc := c.Loc
	if loc == "" {
		loc = "Local"
	}
	// 使用 url.QueryEscape 编码 loc 参数
	locEncoded := url.QueryEscape(loc)

	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=%v&loc=%s",
		c.User, c.Password, c.Host, c.Port, c.Database, c.Charset, c.ParseTime, locEncoded)
}

// PostgresConfig 是 PostgreSQL 配置
type PostgresConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Database        string        `mapstructure:"database"`
	SSLMode         string        `mapstructure:"sslmode"`
	Timezone        string        `mapstructure:"timezone"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// DSN 返回 PostgreSQL DSN
func (c PostgresConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		c.Host, c.Port, c.User, c.Password, c.Database, c.SSLMode, c.Timezone)
}

// MTLSConfig 是 mTLS 配置
type MTLSConfig struct {
	CACert     string `mapstructure:"ca_cert"`
	ServerCert string `mapstructure:"server_cert"`
	ServerKey  string `mapstructure:"server_key"`
}

// LogConfig 是日志配置
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	File   string `mapstructure:"file"`
	MaxAge int    `mapstructure:"max_age"`
}

// AgentConfig 是 Agent 配置（下发到 Agent）
type AgentConfig struct {
	HeartbeatInterval int    `mapstructure:"heartbeat_interval"`
	WorkDir           string `mapstructure:"work_dir"`
}

// MetricsConfig 是监控指标配置
type MetricsConfig struct {
	// MySQL 存储配置
	MySQL MySQLMetricsConfig `mapstructure:"mysql"`
	// Prometheus 配置
	Prometheus PrometheusConfig `mapstructure:"prometheus"`
}

// MySQLMetricsConfig 是 MySQL 监控指标存储配置
type MySQLMetricsConfig struct {
	Enabled       bool          `mapstructure:"enabled"`        // 是否启用 MySQL 存储
	RetentionDays int           `mapstructure:"retention_days"` // 数据保留天数（默认 30 天）
	BatchSize     int           `mapstructure:"batch_size"`     // 批量插入大小（默认 100）
	FlushInterval time.Duration `mapstructure:"flush_interval"` // 刷新间隔（默认 5 秒）
}

// PrometheusConfig 是 Prometheus 配置
type PrometheusConfig struct {
	Enabled        bool   `mapstructure:"enabled"`          // 是否启用 Prometheus 远程写入
	RemoteWriteURL string `mapstructure:"remote_write_url"` // Prometheus Remote Write API URL
	QueryURL       string `mapstructure:"query_url"`        // Prometheus Query API URL（用于查询，如果为空则从 remote_write_url 提取）
	// 如果使用 Pushgateway
	PushgatewayURL string        `mapstructure:"pushgateway_url"` // Pushgateway URL（可选，与 remote_write_url 二选一）
	JobName        string        `mapstructure:"job_name"`        // Job 名称（默认 "mxsec-platform"）
	Timeout        time.Duration `mapstructure:"timeout"`         // 请求超时（默认 10 秒）
}

// Load 加载配置文件
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// 设置配置文件路径
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// 默认查找配置文件
		v.SetConfigName("server")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./configs")
		v.AddConfigPath("/etc/mxsec-server")
	}

	// 设置环境变量支持
	v.SetEnvPrefix("BLS_SERVER")
	v.AutomaticEnv()

	// 读取配置文件
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// 配置文件未找到，使用默认配置
			return loadDefaults(), nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析配置
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 检查 log.file 是否在配置文件中明确设置
	// 如果配置文件中设置了 file: ""，Viper 会将其解析为空字符串
	// 如果配置文件中没有 file 字段，Viper 不会设置这个字段，我们需要设置默认值
	logFileSet := v.IsSet("log.file")

	// 设置默认值
	setDefaults(&cfg, logFileSet)

	return &cfg, nil
}

// loadDefaults 加载默认配置
func loadDefaults() *Config {
	cfg := &Config{}
	setDefaults(cfg, false) // 没有配置文件，使用所有默认值
	return cfg
}

// setDefaults 设置默认值
// logFileSet 表示 log.file 是否在配置文件中明确设置（包括设置为空字符串）
func setDefaults(cfg *Config, logFileSet bool) {
	// Server 默认配置
	if cfg.Server.GRPC.Host == "" {
		cfg.Server.GRPC.Host = "0.0.0.0"
	}
	if cfg.Server.GRPC.Port == 0 {
		cfg.Server.GRPC.Port = 6751
	}
	if cfg.Server.HTTP.Host == "" {
		cfg.Server.HTTP.Host = "0.0.0.0"
	}
	if cfg.Server.HTTP.Port == 0 {
		cfg.Server.HTTP.Port = 8080
	}

	// 数据库默认配置
	if cfg.Database.Type == "" {
		cfg.Database.Type = "mysql"
	}
	if cfg.Database.MySQL.Host == "" {
		cfg.Database.MySQL.Host = "localhost"
	}
	if cfg.Database.MySQL.Port == 0 {
		cfg.Database.MySQL.Port = 3306
	}
	if cfg.Database.MySQL.Charset == "" {
		cfg.Database.MySQL.Charset = "utf8mb4"
	}
	if cfg.Database.MySQL.MaxIdleConns == 0 {
		cfg.Database.MySQL.MaxIdleConns = 10
	}
	if cfg.Database.MySQL.MaxOpenConns == 0 {
		cfg.Database.MySQL.MaxOpenConns = 100
	}
	if cfg.Database.MySQL.ConnMaxLifetime == 0 {
		cfg.Database.MySQL.ConnMaxLifetime = time.Hour
	}

	// 日志默认配置
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "json"
	}
	// 只有在配置文件中没有明确设置 log.file 字段时，才设置默认值
	// 如果配置文件中设置了 file: ""，表示明确禁用文件日志，不设置默认值
	if !logFileSet && cfg.Log.File == "" {
		cfg.Log.File = "/var/log/mxsec-server/server.log"
	}
	if cfg.Log.MaxAge == 0 {
		cfg.Log.MaxAge = 30
	}

	// Agent 默认配置
	if cfg.Agent.HeartbeatInterval == 0 {
		cfg.Agent.HeartbeatInterval = 60
	}
	if cfg.Agent.WorkDir == "" {
		cfg.Agent.WorkDir = "/var/lib/mxsec-agent"
	}

	// Metrics 默认配置
	// 默认使用 MySQL 存储（如果未启用 Prometheus）
	if !cfg.Metrics.Prometheus.Enabled {
		// 默认启用 MySQL 存储
		cfg.Metrics.MySQL.Enabled = true
		if cfg.Metrics.MySQL.RetentionDays == 0 {
			cfg.Metrics.MySQL.RetentionDays = 30
		}
		if cfg.Metrics.MySQL.BatchSize == 0 {
			cfg.Metrics.MySQL.BatchSize = 100
		}
		if cfg.Metrics.MySQL.FlushInterval == 0 {
			cfg.Metrics.MySQL.FlushInterval = 5 * time.Second
		}
	} else {
		// 如果启用 Prometheus，则禁用 MySQL
		cfg.Metrics.MySQL.Enabled = false
		// Prometheus 配置默认值
		if cfg.Metrics.Prometheus.JobName == "" {
			cfg.Metrics.Prometheus.JobName = "mxsec-platform"
		}
		if cfg.Metrics.Prometheus.Timeout == 0 {
			cfg.Metrics.Prometheus.Timeout = 10 * time.Second
		}
	}

	// Plugins 默认配置
	if cfg.Plugins.Dir == "" {
		cfg.Plugins.Dir = "/workspace/dist/plugins" // Docker 开发环境默认路径
	}
	// BaseURL 为空时，表示使用 file:// 协议（开发环境）
}

// Validate 验证配置
func (c *Config) Validate() error {
	// 验证 mTLS 证书文件
	if c.MTLS.CACert != "" {
		if _, err := os.Stat(c.MTLS.CACert); os.IsNotExist(err) {
			return fmt.Errorf("CA 证书文件不存在: %s", c.MTLS.CACert)
		}
	}
	if c.MTLS.ServerCert != "" {
		if _, err := os.Stat(c.MTLS.ServerCert); os.IsNotExist(err) {
			return fmt.Errorf("Server 证书文件不存在: %s", c.MTLS.ServerCert)
		}
	}
	if c.MTLS.ServerKey != "" {
		if _, err := os.Stat(c.MTLS.ServerKey); os.IsNotExist(err) {
			return fmt.Errorf("Server 私钥文件不存在: %s", c.MTLS.ServerKey)
		}
	}

	// 验证 Prometheus 配置
	if c.Metrics.Prometheus.Enabled {
		if c.Metrics.Prometheus.RemoteWriteURL == "" && c.Metrics.Prometheus.PushgatewayURL == "" {
			return fmt.Errorf("Prometheus 已启用但未配置 URL，请配置 remote_write_url 或 pushgateway_url")
		}
	}

	// 验证日志目录（仅在配置了日志文件时）
	// 如果 Log.File 为空字符串，表示不写文件，只输出到控制台，不需要创建目录
	if c.Log.File != "" {
		logDir := filepath.Dir(c.Log.File)
		// 检查是否是绝对路径且指向系统目录（需要权限）
		// 如果是相对路径，会在当前工作目录创建，不需要特殊权限
		if filepath.IsAbs(logDir) {
			// 对于系统目录（如 /var/log），尝试创建，如果失败则返回错误
			if err := os.MkdirAll(logDir, 0755); err != nil {
				return fmt.Errorf("创建日志目录失败: %w (提示: 开发环境建议使用相对路径或设置 file: \"\" 禁用文件日志)", err)
			}
		} else {
			// 相对路径，在当前工作目录创建，通常不会有权限问题
			if err := os.MkdirAll(logDir, 0755); err != nil {
				return fmt.Errorf("创建日志目录失败: %w", err)
			}
		}
	}

	return nil
}

// LogInfo 记录配置信息（隐藏敏感信息）
func (c *Config) LogInfo(logger *zap.Logger) {
	logger.Info("配置加载完成",
		zap.String("grpc_address", c.Server.GRPC.Address()),
		zap.String("http_address", c.Server.HTTP.Address()),
		zap.String("database_type", c.Database.Type),
		zap.String("log_level", c.Log.Level),
		zap.String("log_format", c.Log.Format),
		zap.String("log_file", c.Log.File),
	)
}
