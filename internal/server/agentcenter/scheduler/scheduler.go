// Package scheduler 提供任务调度器
package scheduler

import (
	"time"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/server/agentcenter/service"
	"github.com/imkerbos/mxsec-platform/internal/server/agentcenter/transfer"
)

// StartTaskScheduler 启动任务调度器（定期分发待执行任务）
func StartTaskScheduler(taskService *service.TaskService, transferService *transfer.Service, logger *zap.Logger) {
	ticker := time.NewTicker(30 * time.Second) // 每 30 秒检查一次
	defer ticker.Stop()

	logger.Info("任务调度器已启动", zap.Duration("interval", 30*time.Second))

	// 立即执行一次
	dispatchAllPendingTasks(taskService, transferService, logger)

	// 定时执行
	for range ticker.C {
		dispatchAllPendingTasks(taskService, transferService, logger)
	}
}

// dispatchAllPendingTasks 分发所有待执行任务（检查任务和修复任务）
func dispatchAllPendingTasks(taskService *service.TaskService, transferService *transfer.Service, logger *zap.Logger) {
	// 分发基线检查任务
	if err := taskService.DispatchPendingTasks(transferService); err != nil {
		logger.Error("分发检查任务失败", zap.Error(err))
	}

	// 分发基线修复任务
	if err := taskService.DispatchPendingFixTasks(transferService); err != nil {
		logger.Error("分发修复任务失败", zap.Error(err))
	}
}
