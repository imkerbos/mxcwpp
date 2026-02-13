// Package plugin 提供插件生命周期管理和 Pipe 通信功能
package plugin

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/api/proto/bridge"
	"github.com/imkerbos/mxsec-platform/api/proto/grpc"
	"github.com/imkerbos/mxsec-platform/internal/agent/config"
	"github.com/imkerbos/mxsec-platform/internal/agent/transport"
	"google.golang.org/protobuf/proto"
)

// Manager 是插件管理器
type Manager struct {
	cfg         *config.Config
	logger      *zap.Logger
	transport   *transport.Manager
	plugins     map[string]*Plugin // 插件名称 -> 插件实例
	mu          sync.RWMutex       // 保护 plugins map
	ctx         context.Context
	cancel      context.CancelFunc
	taskTracker *TaskTracker // 任务追踪器
}

// Plugin 表示一个插件实例
type Plugin struct {
	Config       *grpc.Config   // 插件配置
	cmd          *exec.Cmd      // 插件进程
	rx           *os.File       // 接收管道（Agent 从插件读取数据）
	tx           *os.File       // 发送管道（Agent 向插件写入任务）
	logWriter    io.WriteCloser // 日志写入器（支持日志轮转）
	workDir      string         // 插件工作目录
	status       Status         // 插件状态
	mu           sync.RWMutex   // 保护状态
	startTime    time.Time      // 启动时间
	lastActivity time.Time      // 最后一次收到插件数据的时间（用于健康检查）
	stopCh       chan struct{}  // 停止信号
	logger       *zap.Logger
}

// Status 是插件状态
type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusError    Status = "error"
)

// NewManager 创建新的插件管理器
func NewManager(cfg *config.Config, logger *zap.Logger, transportMgr *transport.Manager) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建任务追踪器
	taskTracker, err := NewTaskTracker(cfg.GetWorkDir(), logger)
	if err != nil {
		logger.Warn("failed to create task tracker, task recovery disabled", zap.Error(err))
	}

	return &Manager{
		cfg:         cfg,
		logger:      logger,
		transport:   transportMgr,
		plugins:     make(map[string]*Plugin),
		ctx:         ctx,
		cancel:      cancel,
		taskTracker: taskTracker,
	}
}

// Startup 启动插件管理模块（创建新的管理器）
func Startup(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *zap.Logger, transportMgr *transport.Manager) {
	mgr := NewManager(cfg, logger, transportMgr)
	StartupWithManager(ctx, wg, mgr)
}

// StartupWithManager 启动插件管理模块（使用已创建的管理器）
func StartupWithManager(ctx context.Context, wg *sync.WaitGroup, mgr *Manager) {
	defer wg.Done()

	// 监听插件配置更新
	configCh := mgr.transport.GetPluginConfigChannel()
	if configCh == nil {
		mgr.logger.Warn("plugin config channel not available")
		return
	}

	mgr.logger.Info("plugin manager started")

	// 启动插件健康检查 watchdog
	go mgr.watchPlugins()

	// 监听配置更新和上下文取消
	for {
		select {
		case <-ctx.Done():
			mgr.logger.Info("plugin manager shutting down")
			mgr.ShutdownAll()
			return
		case configs := <-configCh:
			// 同步插件配置
			if err := mgr.SyncPlugins(ctx, configs); err != nil {
				mgr.logger.Error("failed to sync plugins", zap.Error(err))
			}
		}
	}
}

