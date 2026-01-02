// Package database 提供数据库连接管理
package database

import (
	"fmt"
	"io"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"

	"github.com/imkerbos/mxsec-platform/internal/server/config"
	"github.com/imkerbos/mxsec-platform/internal/server/migration"
)

// DB 是全局数据库实例
var DB *gorm.DB

// Init 初始化数据库连接
func Init(cfg config.DatabaseConfig, zapLogger *zap.Logger) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	// 配置 Gorm 日志
	var gormLog gormLogger.Interface
	if zapLogger != nil {
		// 将 Zap logger 适配为 Gorm logger
		gormLog = gormLogger.New(
			&zapWriter{logger: zapLogger},
			gormLogger.Config{
				SlowThreshold:             200,
				LogLevel:                  gormLogger.Info,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
			},
		)
	} else {
		gormLog = gormLogger.Default
	}

	// 根据数据库类型创建连接
	switch cfg.Type {
	case "mysql":
		db, err = gorm.Open(mysql.Open(cfg.MySQL.DSN()), &gorm.Config{
			Logger: gormLog,
		})
		if err != nil {
			return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
		}

		// 配置连接池
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("获取数据库实例失败: %w", err)
		}
		sqlDB.SetMaxIdleConns(cfg.MySQL.MaxIdleConns)
		sqlDB.SetMaxOpenConns(cfg.MySQL.MaxOpenConns)
		sqlDB.SetConnMaxLifetime(cfg.MySQL.ConnMaxLifetime)

	case "postgres":
		db, err = gorm.Open(postgres.Open(cfg.Postgres.DSN()), &gorm.Config{
			Logger: gormLog,
		})
		if err != nil {
			return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
		}

		// 配置连接池
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("获取数据库实例失败: %w", err)
		}
		sqlDB.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)
		sqlDB.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
		sqlDB.SetConnMaxLifetime(cfg.Postgres.ConnMaxLifetime)

	default:
		return nil, fmt.Errorf("不支持的数据库类型: %s", cfg.Type)
	}

	// 执行数据库迁移
	if err := migration.Migrate(db, zapLogger); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}

	// 保存全局实例
	DB = db

	if zapLogger != nil {
		zapLogger.Info("数据库连接成功", zap.String("type", cfg.Type))
	}

	return db, nil
}

// Close 关闭数据库连接
func Close() error {
	if DB == nil {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}

	return sqlDB.Close()
}

// zapWriter 将 Zap logger 适配为 Gorm logger.Writer
type zapWriter struct {
	logger *zap.Logger
}

func (w *zapWriter) Printf(format string, args ...interface{}) {
	w.logger.Info(fmt.Sprintf(format, args...))
}

// Write 实现 io.Writer 接口（兼容性）
func (w *zapWriter) Write(p []byte) (n int, err error) {
	w.logger.Info(string(p))
	return len(p), nil
}

// 确保 zapWriter 实现了所有必要的接口
var (
	_ io.Writer         = (*zapWriter)(nil)
	_ gormLogger.Writer = (*zapWriter)(nil)
)
