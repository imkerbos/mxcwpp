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
// 检测层实现由后续 PR 从 internal/server/engine/celengine 等子包搬入。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/common/mode"
	"github.com/imkerbos/mxsec-platform/internal/server/common/observability"
	"github.com/imkerbos/mxsec-platform/internal/server/engine"
	"github.com/imkerbos/mxsec-platform/internal/server/engine/celengine"
	"github.com/imkerbos/mxsec-platform/internal/server/engine/storyline"
)

func main() {
	configPath := flag.String("config", "configs/engine.yaml", "path to engine config")
	httpAddr := flag.String("http", ":8082", "HTTP listen address for /health and /metrics")
	otelEnabled := flag.Bool("otel", false, "enable OpenTelemetry tracing")
	otelEndpoint := flag.String("otel-endpoint", "localhost:4318", "OTLP collector endpoint")
	otelSampleRate := flag.Float64("otel-sample-rate", 0.1, "OTel trace sample rate (0-1)")
	kafkaBrokers := flag.String("kafka-brokers", "", "Kafka broker addresses (comma separated); empty disables ConsumerGroup B")
	alertTopic := flag.String("alert-topic", "mxsec.engine.alert", "engine alert producer topic")
	defaultMode := flag.String("default-mode", "observe", "default operating mode (observe/protect)")
	dbDSN := flag.String("db-dsn", "", "MySQL DSN for stages requiring DB (CEL/sequence/baseline/storyline);空时跳过实际检测 stages")
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

	// v2.0: OTel 全链路追踪初始化 (otel.enabled=false 时走 noop,零开销)
	tracerProvider, err := observability.InitTracing(ctx, observability.Config{
		Enabled:        *otelEnabled,
		ServiceName:    "engine",
		ServiceVersion: engine.Version,
		Endpoint:       *otelEndpoint,
		Insecure:       true,
		SampleRate:     *otelSampleRate,
	})
	if err != nil {
		logger.Error("OTel 初始化失败,继续走 noop", zap.Error(err))
	}
	defer func() { _ = tracerProvider.Shutdown(context.Background()) }()

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

	// 启动 Kafka 检测链路 (Producer → Pipeline → ConsumerGroup B)。
	// Sprint 2 PR33: Pipeline 真实接入 (stages 由后续 PR 注入,本 PR 用空 stages 跑通)。
	if *kafkaBrokers != "" {
		brokers := strings.Split(*kafkaBrokers, ",")

		// Producer (推送告警到 mxsec.engine.alert)
		producer, err := engine.NewAlertProducer(brokers, *alertTopic, logger)
		if err != nil {
			logger.Fatal("Engine AlertProducer 初始化失败", zap.Error(err))
		}
		defer func() { _ = producer.Close() }()

		// Mode Resolver (默认 observe, 后续 PR 接租户/规则覆盖加载)
		resolver := mode.NewMemoryResolver(mode.Mode(*defaultMode))

		// Stages (DB 可用时启用 CEL/Sequence/Storyline; 否则空数组)
		var stages []engine.Stage
		if *dbDSN != "" {
			db, err := gorm.Open(mysql.Open(*dbDSN), &gorm.Config{})
			if err != nil {
				logger.Warn("Engine DB 初始化失败, 跳过 stages", zap.Error(err))
			} else {
				celEng, err := celengine.New(db, logger.Named("cel"))
				if err != nil {
					logger.Warn("Engine celengine 初始化失败,跳过 CelRuleStage", zap.Error(err))
				} else {
					stages = append(stages, engine.NewCelRuleStage(celEng, logger))
					stages = append(stages, engine.NewSequenceStage(
						celengine.NewSequenceDetector(celEng, db, nil, logger.Named("seq")),
						logger))
				}
				storyEng := storyline.NewEngine(db, logger.Named("story"))
				stages = append(stages, engine.NewStorylineStage(storyEng, logger))
				logger.Info("Engine stages 已注入",
					zap.Int("stages_count", len(stages)),
				)
			}
		} else {
			logger.Warn("Engine DB DSN 未配置, stages 为空, 仅 noop 跑通管线")
		}

		pipeline := engine.NewPipeline(producer, resolver, stages, logger)

		// Consumer (用 Pipeline.Handler 作为消息处理器)
		kc, err := engine.NewKafkaConsumer(brokers, pipeline.Handler(), logger)
		if err != nil {
			logger.Fatal("Engine Kafka consumer 初始化失败", zap.Error(err))
		}
		kc.Start(ctx)
		defer func() { _ = kc.Close() }()

		logger.Info("Engine 检测链路启动",
			zap.String("alert_topic", *alertTopic),
			zap.String("default_mode", *defaultMode),
		)
	} else {
		logger.Warn("Engine Kafka brokers 未配置,跳过检测链路启动")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("Engine shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	logger.Info("Engine stopped")
}