// SyncPlugins 同步插件配置（从 Server 接收的配置）
func (m *Manager) SyncPlugins(ctx context.Context, configs []*grpc.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 构建当前配置的插件名称集合
	configMap := make(map[string]*grpc.Config)
	for _, cfg := range configs {
		configMap[cfg.Name] = cfg
	}

	// 停止已删除的插件
	for name, plugin := range m.plugins {
		if _, exists := configMap[name]; !exists {
			m.logger.Info("stopping removed plugin", zap.String("name", name))
			if err := m.stopPlugin(plugin); err != nil {
				m.logger.Error("failed to stop plugin", zap.String("name", name), zap.Error(err))
			}
			delete(m.plugins, name)
		}
	}

	// 启动或更新插件
	for _, cfg := range configs {
		plugin, exists := m.plugins[cfg.Name]
		if !exists {
			// 新插件，启动它
			m.logger.Info("loading new plugin", zap.String("name", cfg.Name), zap.String("version", cfg.Version))
			newPlugin, err := m.loadPlugin(ctx, cfg)
			if err != nil {
				m.logger.Error("failed to load plugin", zap.String("name", cfg.Name), zap.Error(err))
				continue
			}
			m.plugins[cfg.Name] = newPlugin
		} else {
			// 检查是否需要更新（使用版本比较）
			needsUpdate := false
			if plugin.Config.Sha256 != cfg.Sha256 {
				needsUpdate = true
			} else {
				// 使用版本比较判断是否需要更新
				oldVersion, err1 := ParseVersion(plugin.Config.Version)
				newVersion, err2 := ParseVersion(cfg.Version)
				if err1 != nil || err2 != nil {
					// 版本解析失败，使用字符串比较
					if plugin.Config.Version != cfg.Version {
						needsUpdate = true
					}
				} else if newVersion.GreaterThan(oldVersion) {
					needsUpdate = true
				}
			}

			if needsUpdate {
				m.logger.Info("updating plugin", zap.String("name", cfg.Name),
					zap.String("old_version", plugin.Config.Version),
					zap.String("new_version", cfg.Version))
				// 停止旧插件
				if err := m.stopPlugin(plugin); err != nil {
					m.logger.Error("failed to stop old plugin", zap.String("name", cfg.Name), zap.Error(err))
					// 如果停止失败，尝试强制停止
					if plugin.cmd.Process != nil {
						plugin.cmd.Process.Kill()
					}
				}
				// 等待一小段时间，确保进程完全退出
				time.Sleep(100 * time.Millisecond)
				// 启动新插件
				newPlugin, err := m.loadPlugin(ctx, cfg)
				if err != nil {
					m.logger.Error("failed to load updated plugin", zap.String("name", cfg.Name), zap.Error(err))
					// 更新失败，尝试回滚（重新启动旧版本）
					if oldPlugin, err := m.loadPlugin(ctx, plugin.Config); err == nil {
						m.logger.Info("rolled back to previous plugin version", zap.String("name", cfg.Name))
						m.plugins[cfg.Name] = oldPlugin
					}
					continue
				}
				m.plugins[cfg.Name] = newPlugin
			}
		}
	}

	return nil
}

