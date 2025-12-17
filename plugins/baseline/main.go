// Package main 是 Baseline Plugin 的主程序入口
// Baseline Plugin 作为 Agent 的子进程运行，通过 Pipe 与 Agent 通信
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/mxcsec-platform/mxcsec-platform/api/proto/bridge"
	"github.com/mxcsec-platform/mxcsec-platform/plugins/baseline/engine"
	plugins "github.com/mxcsec-platform/mxcsec-platform/plugins/lib/go"
)

func main() {
	// 1. 初始化插件客户端（通过 Pipe 与 Agent 通信）
	client, err := plugins.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create plugin client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// 2. 初始化日志（简化实现，直接输出到 stderr）
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	logger.Info("baseline plugin starting")

	// 3. 创建检查引擎
	checkEngine := engine.NewEngine(logger)

	// 4. 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 5. 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// 6. 启动任务接收循环
	taskCh := make(chan *bridge.Task, 10)
	go receiveTasks(ctx, client, taskCh, logger)

	// 7. 主循环：处理任务
	for {
		select {
		case <-ctx.Done():
			logger.Info("baseline plugin shutting down")
			return
		case sig := <-sigCh:
			logger.Info("received signal", zap.String("signal", sig.String()))
			cancel()
			return
		case task := <-taskCh:
			if err := handleTask(ctx, task, checkEngine, client, logger); err != nil {
				logger.Error("failed to handle task", zap.Error(err))
			}
		}
	}
}

// receiveTasks 接收任务
func receiveTasks(ctx context.Context, client *plugins.Client, taskCh chan<- *bridge.Task, logger *zap.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			task, err := client.ReceiveTask()
			if err != nil {
				if err.Error() == "EOF" {
					logger.Info("pipe closed, exiting")
					return
				}
				logger.Error("failed to receive task", zap.Error(err))
				time.Sleep(time.Second)
				continue
			}

			select {
			case taskCh <- task:
			case <-ctx.Done():
				return
			}
		}
	}
}

// handleTask 处理任务
func handleTask(ctx context.Context, task *bridge.Task, checkEngine *engine.Engine, client *plugins.Client, logger *zap.Logger) error {
	logger.Info("received task", zap.String("data_type", fmt.Sprintf("%d", task.DataType)), zap.String("object_name", task.ObjectName))

	// 解析任务数据（JSON）
	var taskData map[string]interface{}
	if err := json.Unmarshal([]byte(task.Data), &taskData); err != nil {
		return fmt.Errorf("failed to unmarshal task data: %w", err)
	}

	// 根据任务类型处理
	switch task.DataType {
	case 8000: // 基线检查任务
		return handleBaselineTask(ctx, taskData, checkEngine, client, logger)
	default:
		logger.Warn("unknown task type", zap.Int32("data_type", task.DataType))
		return nil
	}
}

// handleBaselineTask 处理基线检查任务
func handleBaselineTask(ctx context.Context, taskData map[string]interface{}, checkEngine *engine.Engine, client *plugins.Client, logger *zap.Logger) error {
	// 提取任务 ID（用于关联结果）
	taskID, _ := taskData["task_id"].(string)
	policyID, _ := taskData["policy_id"].(string)

	// 提取策略配置
	policiesJSON, ok := taskData["policies"].(string)
	if !ok {
		return fmt.Errorf("missing policies in task data")
	}

	var policiesData []map[string]interface{}
	if err := json.Unmarshal([]byte(policiesJSON), &policiesData); err != nil {
		return fmt.Errorf("failed to unmarshal policies: %w", err)
	}

	// 转换为 Policy 对象
	var policies []*engine.Policy
	for _, pd := range policiesData {
		policyJSON, _ := json.Marshal(pd)
		var p engine.Policy
		if err := json.Unmarshal(policyJSON, &p); err != nil {
			logger.Warn("failed to unmarshal policy", zap.Error(err))
			continue
		}
		policies = append(policies, &p)
	}

	// 提取主机信息（用于 OS 匹配）
	osFamily, _ := taskData["os_family"].(string)
	osVersion, _ := taskData["os_version"].(string)

	logger.Info("executing baseline check",
		zap.String("task_id", taskID),
		zap.String("policy_id", policyID),
		zap.String("os_family", osFamily),
		zap.String("os_version", osVersion),
		zap.Int("policy_count", len(policies)))

	// 执行检查
	results := checkEngine.Execute(ctx, policies, osFamily, osVersion)

	// 上报结果
	for _, result := range results {
		record := &bridge.Record{
			DataType:  8000, // 基线检查结果
			Timestamp: time.Now().UnixNano(),
			Data: &bridge.Payload{
				Fields: map[string]string{
					"task_id":        taskID,   // 添加 task_id
					"rule_id":        result.RuleID,
					"policy_id":      result.PolicyID,
					"status":         string(result.Status),
					"severity":       result.Severity,
					"category":       result.Category,
					"title":          result.Title,
					"actual":         result.Actual,
					"expected":       result.Expected,
					"fix_suggestion": result.FixSuggestion,
					"checked_at":     result.CheckedAt.Format(time.RFC3339),
				},
			},
		}

		if err := client.SendRecord(record); err != nil {
			logger.Error("failed to send result", zap.Error(err))
			continue
		}
	}

	// 发送任务完成信号
	completeRecord := &bridge.Record{
		DataType:  8001, // 任务完成信号
		Timestamp: time.Now().UnixNano(),
		Data: &bridge.Payload{
			Fields: map[string]string{
				"task_id":      taskID,
				"policy_id":    policyID,
				"status":       "completed",
				"result_count": fmt.Sprintf("%d", len(results)),
				"completed_at": time.Now().Format(time.RFC3339),
			},
		},
	}
	if err := client.SendRecord(completeRecord); err != nil {
		logger.Error("failed to send task completion signal", zap.Error(err))
	}

	logger.Info("baseline check completed",
		zap.String("task_id", taskID),
		zap.Int("result_count", len(results)))
	return nil
}
