// Package scheduler 提供任务调度器
package scheduler

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/agentcenter/transfer"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// PluginUpdateScheduler 插件更新调度器
// 定期检查 plugin_configs 表是否有更新，如果有则广播到所有在线 Agent
type PluginUpdateScheduler struct {
	db              *gorm.DB
	transferService *transfer.Service
	logger          *zap.Logger
	lastCheckTime   time.Time
	mu              sync.Mutex
}

// NewPluginUpdateScheduler 创建插件更新调度器
func NewPluginUpdateScheduler(db *gorm.DB, transferService *transfer.Service, logger *zap.Logger) *PluginUpdateScheduler {
	return &PluginUpdateScheduler{
		db:              db,
		transferService: transferService,
		logger:          logger,
		lastCheckTime:   time.Now(),
	}
}

// Start 启动插件更新调度器
func (s *PluginUpdateScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // 每 30 秒检查一次
	defer ticker.Stop()

	s.logger.Info("插件更新调度器已启动", zap.Duration("interval", 30*time.Second))

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("插件更新调度器已停止")
			return
		case <-ticker.C:
			s.checkAndBroadcast(ctx)
		}
	}
}

// checkAndBroadcast 检查是否有更新并广播
func (s *PluginUpdateScheduler) checkAndBroadcast(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 查询最近更新的插件配置
	var latestUpdate time.Time
	err := s.db.Model(&model.PluginConfig{}).
		Select("MAX(updated_at)").
		Where("enabled = ?", true).
		Scan(&latestUpdate).Error

	if err != nil {
		s.logger.Error("查询插件配置更新时间失败", zap.Error(err))
		return
	}

	// 如果有更新且更新时间比上次检查时间新
	if !latestUpdate.IsZero() && latestUpdate.After(s.lastCheckTime) {
		s.logger.Info("检测到插件配置更新，开始广播",
			zap.Time("last_check", s.lastCheckTime),
			zap.Time("latest_update", latestUpdate))

		successCount, failedAgents, err := s.transferService.BroadcastPluginConfigs(ctx)
		if err != nil {
			s.logger.Error("广播插件配置失败", zap.Error(err))
		} else {
			s.logger.Info("广播插件配置完成",
				zap.Int("success_count", successCount),
				zap.Strings("failed_agents", failedAgents))
		}

		// 更新检查时间
		s.lastCheckTime = time.Now()
	}
}

// TriggerBroadcast 手动触发广播（供 API 调用）
func (s *PluginUpdateScheduler) TriggerBroadcast(ctx context.Context) (int, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("手动触发插件配置广播")

	successCount, failedAgents, err := s.transferService.BroadcastPluginConfigs(ctx)
	if err != nil {
		return 0, nil, err
	}

	// 更新检查时间
	s.lastCheckTime = time.Now()

	return successCount, failedAgents, nil
}