// loadPlugin 加载插件
func (m *Manager) loadPlugin(ctx context.Context, cfg *grpc.Config) (*Plugin, error) {
	// 1. 验证插件配置
	if err := m.validatePluginConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid plugin config: %w", err)
	}

	// 2. 准备插件工作目录
	workDir := filepath.Join(m.cfg.GetWorkDir(), "plugins", cfg.Name)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work dir: %w", err)
	}

	// 3. 下载插件（如果不存在或签名不匹配）
	execPath, err := m.downloadPlugin(cfg, workDir)
	if err != nil {
		return nil, fmt.Errorf("failed to download plugin: %w", err)
	}

	// 4. 创建 Pipe
	rx_r, rx_w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create rx pipe: %w", err)
	}

	tx_r, tx_w, err := os.Pipe()
	if err != nil {
		rx_r.Close()
		rx_w.Close()
		return nil, fmt.Errorf("failed to create tx pipe: %w", err)
	}

	// 5. 设置日志文件重定向（带轮转功能）
	// 从 Agent 日志文件路径提取目录，插件日志放在同级的 plugins 子目录
	agentLogFile := m.cfg.Local.Log.File
	if agentLogFile == "" {
		agentLogFile = "/var/log/mxsec-agent/agent.log" // 默认路径
	}
	logDir := filepath.Join(filepath.Dir(agentLogFile), "plugins")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		rx_r.Close()
		rx_w.Close()
		tx_r.Close()
		tx_w.Close()
		return nil, fmt.Errorf("failed to create plugin log dir: %w", err)
	}

	// 配置日志轮转（与 Agent 保持一致：按天轮转，保留7天）
	logFile := filepath.Join(logDir, fmt.Sprintf("%s.log", cfg.Name))
	maxAge := 7 * 24 * time.Hour // 保留7天
	if m.cfg.Local.Log.MaxAge > 0 {
		maxAge = time.Duration(m.cfg.Local.Log.MaxAge) * 24 * time.Hour
	}

	logWriter, err := rotatelogs.New(
		logFile+".%Y-%m-%d",                      // 轮转后的文件名格式：{plugin}.log.YYYY-MM-DD
		rotatelogs.WithLinkName(logFile),         // 当前日志文件链接
		rotatelogs.WithMaxAge(maxAge),            // 保留时间（默认7天）
		rotatelogs.WithRotationTime(24*time.Hour), // 每24小时轮转一次
		rotatelogs.WithRotationCount(0),          // 不限制文件数量，由 MaxAge 控制
	)
	if err != nil {
		rx_r.Close()
		rx_w.Close()
		tx_r.Close()
		tx_w.Close()
		return nil, fmt.Errorf("failed to create plugin log rotator: %w", err)
	}

	// 6. 启动插件进程
	cmd := exec.CommandContext(ctx, execPath)
	cmd.Dir = workDir
	cmd.ExtraFiles = []*os.File{tx_r, rx_w} // 文件描述符 3 (tx_r), 4 (rx_w)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // 创建新的进程组
	}

	// 设置环境变量（可选）
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		logWriter.Close()
		rx_r.Close()
		rx_w.Close()
		tx_r.Close()
		tx_w.Close()
		return nil, fmt.Errorf("failed to start plugin: %w", err)
	}

	// 关闭子进程端不需要的文件描述符
	tx_r.Close()
	rx_w.Close()

	// 7. 创建插件实例
	now := time.Now()
	plugin := &Plugin{
		Config:       cfg,
		cmd:          cmd,
		rx:           rx_r,
		tx:           tx_w,
		logWriter:    logWriter,
		workDir:      workDir,
		status:       StatusStarting,
		startTime:    now,
		lastActivity: now, // 与 startTime 完全相等，After() 返回 false
		stopCh:       make(chan struct{}),
		logger:       m.logger.With(zap.String("plugin", cfg.Name)),
	}

	// 8. 启动管理 goroutine
	go m.waitProcess(plugin)
	go m.receiveData(plugin)
	go m.sendTask(plugin)

	// 等待一小段时间，确认插件启动成功
	time.Sleep(100 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil, fmt.Errorf("plugin exited immediately")
	}

	plugin.mu.Lock()
	plugin.status = StatusRunning
	plugin.mu.Unlock()

	plugin.logger.Info("plugin loaded successfully", zap.String("version", cfg.Version))

	// 9. 重新分发未完成的任务（如果有任务追踪器）
	if m.taskTracker != nil {
		go m.retryPendingTasks(plugin)
	}

	return plugin, nil
}

// validatePluginConfig 验证插件配置
func (m *Manager) validatePluginConfig(cfg *grpc.Config) error {
	if cfg.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if cfg.Version == "" {
		return fmt.Errorf("plugin version is required")
	}
	if len(cfg.DownloadUrls) == 0 {
		return fmt.Errorf("plugin download URLs are required")
	}
	return nil
}

