// Package logger 提供结构化日志功能（基于 Zap）
// 默认配置：按天轮转，保留30天
package logger

import (
	"os"
	"path/filepath"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogConfig 是日志配置
type LogConfig struct {
	Level  string
	Format string
	File   string
	MaxAge int // 保留天数
}

// Init 初始化日志器（使用默认配置：按天轮转，保留30天）
func Init(cfg LogConfig) (*zap.Logger, error) {
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
		// 格式：agent.log.YYYY-MM-DD
		// 保留天数：默认30天
		maxAge := time.Duration(cfg.MaxAge) * 24 * time.Hour
		if maxAge == 0 {
			maxAge = 30 * 24 * time.Hour // 默认30天
		}

		// 创建轮转日志写入器
		rotateWriter, err := rotatelogs.New(
			cfg.File+".%Y-%m-%d",                      // 轮转后的文件名格式
			rotatelogs.WithLinkName(cfg.File),         // 当前日志文件链接
			rotatelogs.WithMaxAge(maxAge),             // 保留时间
			rotatelogs.WithRotationTime(24*time.Hour), // 每24小时轮转一次
			rotatelogs.WithRotationCount(0),           // 不限制文件数量，由 MaxAge 控制
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
