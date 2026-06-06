// Package main 是 VulnSync 主程序入口。
//
// VulnSync 是 v2.0 六微服务架构中的漏洞情报融合服务,职责:
//   - 定时同步 11+ 外部源(NVD/OSV/RHSA/USN/Debian/Alpine/SUSE/CISA KEV/ExploitDB/CNNVD/EPSS/信创 4 源)
//   - PURL+NEVRA 双索引模型 + 3 级 confidence 仲裁
//   - 推送 advisory 到 Kafka mxsec.vuln.advisory
//   - Leader Election (避免重复抓取)
//
// 设计文档: docs/vulnsync-design.md
//
// 本 PR (PR3) 仅提供空骨架: HTTP /health + /metrics + 优雅退出。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/redis/go-redis/v9"

	"github.com/imkerbos/mxsec-platform/internal/server/vulnsync"
	"github.com/imkerbos/mxsec-platform/internal/server/vulnsync/leader"
)

func main() {
	configPath := flag.String("config", "configs/vulnsync.yaml", "path to vulnsync config")
	httpAddr := flag.String("http", ":8083", "HTTP listen address for /health and /metrics")
	redisAddr := flag.String("redis-addr", "", "Redis 地址 (启用 Leader Election);空时跳过")
	instanceID := flag.String("instance-id", "", "实例唯一 ID (默认 hostname+pid)")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("VulnSync starting (skeleton)",
		zap.String("config", *configPath),
		zap.String("http_addr", *httpAddr),
		zap.String("version", vulnsync.Version),
	)

	server := &http.Server{
		Addr:              *httpAddr,
		Handler:           vulnsync.NewHTTPHandler(logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("VulnSync HTTP server listening", zap.String("addr", *httpAddr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(err))
		}
	}()

	// Leader Election (Sprint 2 PR17 引入)
	// Redis 未配置时跳过,单实例部署不需要 Leader Election。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *redisAddr != "" {
		id := *instanceID
		if id == "" {
			if h, err := os.Hostname(); err == nil {
				id = fmt.Sprintf("%s-%d", h, os.Getpid())
			} else {
				id = fmt.Sprintf("vulnsync-%d", os.Getpid())
			}
		}
		rdb := redis.NewClient(&redis.Options{Addr: *redisAddr})
		defer func() { _ = rdb.Close() }()
		election := leader.NewElection(rdb, id, leader.Config{}, logger)
		go election.Run(ctx)
		logger.Info("VulnSync Leader Election started",
			zap.String("redis_addr", *redisAddr),
			zap.String("instance_id", id),
		)
	} else {
		logger.Warn("Redis 未配置,单实例模式,跳过 Leader Election")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("VulnSync shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	logger.Info("VulnSync stopped")
}