// downloadPlugin 下载插件并验证 SHA256
func (m *Manager) downloadPlugin(cfg *grpc.Config, workDir string) (string, error) {
	execPath := filepath.Join(workDir, cfg.Name)

	// 检查插件是否已存在且 SHA256 匹配
	if info, err := os.Stat(execPath); err == nil {
		// 验证 SHA256
		if cfg.Sha256 != "" {
			actualSHA256, err := m.calculateSHA256(execPath)
			if err == nil && actualSHA256 == cfg.Sha256 {
				// SHA256 匹配，检查可执行权限
				if info.Mode().Perm()&0111 != 0 {
					m.logger.Debug("plugin already exists and SHA256 matches",
						zap.String("name", cfg.Name),
						zap.String("path", execPath),
						zap.String("sha256", cfg.Sha256))
					return execPath, nil
				}
				// SHA256 匹配但缺少可执行权限，设置权限
				if err := os.Chmod(execPath, 0755); err != nil {
					m.logger.Warn("failed to set executable permission", zap.Error(err))
				} else {
					return execPath, nil
				}
			} else if err == nil {
				// SHA256 不匹配，需要重新下载
				m.logger.Info("plugin SHA256 mismatch, re-downloading",
					zap.String("name", cfg.Name),
					zap.String("expected", cfg.Sha256),
					zap.String("actual", actualSHA256))
				// 删除旧文件
				if err := os.Remove(execPath); err != nil {
					m.logger.Warn("failed to remove old plugin", zap.Error(err))
				}
			}
		} else {
			// 没有 SHA256 配置，检查可执行权限
			if info.Mode().Perm()&0111 != 0 {
				m.logger.Debug("plugin already exists (no SHA256 check)",
					zap.String("name", cfg.Name),
					zap.String("path", execPath))
				return execPath, nil
			}
		}
	}

	// 下载插件
	if len(cfg.DownloadUrls) == 0 {
		return "", fmt.Errorf("no download URLs provided for plugin %s", cfg.Name)
	}

	// 尝试从每个 URL 下载
	var lastErr error
	for _, url := range cfg.DownloadUrls {
		m.logger.Info("downloading plugin",
			zap.String("name", cfg.Name),
			zap.String("url", url),
			zap.String("version", cfg.Version))

		if err := m.downloadFromURL(url, execPath); err != nil {
			lastErr = err
			m.logger.Warn("failed to download from URL", zap.String("url", url), zap.Error(err))
			continue
		}

		// 验证 SHA256
		if cfg.Sha256 != "" {
			actualSHA256, err := m.calculateSHA256(execPath)
			if err != nil {
				lastErr = fmt.Errorf("failed to calculate SHA256: %w", err)
				os.Remove(execPath)
				continue
			}

			if actualSHA256 != cfg.Sha256 {
				lastErr = fmt.Errorf("SHA256 mismatch: expected %s, got %s", cfg.Sha256, actualSHA256)
				m.logger.Error("SHA256 verification failed",
					zap.String("expected", cfg.Sha256),
					zap.String("actual", actualSHA256))
				os.Remove(execPath)
				continue
			}

			m.logger.Info("SHA256 verification passed", zap.String("sha256", cfg.Sha256))
		}

		// 设置可执行权限
		if err := os.Chmod(execPath, 0755); err != nil {
			lastErr = fmt.Errorf("failed to set executable permission: %w", err)
			os.Remove(execPath)
			continue
		}

		m.logger.Info("plugin downloaded successfully",
			zap.String("name", cfg.Name),
			zap.String("path", execPath))
		return execPath, nil
	}

	return "", fmt.Errorf("failed to download plugin from all URLs: %w", lastErr)
}

