// Package service 提供任务状态管理服务
package service

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

const (
	// TaskTimeout 任务超时时间（1 小时）
	TaskTimeout = 1 * time.Hour
)

// TaskStatusUpdater 任务状态更新器
type TaskStatusUpdater struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewTaskStatusUpdater 创建任务状态更新器
func NewTaskStatusUpdater(db *gorm.DB, logger *zap.Logger) *TaskStatusUpdater {
	return &TaskStatusUpdater{
		db:     db,
		logger: logger,
	}
}

// Start 启动任务状态更新器（后台 goroutine）
func (u *TaskStatusUpdater) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // 每 30 秒检查一次
	defer ticker.Stop()

	u.logger.Info("任务状态更新器已启动")

	for {
		select {
		case <-ctx.Done():
			u.logger.Info("任务状态更新器已停止")
			return
		case <-ticker.C:
			if err := u.updateTaskStatuses(); err != nil {
				u.logger.Error("更新任务状态失败", zap.Error(err))
			}
		}
	}
}

// updateTaskStatuses 更新任务状态
// 检查 running 状态的任务，如果所有主机都已完成检测，则标记任务为 completed
// 如果任务超时，则标记为 failed
func (u *TaskStatusUpdater) updateTaskStatuses() error {
	// 查询所有 running 状态的任务
	var runningTasks []model.ScanTask
	if err := u.db.Where("status = ?", model.TaskStatusRunning).Find(&runningTasks).Error; err != nil {
		return err
	}

	for _, task := range runningTasks {
		// 先检查任务是否超时
		if u.isTaskTimeout(&task) {
			if err := u.markTaskAsFailed(&task, "任务超时"); err != nil {
				u.logger.Error("标记任务为失败失败",
					zap.String("task_id", task.TaskID),
					zap.Error(err),
				)
				continue
			}
			continue
		}

		// 检查任务是否完成
		if err := u.checkAndUpdateTaskStatus(&task); err != nil {
			u.logger.Error("检查任务状态失败",
				zap.String("task_id", task.TaskID),
				zap.Error(err),
			)
			continue
		}
	}

	return nil
}

// isTaskTimeout 检查任务是否超时
func (u *TaskStatusUpdater) isTaskTimeout(task *model.ScanTask) bool {
	// 获取任务执行时间
	taskExecutedAt := task.ExecutedAt
	if taskExecutedAt == nil {
		// 如果任务执行时间为空，使用创建时间
		taskExecutedAt = &task.CreatedAt
	}

	// 检查是否超时
	elapsed := time.Since(time.Time(*taskExecutedAt))
	return elapsed > TaskTimeout
}

// markTaskAsFailed 标记任务为失败
func (u *TaskStatusUpdater) markTaskAsFailed(task *model.ScanTask, reason string) error {
	now := time.Now()
	if err := u.db.Model(task).Updates(map[string]interface{}{
		"status":     model.TaskStatusFailed,
		"updated_at": now,
	}).Error; err != nil {
		return err
	}

	taskExecutedAt := task.ExecutedAt
	if taskExecutedAt == nil {
		taskExecutedAt = &task.CreatedAt
	}

	u.logger.Info("任务已标记为失败",
		zap.String("task_id", task.TaskID),
		zap.String("reason", reason),
		zap.Duration("elapsed", time.Since(time.Time(*taskExecutedAt))),
	)

	return nil
}

// checkAndUpdateTaskStatus 检查并更新单个任务状态
func (u *TaskStatusUpdater) checkAndUpdateTaskStatus(task *model.ScanTask) error {
	// 根据 target_type 查询应该执行任务的主机
	var expectedHosts []model.Host
	switch task.TargetType {
	case model.TargetTypeAll:
		// 查询所有在线主机（在任务创建时是在线的）
		// 注意：这里简化处理，实际应该记录任务创建时的主机列表
		if err := u.db.Where("status = ?", model.HostStatusOnline).Find(&expectedHosts).Error; err != nil {
			return err
		}

	case model.TargetTypeHostIDs:
		if len(task.TargetConfig.HostIDs) == 0 {
			return nil
		}
		if err := u.db.Where("host_id IN ?", task.TargetConfig.HostIDs).Find(&expectedHosts).Error; err != nil {
			return err
		}

	case model.TargetTypeOSFamily:
		if len(task.TargetConfig.OSFamily) == 0 {
			return nil
		}
		if err := u.db.Where("os_family IN ?", task.TargetConfig.OSFamily).Find(&expectedHosts).Error; err != nil {
			return err
		}

	default:
		return nil
	}

	if len(expectedHosts) == 0 {
		// 没有匹配的主机，标记任务为 completed
		return u.db.Model(task).Update("status", model.TaskStatusCompleted).Error
	}

	// 检查每个主机是否有该任务的检测结果
	// 如果所有主机都有结果（且结果时间在任务执行时间之后），则标记任务为 completed
	allCompleted := true
	taskExecutedAt := task.ExecutedAt
	if taskExecutedAt == nil {
		// 如果任务执行时间为空，使用创建时间
		taskExecutedAt = &task.CreatedAt
	}

	for _, host := range expectedHosts {
		// 检查是否有该任务的检测结果（在任务执行时间之后）
		var resultCount int64
		if err := u.db.Model(&model.ScanResult{}).
			Where("task_id = ? AND host_id = ? AND checked_at >= ?", task.TaskID, host.HostID, taskExecutedAt).
			Count(&resultCount).Error; err != nil {
			u.logger.Warn("查询任务结果失败",
				zap.String("task_id", task.TaskID),
				zap.String("host_id", host.HostID),
				zap.Error(err),
			)
			allCompleted = false
			break
		}

		// 如果某个主机没有结果，说明任务还未完成
		if resultCount == 0 {
			allCompleted = false
			break
		}
	}

	// 如果所有主机都已完成，更新任务状态
	if allCompleted {
		now := time.Now()
		if err := u.db.Model(task).Updates(map[string]interface{}{
			"status":     model.TaskStatusCompleted,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}

		u.logger.Info("任务已完成",
			zap.String("task_id", task.TaskID),
			zap.Int("host_count", len(expectedHosts)),
		)
	}

	return nil
}
