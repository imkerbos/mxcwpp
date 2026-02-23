package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

const (
	aideConfigPath   = "/etc/aide-mxsec.conf"
	aideDBPath       = "/var/lib/aide/aide-mxsec.db.gz"
	aideNewDBPath    = "/var/lib/aide/aide-mxsec.db.new.gz"
	aideCheckTimeout = 30 * time.Minute
)

// Engine FIM 检查引擎
type Engine struct {
	logger *zap.Logger
}

// NewEngine 创建引擎实例
func NewEngine(logger *zap.Logger) *Engine {
	return &Engine{logger: logger}
}

// Execute 执行 FIM 检查流程
func (e *Engine) Execute(ctx context.Context, taskData json.RawMessage) (*ExecuteResult, error) {
	// 1. 解析策略
	policy, err := e.parsePolicyFromTask(taskData)
	if err != nil {
		return nil, fmt.Errorf("解析策略失败: %w", err)
	}

	// 2. 检查 AIDE 是否安装
	if err := e.checkAIDEInstalled(); err != nil {
		return nil, err
	}

	// 3. 渲染配置文件
	e.logger.Info("渲染 AIDE 配置", zap.String("config_path", aideConfigPath))
	if err := Render(policy, aideConfigPath); err != nil {
		return nil, fmt.Errorf("渲染配置失败: %w", err)
	}

	// 4. 确保 AIDE 数据库存在
	if err := e.ensureAIDEDB(ctx); err != nil {
		return nil, fmt.Errorf("初始化 AIDE 数据库失败: %w", err)
	}

	// 5. 执行 AIDE 检查
	output, err := e.runAIDECheck(ctx)
	if err != nil {
		return nil, fmt.Errorf("AIDE 检查失败: %w", err)
	}

	// 6. 解析输出
	report := Parse(output)

	// 7. 分类每个事件
	for i := range report.Events {
		Classify(&report.Events[i])
	}

	// 8. 更新数据库（后台，不影响结果）
	go e.updateAIDEDB(context.Background())

	return &ExecuteResult{
		Summary: report.Summary,
		Events:  report.Events,
	}, nil
}

// RenderConfig 仅渲染配置文件（用于策略更新）
func (e *Engine) RenderConfig(taskData json.RawMessage) error {
	policy, err := e.parsePolicyFromTask(taskData)
	if err != nil {
		return fmt.Errorf("解析策略失败: %w", err)
	}
	return Render(policy, aideConfigPath)
}

// parsePolicyFromTask 从任务 JSON 提取策略配置
func (e *Engine) parsePolicyFromTask(taskData json.RawMessage) (*FIMPolicy, error) {
	var policy FIMPolicy
	if err := json.Unmarshal(taskData, &policy); err != nil {
		return nil, fmt.Errorf("解析策略 JSON 失败: %w", err)
	}
	if len(policy.WatchPaths) == 0 {
		return nil, fmt.Errorf("策略未配置监控路径")
	}
	return &policy, nil
}

// checkAIDEInstalled 检查 AIDE 是否已安装
func (e *Engine) checkAIDEInstalled() error {
	_, err := exec.LookPath("aide")
	if err != nil {
		return fmt.Errorf("AIDE 未安装，请先安装: yum install aide 或 apt install aide")
	}
	return nil
}

// ensureAIDEDB 确保 AIDE 数据库存在，不存在则初始化
func (e *Engine) ensureAIDEDB(ctx context.Context) error {
	if _, err := os.Stat(aideDBPath); err == nil {
		return nil // 数据库已存在
	}

	e.logger.Info("AIDE 数据库不存在，执行初始化")

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(aideDBPath), 0700); err != nil {
		return fmt.Errorf("创建 AIDE 数据库目录失败: %w", err)
	}

	initCtx, cancel := context.WithTimeout(ctx, aideCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(initCtx, "aide", "--init", "-c", aideConfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aide --init 失败: %w, output: %s", err, string(output))
	}

	// mv new db to db
	if err := os.Rename(aideNewDBPath, aideDBPath); err != nil {
		return fmt.Errorf("移动 AIDE 数据库失败: %w", err)
	}

	e.logger.Info("AIDE 数据库初始化完成")
	return nil
}

// runAIDECheck 执行 AIDE 检查
func (e *Engine) runAIDECheck(ctx context.Context) (string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, aideCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, "aide", "--check", "-c", aideConfigPath)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// AIDE exit codes: 1-7 表示检测到变更（非错误），>7 为真正错误
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if code >= 1 && code <= 7 {
				e.logger.Info("AIDE 检测到变更", zap.Int("exit_code", code))
				return string(output), nil
			}
			return "", fmt.Errorf("aide --check 异常退出 (code=%d): %s", code, string(output))
		}
		return "", fmt.Errorf("aide --check 执行失败: %w", err)
	}

	// exit code 0 = 无变更
	e.logger.Info("AIDE 检查完成，无变更")
	return string(output), nil
}

// updateAIDEDB 更新 AIDE 数据库（检查完成后执行）
func (e *Engine) updateAIDEDB(ctx context.Context) {
	updateCtx, cancel := context.WithTimeout(ctx, aideCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(updateCtx, "aide", "--update", "-c", aideConfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		e.logger.Warn("aide --update 失败", zap.Error(err), zap.String("output", string(output)))
		return
	}

	if err := os.Rename(aideNewDBPath, aideDBPath); err != nil {
		e.logger.Warn("移动更新后的 AIDE 数据库失败", zap.Error(err))
		return
	}

	e.logger.Info("AIDE 数据库已更新")
}