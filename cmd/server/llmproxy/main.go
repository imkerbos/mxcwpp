// Package main 是 LLMProxy 主程序入口。
//
// LLMProxy 是 v2.0 六微服务架构中的多 LLM 厂商适配网关,职责:
//   - 统一 Provider 抽象 (OpenAI/Anthropic/Gemini/DashScope/DeepSeek/Ollama/vLLM)
//   - 场景路由 (告警分析 / Storyline 总结 / 自然语言转查询 / 规则起草)
//   - Redis 24h 缓存 (入参 SHA256 -> 响应)
//   - 主厂商失败 Fallback (3 次失败黑名单 5min)
//   - 租户级 token 上限 + 月度成本告警 + 审计入 mxsec.llm.audit
//
// 设计文档: docs/llmproxy-design.md
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

	"github.com/imkerbos/mxsec-platform/internal/server/llmproxy"
)

func main() {
	configPath := flag.String("config", "configs/llmproxy.yaml", "path to llmproxy config")
	httpAddr := flag.String("http", ":8084", "HTTP listen address")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("LLMProxy starting (skeleton)",
		zap.String("config", *configPath),
		zap.String("http_addr", *httpAddr),
		zap.String("version", llmproxy.Version),
	)

	server := &http.Server{
		Addr:              *httpAddr,
		Handler:           llmproxy.NewHTTPHandler(logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("LLMProxy HTTP server listening", zap.String("addr", *httpAddr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(err))
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("LLMProxy shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	logger.Info("LLMProxy stopped")
}
