package biz

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	grpcProto "github.com/imkerbos/mxsec-platform/api/proto/grpc"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

const (
	// RemediationDataType 漏洞修复任务的 DataType
	RemediationDataType = 9000
	// RemediationPluginName Agent 端处理修复任务的插件名称
	RemediationPluginName = "remediation"
)

// RemediationExecutor 修复任务执行器
type RemediationExecutor struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewRemediationExecutor 创建修复任务执行器
func NewRemediationExecutor(db *gorm.DB, logger *zap.Logger) *RemediationExecutor {
	return &RemediationExecutor{db: db, logger: logger}
}

// RemediationTaskPayload 下发给 Agent 的修复任务数据
type RemediationTaskPayload struct {
	TaskID       uint   `json:"task_id"`
	CveID        string `json:"cve_id"`
	Component    string `json:"component"`
	FixedVersion string `json:"fixed_version"`
	Command      string `json:"command"`
	DryRun       bool   `json:"dry_run"`
}

// remediationTaskTimeout 修复任务超时时间（running 状态超过此时间视为失败）
const remediationTaskTimeout = 30 * time.Minute

// DispatchConfirmedTasks 分发已确认的修复任务到 Agent
func (e *RemediationExecutor) DispatchConfirmedTasks(transferService interface {
	SendCommand(agentID string, cmd *grpcProto.Command) error
}) error {
	// 超时处理：将 running 超过 30 分钟的任务标记为失败
	e.timeoutRunningTasks()

	// 查询已确认待执行的任务
	var tasks []model.RemediationTask
	if err := e.db.Where("status = ?", "confirmed").Find(&tasks).Error; err != nil {
		return fmt.Errorf("查询已确认修复任务失败: %w", err)
	}

	if len(tasks) == 0 {
		return nil
	}

	e.logger.Info("发现待执行修复任务", zap.Int("count", len(tasks)))

	for i := range tasks {
		if err := e.dispatchTask(&tasks[i], transferService); err != nil {
			e.logger.Error("下发修复任务失败",
				zap.Uint("task_id", tasks[i].ID),
				zap.String("host_id", tasks[i].HostID),
				zap.Error(err),
			)
			continue
		}
	}

	return nil
}

// timeoutRunningTasks 将超时的 running 任务标记为 failed
func (e *RemediationExecutor) timeoutRunningTasks() {
	cutoff := time.Now().Add(-remediationTaskTimeout)
	now := model.Now()

	result := e.db.Model(&model.RemediationTask{}).
		Where("status = ? AND started_at < ?", "running", cutoff).
		Updates(map[string]any{
			"status":      "failed",
			"exec_output": "任务超时：Agent 未在规定时间内返回结果，可能主机已离线或命令执行超时",
			"finished_at": now,
		})

	if result.RowsAffected > 0 {
		e.logger.Warn("修复任务超时标记为失败", zap.Int64("count", result.RowsAffected))
	}
}

