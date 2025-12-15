// Package main 是 Agent 主程序入口
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"

	"github.com/mxcsec-platform/mxcsec-platform/api/proto/grpc"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/config"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/connection"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/heartbeat"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/id"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/logger"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/plugin"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/transport"
)

var (
	version = flag.Bool("version", false, "显示版本信息")
)

// 构建时嵌入的变量（通过 -ldflags 设置）
// Server 在部署时生成配置，编译时嵌入到 Agent 二进制
// 示例: go build -ldflags "-X main.serverHost=10.0.0.1:6751 -X main.buildVersion=1.0.0" ./cmd/agent
var (
	serverHost   string // Server 地址（构建时嵌入，必须）
	buildVersion string // 构建版本（构建时嵌入）
	buildTime    string // 构建时间（构建时嵌入）
)

func main() {
	flag.Parse()

	if *version {
		printVersion()
		return
	}

	// 1. 验证构建时嵌入的配置（必须）
	if serverHost == "" {
		panic("serverHost must be embedded at build time, use -ldflags \"-X main.serverHost=HOST:PORT\"")
	}

	// 2. 加载默认配置（完全依赖构建时嵌入，不需要配置文件）
	cfg := config.LoadDefaults()
	cfg.Local.Server.AgentCenter.PrivateHost = serverHost

	// 3. 初始化日志（默认配置：按天轮转，保留30天）
	log, err := logger.Init(logger.LogConfig{
		Level:  "info",
		Format: "json",
		File:   "/var/log/mxsec-agent/agent.log",
		MaxAge: 30, // 保留30天
	})
	if err != nil {
		panic(err)
	}
	defer log.Sync()

	log.Info("Agent starting",
		zap.String("version", cfg.GetVersion()),
		zap.String("product", cfg.GetProduct()),
		zap.String("server", serverHost),
		zap.Bool("remote_config_loaded", cfg.Remote.Loaded),
	)

	// 4. 初始化 Agent ID
	agentID, err := id.InitID(cfg.Local.IDFile)
	if err != nil {
		log.Fatal("failed to init agent ID", zap.Error(err))
	}
	log.Info("Agent ID initialized", zap.String("agent_id", agentID))

	// 5. 创建连接管理器
	connMgr := connection.NewManager(cfg, log)

	// 6. 创建传输管理器（用于心跳模块）
	transportMgr, err := transport.NewManager(cfg, log, connMgr, agentID)
	if err != nil {
		log.Fatal("创建传输管理器失败", zap.Error(err))
	}

	// 7. 创建插件管理器（需要在心跳模块之前创建，以便传递引用）
	pluginMgr := plugin.NewManager(cfg, log, transportMgr)

	// 8. 设置配置更新回调
	transportMgr.SetConfigUpdateCallback(func(agentConfig *grpc.AgentConfig, certBundle *grpc.CertificateBundle) {
		// 处理证书包更新
		if certBundle != nil {
			certDir := "/var/lib/mxsec-agent/certs"
			if err := cfg.SyncCertificatesFromServer(certBundle, certDir); err != nil {
				log.Error("failed to sync certificates from server", zap.Error(err))
			} else {
				log.Info("certificates updated from server",
					zap.String("cert_dir", certDir),
					zap.String("hint", "证书已保存，后续连接将使用正式证书"),
				)
				// 证书更新后，需要重新建立连接（使用新证书）
				// 注意：当前连接会继续使用，下次重连时会自动使用新证书
				log.Info("certificates saved successfully, will use them for next connection")
			}
		}

		// 处理 Agent 配置更新
		if agentConfig != nil {
			if err := cfg.SyncFromServer(agentConfig); err != nil {
				log.Error("failed to sync config from server", zap.Error(err))
			} else {
				log.Info("config updated from server",
					zap.Int32("heartbeat_interval", agentConfig.HeartbeatInterval),
					zap.String("work_dir", agentConfig.WorkDir),
				)
			}
		}
	})

	// 9. 启动核心模块
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg := &sync.WaitGroup{}
	wg.Add(3)

	// 心跳模块（传递插件管理器引用）
	go heartbeat.Startup(ctx, wg, cfg, log, transportMgr, agentID, pluginMgr)

	// 传输模块（使用已创建的传输管理器）
	go transport.StartupWithManager(ctx, wg, transportMgr)

	// 插件管理模块（使用已创建的插件管理器）
	go plugin.StartupWithManager(ctx, wg, pluginMgr)

	// 9. 信号处理
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGINT)

	log.Info("Agent started, waiting for shutdown signal...")

	// 等待信号
	sig := <-signalCh
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// 10. 优雅退出
	log.Info("Shutting down...")
	cancel()
	wg.Wait()

	// 关闭连接
	if err := connMgr.Close(); err != nil {
		log.Error("Failed to close connection", zap.Error(err))
	}

	log.Info("Agent stopped")
}

func printVersion() {
	version := buildVersion
	if version == "" {
		version = "dev"
	}
	buildTimeStr := buildTime
	if buildTimeStr == "" {
		buildTimeStr = "unknown"
	}
	fmt.Printf("mxsec-agent version %s\n", version)
	fmt.Printf("Build time: %s\n", buildTimeStr)
	if serverHost != "" {
		fmt.Printf("Server: %s\n", serverHost)
	}
}
