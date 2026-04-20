package cluster

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

type RenderOptions struct {
	ConfigPath string
	OutputDir  string
	RepoRoot   string
	Clean      bool
}

type RenderResult struct {
	ClusterDir  string
	NodeBundles []NodeBundle
}

type NodeBundle struct {
	Node       Node
	Assignment RoleAssignment
	BundleDir  string
}

type nodeTemplateData struct {
	ClusterName      string
	Environment      string
	Version          string
	Config           *Config
	Node             Node
	Assignment       RoleAssignment
	InstallDir       string
	DataRoot         string
	Timezone         string
	Network          Network
	App              App
	ControlPlane     ControlPlane
	ManagerImage     string
	AgentCenterImage string
	ConsumerImage    string
	UIImage          string
	MySQLImage       string
	RedisImage       string
	ClickHouseImage  string
	KafkaImage       string
	MySQLPort        int
	RedisPort        int
	ClickHouseHTTP   int
	ClickHouseTCP    int
	KafkaPorts       []int
	KafkaHost        string
	KafkaClusterID   string
}

type serverConfigDoc struct {
	Server     serverDoc     `yaml:"server"`
	Database   databaseDoc   `yaml:"database"`
	Redis      redisDoc      `yaml:"redis"`
	Kafka      kafkaDoc      `yaml:"kafka"`
	ClickHouse clickHouseDoc `yaml:"clickhouse"`
	Metrics    metricsDoc    `yaml:"metrics"`
	MTLS       mtlsDoc       `yaml:"mtls"`
	Log        logDoc        `yaml:"log"`
	Agent      agentDoc      `yaml:"agent"`
	Plugins    pluginsDoc    `yaml:"plugins"`
}

type serverDoc struct {
	GRPC        endpointDoc `yaml:"grpc"`
	HTTP        endpointDoc `yaml:"http"`
	JWTSecret   string      `yaml:"jwt_secret"`
	ManagerAddr string      `yaml:"manager_addr"`
	InstanceID  string      `yaml:"instance_id"`
}

type endpointDoc struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type databaseDoc struct {
	Type  string   `yaml:"type"`
	MySQL mysqlDoc `yaml:"mysql"`
}

