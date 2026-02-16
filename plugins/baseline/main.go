// Package main 是 Baseline Plugin 的主程序入口
// Baseline Plugin 作为 Agent 的子进程运行，通过 Pipe 与 Agent 通信
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/imkerbos/mxsec-platform/api/proto/bridge"
	"github.com/imkerbos/mxsec-platform/plugins/baseline/engine"
	plugins "github.com/imkerbos/mxsec-platform/plugins/lib/go"
)

// 版本信息（编译时注入）
var (
	buildVersion = "dev" // 通过 -ldflags "-X main.buildVersion=x.x.x" 注入
	buildTime    = ""    // 通过 -ldflags "-X main.buildTime=xxx" 注入
)

func main() {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "PANIC in main: %v\nStack trace:\n%s\n", r, debug.Stack())
			os.Exit(1)
		}
	}()

	// 1. 初始化插件客户端（通过 Pipe 与 Agent 通信）
	client, err := plugins.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create plugin client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// 2. 初始化日志（输出到 stderr，由 Agent 重定向到 /var/log/mxsec/plugins/baseline.log）
	logger, err := newPluginLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("baseline plugin starting",
		zap.Int("pid", os.Getpid()),
		zap.String("version", buildVersion),
		zap.String("build_time", buildTime))

	// 3. 创建检查引擎和修复执行器
	logger.Info("initializing check engine")
	checkEngine := engine.NewEngine(logger)
	logger.Info("check engine initialized successfully")

	logger.Info("initializing fixer")
	fixer := engine.NewFixer(logger)
	logger.Info("fixer initialized successfully")

	// 4. 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 5. 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// 6. 启动任务接收循环
	taskCh := make(chan *bridge.Task, 10)
	logger.Info("starting task receiver goroutine")
	go receiveTasks(ctx, client, taskCh, logger)
	logger.Info("task receiver goroutine started")

	logger.Info("baseline plugin initialization completed, entering main loop")

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
			if err := handleTask(ctx, task, checkEngine, fixer, client, logger); err != nil {
				logger.Error("failed to handle task", zap.Error(err))
			}
		}
	}
}

// receiveTasks 接收任务
func receiveTasks(ctx context.Context, client *plugins.Client, taskCh chan<- *bridge.Task, logger *zap.Logger) {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			logger.Error("PANIC in receiveTasks goroutine",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()

	logger.Info("receiveTasks goroutine started, waiting for tasks")

	for {
		select {
		case <-ctx.Done():
			logger.Info("receive tasks goroutine stopping due to context cancellation")
			return
		default:
			logger.Debug("calling ReceiveTask, waiting for data from pipe")
			task, err := client.ReceiveTask()
			if err != nil {
				// 检查是否是真正的管道关闭（EOF）
				if err.Error() == "EOF" || err.Error() == "io: read/write on closed pipe" {
					logger.Warn("pipe closed, plugin will exit", zap.Error(err))
					return
				}
				// 其他错误（如临时网络错误、解析错误等）记录后继续重试
				logger.Error("failed to receive task, will retry",
					zap.Error(err),
					zap.String("error_type", fmt.Sprintf("%T", err)))
				time.Sleep(time.Second)
				continue
			}

			logger.Debug("task received successfully from pipe")

			// 成功接收任务，发送到处理通道
			select {
			case taskCh <- task:
				logger.Debug("task forwarded to processing channel")
			case <-ctx.Done():
				logger.Info("receive tasks goroutine stopping while forwarding task")
				return
			}
		}
	}
}

// handleTask 处理任务
func handleTask(ctx context.Context, task *bridge.Task, checkEngine *engine.Engine, fixer *engine.Fixer, client *plugins.Client, logger *zap.Logger) (err error) {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			logger.Error("PANIC in handleTask",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
			err = fmt.Errorf("panic in handleTask: %v", r)
		}
	}()

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
	case 8002: // 基线修复任务
		return handleFixTask(ctx, taskData, fixer, client, logger)
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

// newPluginLogger 创建插件专用的 logger
// 输出到 stderr，由 Agent 重定向到 /var/log/mxsec/plugins/baseline.log
func newPluginLogger() (*zap.Logger, error) {
	config := zap.NewProductionConfig()
	// 输出到 stderr（Agent 会重定向到日志文件）
	config.OutputPaths = []string{"stderr"}
	config.ErrorOutputPaths = []string{"stderr"}
	// 使用 JSON 格式，便于解析
	config.Encoding = "json"
	// 设置日志级别为 Info
	config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	// 使用人类可读的时间格式
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return config.Build()
}

// handleFixTask 处理基线修复任务
func handleFixTask(ctx context.Context, taskData map[string]interface{}, fixer *engine.Fixer, client *plugins.Client, logger *zap.Logger) error {
	// 提取任务 ID
	taskID, _ := taskData["task_id"].(string)
	fixTaskID, _ := taskData["fix_task_id"].(string)

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

	// 提取规则 ID 列表
	ruleIDsInterface, _ := taskData["rule_ids"].([]interface{})
	var ruleIDs []string
	for _, id := range ruleIDsInterface {
		if idStr, ok := id.(string); ok {
			ruleIDs = append(ruleIDs, idStr)
		}
	}

	// 提取主机信息（用于 OS 匹配）
	osFamily, _ := taskData["os_family"].(string)
	osVersion, _ := taskData["os_version"].(string)

	logger.Info("executing baseline fix",
		zap.String("task_id", taskID),
		zap.String("fix_task_id", fixTaskID),
		zap.String("os_family", osFamily),
		zap.String("os_version", osVersion),
		zap.Int("policy_count", len(policies)),
		zap.Int("rule_count", len(ruleIDs)))

	// 执行修复（通过回调实时上报每条结果）
	results := fixer.FixBatch(ctx, policies, ruleIDs, osFamily, osVersion, func(result *engine.FixResult) {
		record := &bridge.Record{
			DataType:  8003, // 基线修复结果
			Timestamp: time.Now().UnixNano(),
			Data: &bridge.Payload{
				Fields: map[string]string{
					"task_id":     taskID,
					"fix_task_id": fixTaskID,
					"rule_id":     result.RuleID,
					"policy_id":   result.PolicyID,
					"status":      string(result.Status),
					"command":     result.Command,
					"output":      result.Output,
					"error_msg":   result.ErrorMsg,
					"message":     result.Message,
					"fixed_at":    result.FixedAt.Format(time.RFC3339),
				},
			},
		}

		if err := client.SendRecord(record); err != nil {
			logger.Error("failed to send fix result", zap.Error(err))
		}
	})

	// 发送任务完成信号
	completeRecord := &bridge.Record{
		DataType:  8004, // 修复任务完成信号
		Timestamp: time.Now().UnixNano(),
		Data: &bridge.Payload{
			Fields: map[string]string{
				"task_id":      taskID,
				"fix_task_id":  fixTaskID,
				"status":       "completed",
				"result_count": fmt.Sprintf("%d", len(results)),
				"completed_at": time.Now().Format(time.RFC3339),
			},
		},
	}
	if err := client.SendRecord(completeRecord); err != nil {
		logger.Error("failed to send fix task completion signal", zap.Error(err))
	}

	logger.Info("baseline fix completed",
		zap.String("task_id", taskID),
		zap.String("fix_task_id", fixTaskID),
		zap.Int("result_count", len(results)))
	return nil
}
