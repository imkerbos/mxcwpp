// Package main 是 Remediation Plugin 的主程序入口
// Remediation Plugin 作为 Agent 的子进程运行，接收修复命令并执行
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/imkerbos/mxsec-platform/api/proto/bridge"
	plugins "github.com/imkerbos/mxsec-platform/plugins/lib/go"
)

var (
	buildVersion = "dev"
	buildTime    = ""
)

// commandTimeout 单条修复命令的最大执行时间
const commandTimeout = 10 * time.Minute

// taskPayload 从 Server 下发的修复任务数据
type taskPayload struct {
	TaskID       uint   `json:"task_id"`
	CveID        string `json:"cve_id"`
	Component    string `json:"component"`
	FixedVersion string `json:"fixed_version"`
	Command      string `json:"command"`
	DryRun       bool   `json:"dry_run"`
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "PANIC in main: %v\nStack trace:\n%s\n", r, debug.Stack())
			os.Exit(1)
		}
	}()

	client, err := plugins.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create plugin client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	logger, err := newPluginLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("remediation plugin started",
		zap.String("version", buildVersion),
		zap.String("build_time", buildTime))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
		cancel()
	}()

	// 任务接收循环
	taskCh := make(chan *bridge.Task, 10)
	go receiveTasks(ctx, client, taskCh, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info("remediation plugin stopped")
			return
		case task, ok := <-taskCh:
			if !ok {
				return
			}
			if err := handleTask(ctx, task, client, logger); err != nil {
				logger.Error("handle task failed", zap.Error(err))
			}
		}
	}
}

func receiveTasks(ctx context.Context, client *plugins.Client, taskCh chan<- *bridge.Task, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("PANIC in receiveTasks", zap.Any("recover", r))
		}
		close(taskCh)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		task, err := client.ReceiveTaskWithTimeout(5 * time.Second)
		if err != nil {
			if strings.Contains(err.Error(), "timeout") {
				continue
			}
			if strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "closed") {
				logger.Info("pipe closed, exiting")
				return
			}
			logger.Error("receive task failed", zap.Error(err))
			time.Sleep(time.Second)
			continue
		}

		if task != nil {
			taskCh <- task
		}
	}
}

func handleTask(ctx context.Context, task *bridge.Task, client *plugins.Client, logger *zap.Logger) error {
	logger.Info("received remediation task",
		zap.Int32("data_type", task.DataType),
		zap.String("token", task.Token))

	// 解析任务数据
	var payload taskPayload
	if err := json.Unmarshal([]byte(task.Data), &payload); err != nil {
		return fmt.Errorf("解析任务数据失败: %w", err)
	}

	logger.Info("executing remediation command",
		zap.Uint("task_id", payload.TaskID),
		zap.String("cve_id", payload.CveID),
		zap.String("component", payload.Component),
		zap.String("command", payload.Command),
		zap.Bool("dry_run", payload.DryRun))

	// 命令安全校验
	if payload.Command == "" {
		return sendResult(client, payload.TaskID, 1, "", "修复命令为空", logger)
	}

	// DryRun 模式：不实际执行
	if payload.DryRun {
		logger.Info("dry run mode, skipping execution")
		return sendResult(client, payload.TaskID, 0, "[DRY RUN] 命令未实际执行: "+payload.Command, "", logger)
	}

	// 执行修复命令
	execCtx, execCancel := context.WithTimeout(ctx, commandTimeout)
	defer execCancel()

	cmd := exec.CommandContext(execCtx, "/bin/sh", "-c", payload.Command)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	output, err := cmd.CombinedOutput()
	exitCode := 0
	stdout := string(output)
	stderr := ""

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			exitCode = 124 // timeout
			stderr = "命令执行超时（超过 10 分钟）"
		} else {
			exitCode = 1
			stderr = err.Error()
		}
	}

	logger.Info("remediation command completed",
		zap.Uint("task_id", payload.TaskID),
		zap.Int("exit_code", exitCode),
		zap.Int("output_len", len(stdout)))

	return sendResult(client, payload.TaskID, exitCode, stdout, stderr, logger)
}

func sendResult(client *plugins.Client, taskID uint, exitCode int, stdout, stderr string, logger *zap.Logger) error {
	record := &bridge.Record{
		DataType:  9001, // 漏洞修复结果
		Timestamp: time.Now().UnixNano(),
		Data: &bridge.Payload{
			Fields: map[string]string{
				"task_id":   fmt.Sprintf("%d", taskID),
				"exit_code": fmt.Sprintf("%d", exitCode),
				"stdout":    stdout,
				"stderr":    stderr,
			},
		},
	}

	if err := client.SendRecord(record); err != nil {
		logger.Error("send result failed",
			zap.Uint("task_id", taskID),
			zap.Error(err))
		return fmt.Errorf("发送修复结果失败: %w", err)
	}

	logger.Info("result sent",
		zap.Uint("task_id", taskID),
		zap.Int("exit_code", exitCode))
	return nil
}

func newPluginLogger() (*zap.Logger, error) {
	config := zap.NewProductionEncoderConfig()
	config.TimeKey = "ts"
	config.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(config),
		zapcore.Lock(os.Stderr),
		zapcore.InfoLevel,
	)

	return zap.New(core, zap.AddCaller()), nil
}
