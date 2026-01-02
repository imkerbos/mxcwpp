// Package logger 提供 Server 结构化日志功能（基于 Zap）
package logger

import (
	"os"
	"path/filepath"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/imkerbos/mxsec-platform/internal/server/config"
)

// Init 初始化日志器
func Init(cfg config.LogConfig) (*zap.Logger, error) {
	// 配置日志级别
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	// 配置编码器
	var encoder zapcore.Encoder
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder

	if cfg.Format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// 配置写入器
	var writeSyncer zapcore.WriteSyncer
	if cfg.File != "" {
		// 确保日志目录存在
		logDir := filepath.Dir(cfg.File)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, err
		}

		// 配置按天轮转的日志文件
		maxAge := time.Duration(cfg.MaxAge) * 24 * time.Hour
		if maxAge == 0 {
			maxAge = 30 * 24 * time.Hour // 默认30天
		}

		// 创建轮转日志写入器
		rotateWriter, err := rotatelogs.New(
			cfg.File+".%Y-%m-%d",
			rotatelogs.WithLinkName(cfg.File),
			rotatelogs.WithMaxAge(maxAge),
			rotatelogs.WithRotationTime(24*time.Hour),
			rotatelogs.WithRotationCount(0),
		)
		if err != nil {
			return nil, err
		}

		// 文件日志 + 标准输出
		writeSyncer = zapcore.NewMultiWriteSyncer(
			zapcore.AddSync(rotateWriter),
			zapcore.AddSync(os.Stdout),
		)
	} else {
		// 仅标准输出
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	// 创建核心
	core := zapcore.NewCore(encoder, writeSyncer, level)

	// 创建 logger
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger, nil
}