// downloadFromURL 从 URL 下载文件（支持 http://, https://, file:// 协议）
func (m *Manager) downloadFromURL(urlStr, destPath string) error {
	// 支持 file:// 协议（本地文件复制）
	if strings.HasPrefix(urlStr, "file://") {
		srcPath := strings.TrimPrefix(urlStr, "file://")
		return m.copyFile(srcPath, destPath)
	}

	// HTTP/HTTPS 下载
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	// 发送请求
	resp, err := client.Get(urlStr)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 创建目标文件
	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer destFile.Close()

	// 复制数据
	_, err = io.Copy(destFile, resp.Body)
	if err != nil {
		os.Remove(destPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// copyFile 复制文件（用于 file:// 协议）
func (m *Manager) copyFile(srcPath, destPath string) error {
	// 打开源文件
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", srcPath, err)
	}
	defer srcFile.Close()

	// 获取源文件信息
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// 创建目标文件
	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create dest file: %w", err)
	}
	defer destFile.Close()

	// 复制内容
	if _, err := io.Copy(destFile, srcFile); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// 设置可执行权限
	if err := os.Chmod(destPath, srcInfo.Mode()|0111); err != nil {
		m.logger.Warn("failed to set executable permission", zap.String("path", destPath), zap.Error(err))
	}

	m.logger.Info("copied plugin from local path",
		zap.String("src", srcPath),
		zap.String("dest", destPath),
		zap.String("size", fmt.Sprintf("%d bytes", srcInfo.Size())))

	return nil
}

// calculateSHA256 计算文件的 SHA256 校验和
func (m *Manager) calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// waitProcess 等待插件进程退出
func (m *Manager) waitProcess(plugin *Plugin) {
	err := plugin.cmd.Wait()

	plugin.mu.Lock()
	wasRunning := plugin.status == StatusRunning
	if plugin.status == StatusStopping {
		plugin.status = StatusStopped
	} else {
		plugin.status = StatusError
	}
	plugin.mu.Unlock()

	if err != nil {
		plugin.logger.Error("plugin process exited with error", zap.Error(err))
	} else {
		plugin.logger.Info("plugin process exited")
	}

	// 关闭管道
	plugin.rx.Close()
	plugin.tx.Close()

	// 关闭日志文件
	if plugin.logWriter != nil {
		plugin.logWriter.Close()
	}

	// 通知停止
	close(plugin.stopCh)

	// 如果插件是意外退出（不是主动停止），尝试重启
	if wasRunning && err != nil {
		plugin.logger.Warn("plugin crashed unexpectedly, will attempt to restart",
			zap.String("plugin", plugin.Config.Name),
			zap.Error(err))
		go m.restartPlugin(plugin)
	}
}

// restartPlugin 重启崩溃的插件
func (m *Manager) restartPlugin(oldPlugin *Plugin) {
	// 等待一小段时间，避免快速重启循环
	time.Sleep(3 * time.Second)

	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查插件是否还在管理列表中
	currentPlugin, exists := m.plugins[oldPlugin.Config.Name]
	if !exists || currentPlugin != oldPlugin {
		m.logger.Info("plugin already removed or replaced, skip restart",
			zap.String("plugin", oldPlugin.Config.Name))
		return
	}

	m.logger.Info("restarting crashed plugin",
		zap.String("plugin", oldPlugin.Config.Name),
		zap.String("version", oldPlugin.Config.Version))

	// 重新加载插件
	newPlugin, err := m.loadPlugin(m.ctx, oldPlugin.Config)
	if err != nil {
		m.logger.Error("failed to restart plugin",
			zap.String("plugin", oldPlugin.Config.Name),
			zap.Error(err))
		return
	}

	// 替换插件实例
	m.plugins[oldPlugin.Config.Name] = newPlugin
	m.logger.Info("plugin restarted successfully",
		zap.String("plugin", oldPlugin.Config.Name))
}

// watchPlugins 定期检查插件健康状态
// 检查策略：
// 1. 进程是否还存活（发送 signal 0 检测）
// 2. 如果有数据交互，检查是否超时（假死检测）
func (m *Manager) watchPlugins() {
	const checkInterval = 60 * time.Second  // 每 60 秒检查一次
	const silentThreshold = 5 * time.Minute // 有过数据交互的插件，超过 5 分钟无活动视为假死

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			for name, plugin := range m.plugins {
				plugin.mu.RLock()
				status := plugin.status
				lastAct := plugin.lastActivity
				startTime := plugin.startTime
				plugin.mu.RUnlock()

				if status != StatusRunning {
					continue
				}

				// 检查进程是否还存活（signal 0 不会杀进程，只检测是否存在）
				if plugin.cmd != nil && plugin.cmd.Process != nil {
					if err := plugin.cmd.Process.Signal(syscall.Signal(0)); err != nil {
						m.logger.Warn("plugin process not alive, will be cleaned up by waitProcess",
							zap.String("plugin", name),
							zap.Error(err),
						)
						continue
					}
				}

				// 插件刚启动不到 silentThreshold，跳过
				if time.Since(startTime) < silentThreshold {
					continue
				}

				// 只有插件曾经有过数据交互（lastActivity != startTime），才做假死检测
				// 如果插件从未发过数据（空闲状态），只靠进程存活检测
				if lastAct.After(startTime) && time.Since(lastAct) > silentThreshold {
					m.logger.Warn("plugin appears unresponsive, force restarting",
						zap.String("plugin", name),
						zap.Duration("silent_duration", time.Since(lastAct)),
						zap.Time("last_activity", lastAct),
					)
					go m.forceRestartPlugin(name)
				}
			}
			m.mu.RUnlock()
		}
	}
}

// forceRestartPlugin 强制重启指定插件
func (m *Manager) forceRestartPlugin(name string) {
	m.mu.RLock()
	plugin, exists := m.plugins[name]
	m.mu.RUnlock()

	if !exists {
		return
	}

	// 先停止（等待 waitProcess 完成全部清理）
	m.stopPlugin(plugin)

	// 显式确保任务通道已清理（防止旧 sendTask defer 延迟执行的竞争）
	m.transport.UnregisterTaskChannel(name)

	// 重启（不再需要 sleep，stopPlugin 已等待完整清理）
	m.restartPlugin(plugin)
}

