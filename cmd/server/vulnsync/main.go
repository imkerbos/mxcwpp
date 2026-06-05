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

	"github.com/imkerbos/mxsec-platform/internal/server/vulnsync"
)

func main() {
	configPath := flag.String("config", "configs/vulnsync.yaml", "path to vulnsync config")
	httpAddr := flag.String("http", ":8083", "HTTP listen address for /health and /metrics")
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

	// 后续 PR: Leader Election + Cron 同步管线
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("VulnSync shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	logger.Info("VulnSync stopped")
}
