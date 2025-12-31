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

	"go.uber.org/zap"

	"github.com/mxcsec-platform/mxcsec-platform/api/proto/bridge"
	"github.com/mxcsec-platform/mxcsec-platform/api/proto/grpc"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/config"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/transport"
	"google.golang.org/protobuf/proto"
)

// Manager 是插件管理器
type Manager struct {
	cfg       *config.Config
	logger    *zap.Logger
	transport *transport.Manager
	plugins   map[string]*Plugin // 插件名称 -> 插件实例
	mu        sync.RWMutex       // 保护 plugins map
	ctx       context.Context
	cancel    context.CancelFunc
}

// Plugin 表示一个插件实例
type Plugin struct {
	Config    *grpc.Config  // 插件配置
	cmd       *exec.Cmd     // 插件进程
	rx        *os.File      // 接收管道（Agent 从插件读取数据）
	tx        *os.File      // 发送管道（Agent 向插件写入任务）
	logWriter *os.File      // 日志文件
	workDir   string        // 插件工作目录
	status    Status        // 插件状态
	mu        sync.RWMutex  // 保护状态
	startTime time.Time     // 启动时间
	stopCh    chan struct{} // 停止信号
	logger    *zap.Logger
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
	return &Manager{
		cfg:       cfg,
		logger:    logger,
		transport: transportMgr,
		plugins:   make(map[string]*Plugin),
		ctx:       ctx,
		cancel:    cancel,
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

	// 5. 设置日志文件重定向
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

	logFile := filepath.Join(logDir, fmt.Sprintf("%s.log", cfg.Name))
	logWriter, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		rx_r.Close()
		rx_w.Close()
		tx_r.Close()
		tx_w.Close()
		return nil, fmt.Errorf("failed to open plugin log file: %w", err)
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
	plugin := &Plugin{
		Config:    cfg,
		cmd:       cmd,
		rx:        rx_r,
		tx:        tx_w,
		logWriter: logWriter,
		workDir:   workDir,
		status:    StatusStarting,
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
		logger:    m.logger.With(zap.String("plugin", cfg.Name)),
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
				continue
			}

			// 写入长度（4 字节，小端序）
			len := uint32(len(taskData))
			if err := binary.Write(writer, binary.LittleEndian, len); err != nil {
				plugin.logger.Error("failed to write task size", zap.Error(err))
				continue
			}

			// 写入数据
			if _, err := writer.Write(taskData); err != nil {
				plugin.logger.Error("failed to write task data", zap.Error(err))
				continue
			}

			// 刷新缓冲区
			if err := writer.Flush(); err != nil {
				plugin.logger.Error("failed to flush task data", zap.Error(err))
				continue
			}

			plugin.logger.Info("task sent to plugin",
				zap.String("task_token", task.Token),
				zap.Int32("data_type", task.DataType))
		}
	}
}

// stopPlugin 停止插件
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

	// 等待进程退出（最多等待 5 秒）
	done := make(chan error, 1)
	go func() {
		done <- plugin.cmd.Wait()
	}()

	select {
	case <-done:
		plugin.logger.Info("plugin stopped")
	case <-time.After(5 * time.Second):
		plugin.logger.Warn("plugin did not stop gracefully, killing")
		if plugin.cmd.Process != nil {
			plugin.cmd.Process.Kill()
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