// receiveData 接收插件数据（从 Pipe 读取）
func (m *Manager) receiveData(plugin *Plugin) {
	reader := bufio.NewReader(plugin.rx)

	for {
		select {
		case <-plugin.stopCh:
			return
		case <-m.ctx.Done():
			return
		default:
			// 读取长度（4 字节，小端序）
			var len uint32
			if err := binary.Read(reader, binary.LittleEndian, &len); err != nil {
				if err == io.EOF {
					return
				}
				plugin.logger.Error("failed to read record size", zap.Error(err))
				time.Sleep(time.Second)
				continue
			}

			// 限制最大消息大小
			const maxMessageSize = 10 * 1024 * 1024 // 10MB
			if len > maxMessageSize {
				plugin.logger.Error("record size exceeds maximum", zap.Uint32("size", len))
				// 跳过这个记录
				io.CopyN(io.Discard, reader, int64(len))
				continue
			}

			// 读取数据
			buf := make([]byte, len)
			if _, err := io.ReadFull(reader, buf); err != nil {
				plugin.logger.Error("failed to read record data", zap.Error(err))
				continue
			}

			// 解析 Record（Agent 不解析，直接透传到 Server）
			record := &bridge.Record{}
			if err := proto.Unmarshal(buf, record); err != nil {
				plugin.logger.Error("failed to unmarshal record", zap.Error(err))
				continue
			}

			// 更新最后活动时间（用于健康检查）
			plugin.mu.Lock()
			plugin.lastActivity = time.Now()
			plugin.mu.Unlock()

			// 检查是否是任务完成信号（DataType 8001 或 8004）
			if m.taskTracker != nil && (record.DataType == 8001 || record.DataType == 8004) {
				// 从 payload 中提取 task_id
				if record.Data != nil && record.Data.Fields != nil {
					if taskID, ok := record.Data.Fields["task_id"]; ok && taskID != "" {
						// 标记任务完成
						if err := m.taskTracker.MarkCompleted(taskID); err != nil {
							plugin.logger.Warn("failed to mark task as completed",
								zap.String("task_id", taskID),
								zap.Error(err))
						}
					}
				}
			}

			// 透传到 Server（通过 transport 模块）
			if err := m.transport.SendPluginData(plugin.Config.Name, record); err != nil {
				plugin.logger.Error("failed to send plugin data to server", zap.Error(err))
			}
		}
	}
}

// sendTask 发送任务到插件（写入 Pipe）
func (m *Manager) sendTask(plugin *Plugin) {
	writer := bufio.NewWriter(plugin.tx)
	defer writer.Flush()

	// 为该插件注册专用任务通道（按插件名称分发）
	taskCh := m.transport.RegisterTaskChannel(plugin.Config.Name)
	if taskCh == nil {
		plugin.logger.Warn("failed to register task channel")
		return
	}
	// 插件停止时注销任务通道
	defer m.transport.UnregisterTaskChannel(plugin.Config.Name)

	plugin.logger.Info("task channel registered for plugin",
		zap.String("plugin_name", plugin.Config.Name))

	for {
		select {
		case <-plugin.stopCh:
			return
		case <-m.ctx.Done():
			return
		case task, ok := <-taskCh:
			if !ok {
				// 通道已关闭
				return
			}

			// 追踪任务（如果有任务追踪器）
			if m.taskTracker != nil {
				if err := m.taskTracker.TrackTask(task, plugin.Config.Name); err != nil {
					plugin.logger.Error("failed to track task", zap.Error(err))
				}
			}

			// 序列化任务为 bridge.Task
			bridgeTask := &bridge.Task{
				DataType:   task.DataType,
				ObjectName: task.ObjectName,
				Data:       task.Data,
				Token:      task.Token,
			}

			// 序列化为 protobuf
			taskData, err := proto.Marshal(bridgeTask)
			if err != nil {
				plugin.logger.Error("failed to marshal task", zap.Error(err))
				if m.taskTracker != nil {
					m.taskTracker.MarkFailed(task.Token)
				}
				continue
			}

			// 写入长度（4 字节，小端序）
			len := uint32(len(taskData))
			if err := binary.Write(writer, binary.LittleEndian, len); err != nil {
				plugin.logger.Error("failed to write task size", zap.Error(err))
				if m.taskTracker != nil {
					m.taskTracker.MarkFailed(task.Token)
				}
				continue
			}

			// 写入数据
			if _, err := writer.Write(taskData); err != nil {
				plugin.logger.Error("failed to write task data", zap.Error(err))
				if m.taskTracker != nil {
					m.taskTracker.MarkFailed(task.Token)
				}
				continue
			}

			// 刷新缓冲区
			if err := writer.Flush(); err != nil {
				plugin.logger.Error("failed to flush task data", zap.Error(err))
				if m.taskTracker != nil {
					m.taskTracker.MarkFailed(task.Token)
				}
				continue
			}

			// 标记任务已分发
			if m.taskTracker != nil {
				if err := m.taskTracker.MarkDispatched(task.Token); err != nil {
					plugin.logger.Warn("failed to mark task as dispatched", zap.Error(err))
				}
			}

			plugin.logger.Info("task sent to plugin",
				zap.String("task_token", task.Token),
				zap.Int32("data_type", task.DataType))
		}
	}
}