// dispatchTask 下发单个修复任务
func (e *RemediationExecutor) dispatchTask(task *model.RemediationTask, transferService interface {
	SendCommand(agentID string, cmd *grpcProto.Command) error
}) error {
	// 检查主机是否在线
	var host model.Host
	if err := e.db.Where("host_id = ? AND status = ?", task.HostID, "online").First(&host).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			e.logger.Debug("目标主机不在线，等待下次调度",
				zap.Uint("task_id", task.ID),
				zap.String("host_id", task.HostID))
			return nil
		}
		return fmt.Errorf("查询主机状态失败: %w", err)
	}

	// 先更新状态为 running（防止重复下发）
	now := model.Now()
	result := e.db.Model(task).
		Where("status = ?", "confirmed"). // CAS: 只有 confirmed 状态才能转 running
		Updates(map[string]any{
			"status":     "running",
			"started_at": now,
		})
	if result.RowsAffected == 0 {
		// 状态已被其他调度实例改变，跳过
		return nil
	}

	// 构建任务数据
	payload := RemediationTaskPayload{
		TaskID:       task.ID,
		CveID:        task.CveID,
		Component:    task.Component,
		FixedVersion: task.FixedVersion,
		Command:      task.Command,
		DryRun:       false,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		// 回滚状态
		e.db.Model(task).Updates(map[string]any{"status": "confirmed", "started_at": nil})
		return fmt.Errorf("序列化任务数据失败: %w", err)
	}

	// 构建 gRPC Task
	grpcTask := &grpcProto.Task{
		DataType:   RemediationDataType,
		ObjectName: RemediationPluginName,
		Data:       string(payloadJSON),
		Token:      fmt.Sprintf("rem-%d", task.ID),
	}

	cmd := &grpcProto.Command{
		Tasks: []*grpcProto.Task{grpcTask},
	}

	// 发送命令
	if err := transferService.SendCommand(host.HostID, cmd); err != nil {
		// 发送失败，回滚状态，下次调度重试
		e.db.Model(task).Updates(map[string]any{"status": "confirmed", "started_at": nil})
		return fmt.Errorf("发送修复命令失败: %w", err)
	}

	e.logger.Info("修复任务已下发",
		zap.Uint("task_id", task.ID),
		zap.String("host_id", task.HostID),
		zap.String("cve_id", task.CveID),
	)

	return nil
}

// HandleResult 处理 Agent 上报的修复执行结果
// DataType = 9001
func (e *RemediationExecutor) HandleResult(agentID string, data map[string]string) error {
	taskIDStr, ok := data["task_id"]
	if !ok {
		return fmt.Errorf("missing task_id in remediation result")
	}

	var taskID uint
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		return fmt.Errorf("invalid task_id: %s", taskIDStr)
	}

	var task model.RemediationTask
	if err := e.db.First(&task, taskID).Error; err != nil {
		return fmt.Errorf("task not found: %d", taskID)
	}

	// 安全校验：确认上报 Agent 就是任务目标主机
	if task.HostID != agentID {
		return fmt.Errorf("agent %s 无权上报任务 %d 的结果（目标主机为 %s）", agentID, taskID, task.HostID)
	}

	// 状态校验：只处理 running 状态的任务，避免重复处理
	if task.Status != "running" {
		e.logger.Warn("收到非 running 状态的修复结果，忽略",
			zap.Uint("task_id", taskID),
			zap.String("current_status", task.Status))
		return nil
	}

	exitCodeStr := data["exit_code"]
	var exitCode int
	fmt.Sscanf(exitCodeStr, "%d", &exitCode)

	stdout := data["stdout"]
	stderr := data["stderr"]
	output := stdout
	if stderr != "" {
		if output != "" {
			output += "\n--- STDERR ---\n" + stderr
		} else {
			output = stderr
		}
	}
	if output == "" {
		if exitCode == 0 {
			output = "命令执行成功（无输出）"
		} else {
			output = fmt.Sprintf("命令执行失败，退出码 %d（无输出）", exitCode)
		}
	}

	now := model.Now()
	status := "success"
	if exitCode != 0 {
		status = "failed"
	}

	updates := map[string]any{
		"status":      status,
		"exec_output": output,
		"exit_code":   exitCode,
		"finished_at": now,
	}

	if err := e.db.Model(&task).Updates(updates).Error; err != nil {
		return fmt.Errorf("update task status failed: %w", err)
	}

	// 修复成功时，更新漏洞主机关联状态
	if status == "success" {
		remSvc := NewRemediationService(e.db, e.logger)
		if err := remSvc.PatchVulnerability(task.VulnID, []string{task.HostID}); err != nil {
			e.logger.Error("更新漏洞修复状态失败",
				zap.Uint("task_id", task.ID),
				zap.Error(err))
		}
	}

	e.logger.Info("修复任务结果已处理",
		zap.Uint("task_id", taskID),
		zap.String("agent_id", agentID),
		zap.String("status", status),
		zap.Int("exit_code", exitCode),
	)

	return nil
}