type mysqlDoc struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	Database        string `yaml:"database"`
	Charset         string `yaml:"charset"`
	ParseTime       bool   `yaml:"parse_time"`
	Loc             string `yaml:"loc"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	ConnMaxLifetime string `yaml:"conn_max_lifetime"`
}

type redisDoc struct {
	Addr          string   `yaml:"addr"`
	Password      string   `yaml:"password"`
	DB            int      `yaml:"db"`
	PoolSize      int      `yaml:"pool_size"`
	Sentinel      bool     `yaml:"sentinel"`
	MasterName    string   `yaml:"master_name"`
	SentinelAddrs []string `yaml:"sentinel_addrs"`
}

type kafkaDoc struct {
	Enabled     bool         `yaml:"enabled"`
	Brokers     []string     `yaml:"brokers"`
	TopicPrefix string       `yaml:"topic_prefix"`
	Producer    kafkaProdDoc `yaml:"producer"`
}

type kafkaProdDoc struct {
	RequiredAcks int `yaml:"required_acks"`
}

type clickHouseDoc struct {
	Enabled         bool     `yaml:"enabled"`
	Addrs           []string `yaml:"addrs"`
	Database        string   `yaml:"database"`
	Username        string   `yaml:"username"`
	Password        string   `yaml:"password"`
	MaxOpenConns    int      `yaml:"max_open_conns"`
	MaxIdleConns    int      `yaml:"max_idle_conns"`
	ConnMaxLifetime string   `yaml:"conn_max_lifetime"`
	BatchSize       int      `yaml:"batch_size"`
	FlushTimeout    string   `yaml:"flush_timeout"`
}

type metricsDoc struct {
	Prometheus prometheusDoc `yaml:"prometheus"`
}

type prometheusDoc struct {
	Enabled  bool   `yaml:"enabled"`
	QueryURL string `yaml:"query_url"`
	Timeout  string `yaml:"timeout"`
}

type mtlsDoc struct {
	CACert     string `yaml:"ca_cert"`
	ServerCert string `yaml:"server_cert"`
	ServerKey  string `yaml:"server_key"`
}

type logDoc struct {
	Level     string `yaml:"level"`
	Format    string `yaml:"format"`
	File      string `yaml:"file"`
	ErrorFile string `yaml:"error_file"`
	MaxAge    int    `yaml:"max_age"`
}

type agentDoc struct {
	HeartbeatInterval int    `yaml:"heartbeat_interval"`
	WorkDir           string `yaml:"work_dir"`
}

type pluginsDoc struct {
	Dir     string `yaml:"dir"`
	BaseURL string `yaml:"base_url"`
}

func RenderCluster(cfg *Config, opts RenderOptions) (*RenderResult, error) {
	clusterDir := filepath.Join(opts.OutputDir, cfg.Metadata.Name)
	if opts.Clean {
		if err := os.RemoveAll(clusterDir); err != nil {
			return nil, fmt.Errorf("清理旧渲染目录失败: %w", err)
		}
	}
	if err := os.MkdirAll(clusterDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建渲染目录失败: %w", err)
	}
	if err := WriteResolvedConfig(filepath.Join(clusterDir, "resolved-cluster.yaml"), cfg); err != nil {
		return nil, err
	}

	certs, err := GenerateCertificates(cfg)
	if err != nil {
		return nil, err
	}

	result := &RenderResult{ClusterDir: clusterDir}
	for _, assignment := range cfg.RoleAssignments() {
		bundleDir := filepath.Join(clusterDir, "nodes", assignment.Node.Name)
		if err := renderNodeBundle(cfg, assignment, certs, opts.RepoRoot, bundleDir); err != nil {
			return nil, fmt.Errorf("渲染节点 %s 失败: %w", assignment.Node.Name, err)
		}
		result.NodeBundles = append(result.NodeBundles, NodeBundle{
			Node:       assignment.Node,
			Assignment: assignment,
			BundleDir:  bundleDir,
		})
	}
	return result, nil
}

func renderNodeBundle(cfg *Config, assignment RoleAssignment, certs *CertificateBundle, repoRoot, bundleDir string) error {
	for _, dir := range []string{
		bundleDir,
		filepath.Join(bundleDir, "compose"),
		filepath.Join(bundleDir, "config"),
		filepath.Join(bundleDir, "scripts"),
		filepath.Join(bundleDir, "deploy"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if assignment.Node.HasRole(RoleControl) {
		if err := os.MkdirAll(filepath.Join(bundleDir, "certs"), 0o755); err != nil {
			return err
		}
	}

	data := nodeTemplateData{
		ClusterName:      cfg.Metadata.Name,
		Environment:      cfg.Metadata.Environment,
		Version:          cfg.Release.Version,
		Config:           cfg,
		Node:             assignment.Node,
		Assignment:       assignment,
		InstallDir:       assignment.Node.InstallDir,
		DataRoot:         assignment.Node.DataRoot,
		Timezone:         cfg.Release.Timezone,
		Network:          cfg.Network,
		App:              cfg.App,
		ControlPlane:     cfg.ControlPlane,
		ManagerImage:     cfg.ImageRef("mxsec-manager"),
		AgentCenterImage: cfg.ImageRef("mxsec-agentcenter"),
		ConsumerImage:    cfg.ImageRef("mxsec-consumer"),
		UIImage:          cfg.ImageRef("mxsec-ui"),
		MySQLImage:       "mysql:8.0",
		RedisImage:       "redis:7-alpine",
		ClickHouseImage:  "clickhouse/clickhouse-server:24-alpine",
		KafkaImage:       "confluentinc/cp-kafka:7.5.0",
		MySQLPort:        cfg.Infrastructure.MySQL.Port,
		RedisPort:        cfg.Infrastructure.Redis.Port,
		ClickHouseHTTP:   cfg.Infrastructure.ClickHouse.HTTPPort,
		ClickHouseTCP:    cfg.Infrastructure.ClickHouse.TCPPort,
		KafkaPorts:       cfg.Infrastructure.Kafka.BrokerPorts,
		KafkaHost:        cfg.KafkaHost(),
		KafkaClusterID:   kafkaClusterID(cfg),
	}

	installScript := filepath.Join(repoRoot, "scripts", "prod", "install-deps.sh")
	if err := copyFile(installScript, filepath.Join(bundleDir, "scripts", "install-deps.sh"), 0o755); err != nil {
		return err
	}
	if assignment.Node.HasRole(RoleStorage) {
		if err := copyFile(filepath.Join(repoRoot, "deploy", "init.sql"), filepath.Join(bundleDir, "deploy", "init.sql"), 0o644); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(repoRoot, "deploy", "init-clickhouse.sql"), filepath.Join(bundleDir, "deploy", "init-clickhouse.sql"), 0o644); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(repoRoot, "deploy", "config", "mysql.cnf"), filepath.Join(bundleDir, "config", "mysql.cnf"), 0o644); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(repoRoot, "deploy", "config", "clickhouse.xml"), filepath.Join(bundleDir, "config", "clickhouse.xml"), 0o644); err != nil {
			return err
		}
		if err := renderTemplateFile(filepath.Join(repoRoot, "deploy", "prod", "templates", "docker-compose.storage.yml.tmpl"), filepath.Join(bundleDir, "compose", "docker-compose.storage.yml"), data, 0o644); err != nil {
			return err
		}
	}
	if assignment.Node.HasRole(RoleKafka) {
		if err := renderTemplateFile(filepath.Join(repoRoot, "deploy", "prod", "templates", "docker-compose.kafka.yml.tmpl"), filepath.Join(bundleDir, "compose", "docker-compose.kafka.yml"), data, 0o644); err != nil {
			return err
		}
	}
	if assignment.Node.HasRole(RoleControl) {
		if err := renderTemplateFile(filepath.Join(repoRoot, "deploy", "prod", "templates", "docker-compose.control.yml.tmpl"), filepath.Join(bundleDir, "compose", "docker-compose.control.yml"), data, 0o644); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(repoRoot, "deploy", "config", "nginx.conf"), filepath.Join(bundleDir, "config", "nginx.conf"), 0o644); err != nil {
			return err
		}
		if err := writeServerConfig(filepath.Join(bundleDir, "config", "server.yaml"), cfg, assignment); err != nil {
			return err
		}
		if err := writeControlCerts(bundleDir, certs); err != nil {
			return err
		}
	}
	if err := writeNodeSummary(filepath.Join(bundleDir, "README.txt"), cfg, assignment); err != nil {
		return err
	}
	return nil
}

func renderTemplateFile(tmplPath, outputPath string, data any, mode os.FileMode) error {
	content, err := os.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("读取模板失败 %s: %w", tmplPath, err)
	}
	tmpl, err := template.New(filepath.Base(tmplPath)).Funcs(template.FuncMap{
		"join": strings.Join,
	}).Parse(string(content))
	if err != nil {
		return fmt.Errorf("解析模板失败 %s: %w", tmplPath, err)
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("创建输出文件失败 %s: %w", outputPath, err)
	}
	defer file.Close()
	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("渲染模板失败 %s: %w", tmplPath, err)
	}
	return nil
}

func writeServerConfig(path string, cfg *Config, assignment RoleAssignment) error {
	doc := serverConfigDoc{
		Server: serverDoc{
			GRPC:        endpointDoc{Host: "0.0.0.0", Port: cfg.App.GRPCPort},
			HTTP:        endpointDoc{Host: "0.0.0.0", Port: cfg.App.ManagerHTTPPort},
			JWTSecret:   cfg.App.JWTSecret,
			ManagerAddr: "http://manager:8080",
			InstanceID:  assignment.Node.Name,
		},
		Database: databaseDoc{
			Type: "mysql",
			MySQL: mysqlDoc{
				Host:            cfg.MySQLHost(),
				Port:            cfg.Infrastructure.MySQL.Port,
				User:            cfg.Infrastructure.MySQL.User,
				Password:        cfg.Infrastructure.MySQL.Password,
				Database:        cfg.Infrastructure.MySQL.Database,
				Charset:         "utf8mb4",
				ParseTime:       true,
				Loc:             cfg.Release.Timezone,
				MaxIdleConns:    20,
				MaxOpenConns:    200,
				ConnMaxLifetime: "1h",
			},
		},
		Redis: redisDoc{
			Addr:          fmt.Sprintf("%s:%d", cfg.RedisHost(), cfg.Infrastructure.Redis.Port),
			Password:      cfg.Infrastructure.Redis.Password,
			DB:            cfg.Infrastructure.Redis.DB,
			PoolSize:      100,
			Sentinel:      false,
			MasterName:    "mymaster",
			SentinelAddrs: []string{"", "", ""},
		},
		Kafka: kafkaDoc{
			Enabled:     cfg.Infrastructure.Kafka.Enabled,
			Brokers:     cfg.KafkaBrokerEndpoints(),
			TopicPrefix: cfg.Infrastructure.Kafka.TopicPrefix,
			Producer: kafkaProdDoc{
				RequiredAcks: -1,
			},
		},
		ClickHouse: clickHouseDoc{
			Enabled:         true,
			Addrs:           []string{fmt.Sprintf("%s:%d", cfg.ClickHouseHost(), cfg.Infrastructure.ClickHouse.TCPPort)},
			Database:        cfg.Infrastructure.ClickHouse.Database,
			Username:        cfg.Infrastructure.ClickHouse.User,
			Password:        cfg.Infrastructure.ClickHouse.Password,
			MaxOpenConns:    20,
			MaxIdleConns:    5,
			ConnMaxLifetime: "1h",
			BatchSize:       10000,
			FlushTimeout:    "5s",
		},
		Metrics: metricsDoc{
			Prometheus: prometheusDoc{
				Enabled:  cfg.App.PrometheusEnabled,
				QueryURL: cfg.App.PrometheusQueryURL,
				Timeout:  cfg.App.PrometheusTimeout,
			},
		},
		MTLS: mtlsDoc{
			CACert:     "/etc/mxsec-platform/certs/ca.crt",
			ServerCert: "/etc/mxsec-platform/certs/server.crt",
			ServerKey:  "/etc/mxsec-platform/certs/server.key",
		},
		Log: logDoc{
			Level:     cfg.App.LogLevel,
			Format:    cfg.App.LogFormat,
			File:      "/var/log/mxsec-platform/server.log",
			ErrorFile: "/var/log/mxsec-platform/error.log",
			MaxAge:    7,
		},
		Agent: agentDoc{
			HeartbeatInterval: cfg.App.HeartbeatInterval,
			WorkDir:           "/var/lib/mxsec-agent",
		},
		Plugins: pluginsDoc{
			Dir:     "/opt/mxsec-platform/plugins",
			BaseURL: cfg.PluginsBaseURL(),
		},
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化 server.yaml 失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入 server.yaml 失败: %w", err)
	}
	return nil
}

func writeControlCerts(bundleDir string, certs *CertificateBundle) error {
	files := map[string][]byte{
		"ca.crt":     certs.CACert,
		"ca.key":     certs.CAKey,
		"server.crt": certs.ServerCert,
		"server.key": certs.ServerKey,
		"agent.crt":  certs.AgentCert,
		"agent.key":  certs.AgentKey,
		"client.crt": certs.ClientCert,
		"client.key": certs.ClientKey,
	}
	for name, content := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".key") {
			mode = 0o600
		}
		if err := os.WriteFile(filepath.Join(bundleDir, "certs", name), content, mode); err != nil {
			return fmt.Errorf("写入证书文件失败 %s: %w", name, err)
		}
	}
	return nil
}

func writeNodeSummary(path string, cfg *Config, assignment RoleAssignment) error {
	var builder strings.Builder
	builder.WriteString("MxSec prod bundle\n")
	builder.WriteString(fmt.Sprintf("cluster: %s\n", cfg.Metadata.Name))
	builder.WriteString(fmt.Sprintf("node: %s (%s)\n", assignment.Node.Name, assignment.Node.Host))
	builder.WriteString(fmt.Sprintf("roles: %s\n", strings.Join(assignment.Roles, ",")))
	if assignment.Node.HasRole(RoleControl) {
		builder.WriteString(fmt.Sprintf("manager replicas: %d\n", assignment.ManagerReplicas))
		builder.WriteString(fmt.Sprintf("agentcenter replicas: %d\n", assignment.AgentCenterReplicas))
		builder.WriteString(fmt.Sprintf("consumer replicas: %d\n", assignment.ConsumerReplicas))
		builder.WriteString("start: docker compose -f compose/docker-compose.control.yml up -d\n")
	}
	if assignment.Node.HasRole(RoleStorage) {
		builder.WriteString("storage compose: compose/docker-compose.storage.yml\n")
	}
	if assignment.Node.HasRole(RoleKafka) {
		builder.WriteString("kafka compose: compose/docker-compose.kafka.yml\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func kafkaClusterID(cfg *Config) string {
	sum := sha1.Sum([]byte(cfg.Metadata.Name + ":" + cfg.Metadata.Environment + ":" + cfg.Release.Version))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(encoded) < 22 {
		return encoded
	}
	return encoded[:22]
}

func copyFile(src, dst string, mode os.FileMode) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取文件失败 %s: %w", src, err)
	}
	if err := os.WriteFile(dst, content, mode); err != nil {
		return fmt.Errorf("写入文件失败 %s: %w", dst, err)
	}
	return nil
}