// stopPlugin 停止插件
// 注意：不直接调用 cmd.Wait()，而是等待 waitProcess 通过 stopCh 通知完成，
// 避免与 waitProcess 的 Wait() 调用产生竞争。
func (m *Manager) stopPlugin(plugin *Plugin) error {
	plugin.mu.Lock()
	if plugin.status != StatusRunning && plugin.status != StatusStarting {
		plugin.mu.Unlock()
		return nil
	}
	plugin.status = StatusStopping
	plugin.mu.Unlock()

	plugin.logger.Info("stopping plugin")

	// 发送 SIGTERM 信号
	if plugin.cmd.Process != nil {
		if err := plugin.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			plugin.logger.Warn("failed to send SIGTERM", zap.Error(err))
		}
	}

	// 等待 waitProcess 完成清理（通过 stopCh 通知）
	select {
	case <-plugin.stopCh:
		plugin.logger.Info("plugin stopped")
	case <-time.After(5 * time.Second):
		plugin.logger.Warn("plugin did not stop gracefully, killing")
		if plugin.cmd.Process != nil {
			plugin.cmd.Process.Kill()
		}
		// Kill 后再等 waitProcess 完成
		select {
		case <-plugin.stopCh:
			plugin.logger.Info("plugin stopped after kill")
		case <-time.After(3 * time.Second):
			plugin.logger.Error("plugin failed to stop even after kill")
		}
	}

	return nil
}

// ShutdownAll 关闭所有插件
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cancel()

	for name, plugin := range m.plugins {
		m.logger.Info("shutting down plugin", zap.String("name", name))
		if err := m.stopPlugin(plugin); err != nil {
			m.logger.Error("failed to stop plugin", zap.String("name", name), zap.Error(err))
		}
	}

	m.plugins = make(map[string]*Plugin)
}

// GetPluginStatus 获取插件状态
func (m *Manager) GetPluginStatus(name string) (Status, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return StatusStopped, fmt.Errorf("plugin not found: %s", name)
	}

	plugin.mu.RLock()
	defer plugin.mu.RUnlock()

	return plugin.status, nil
}

// GetAllPluginStats 获取所有插件状态（用于心跳上报）
func (m *Manager) GetAllPluginStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	for name, plugin := range m.plugins {
		plugin.mu.RLock()
		stats[name] = map[string]interface{}{
			"status":     string(plugin.status),
			"version":    plugin.Config.Version,
			"start_time": plugin.startTime.Unix(),
		}
		plugin.mu.RUnlock()
	}

	return stats
}

// retryPendingTasks 重新分发未完成的任务
func (m *Manager) retryPendingTasks(plugin *Plugin) {
	// 等待插件完全启动
	time.Sleep(2 * time.Second)

	// 获取该插件的未完成任务
	pendingTasks := m.taskTracker.GetPendingTasks(plugin.Config.Name)
	if len(pendingTasks) == 0 {
		plugin.logger.Info("no pending tasks to retry")
		return
	}

	plugin.logger.Info("retrying pending tasks",
		zap.String("plugin", plugin.Config.Name),
		zap.Int("count", len(pendingTasks)))

	// 重新分发任务
	for _, task := range pendingTasks {
		if err := m.transport.SendTaskToPlugin(plugin.Config.Name, task); err != nil {
			plugin.logger.Error("failed to re-dispatch pending task",
				zap.String("token", task.Token),
				zap.Error(err))
		} else {
			plugin.logger.Info("pending task re-dispatched",
				zap.String("token", task.Token),
				zap.Int32("data_type", task.DataType))
		}
	}
}
