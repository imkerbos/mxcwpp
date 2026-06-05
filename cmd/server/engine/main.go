// Package main 是 Engine 主程序入口。
//
// Engine 是 v2.0 六微服务架构中的检测分析引擎,职责:
//   - 订阅 Kafka mxsec.agent.* (ConsumerGroup B "mxsec-engine")
//   - 多层引擎: CEL 规则 + 序列检测 + ONNX ML + Storyline + K8s Audit
//   - 产出 mxsec.engine.alert / storyline / feedback
//
// 设计文档: docs/engine-design.md / docs/engine-detection-design.md
//
// 本 PR (PR3) 仅提供空骨架: HTTP /health + /metrics + 优雅退出。
// 检测层实现由后续 PR 从 internal/server/consumer/celengine 等子包搬入。
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

	"github.com/imkerbos/mxsec-platform/internal/server/engine"
)

func main() {
	configPath := flag.String("config", "configs/engine.yaml", "path to engine config")
	httpAddr := flag.String("http", ":8082", "HTTP listen address for /health and /metrics")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("Engine starting (skeleton)",
		zap.String("config", *configPath),
		zap.String("http_addr", *httpAddr),
		zap.String("version", engine.Version),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := &http.Server{
		Addr:              *httpAddr,
		Handler:           engine.NewHTTPHandler(logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("Engine HTTP server listening", zap.String("addr", *httpAddr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(err))
		}
	}()

	// 后续 PR: 启动 Kafka ConsumerGroup B 订阅 mxsec.agent.* 和检测管线。
	_ = ctx

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("Engine shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	logger.Info("Engine stopped")
}
