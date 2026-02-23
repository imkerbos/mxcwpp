// Package transport 提供 gRPC 传输功能（双向流）
package transport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/imkerbos/mxsec-platform/api/proto/bridge"
	"github.com/imkerbos/mxsec-platform/api/proto/grpc"
	"github.com/imkerbos/mxsec-platform/internal/agent/cache"
	"github.com/imkerbos/mxsec-platform/internal/agent/config"
	"github.com/imkerbos/mxsec-platform/internal/agent/connection"
)

// Manager 是传输管理器
type Manager struct {
	cfg            *config.Config
	logger         *zap.Logger
	connMgr        *connection.Manager
	agentID        string
	sendBuffer     chan *grpc.PackagedData
	pluginConfigCh chan []*grpc.Config                              // 插件配置通道
	agentUpdateCh  chan *grpc.AgentUpdate                           // Agent 更新通道
	taskCh         chan *grpc.Task                                  // 任务通道（兼容旧代码）
	taskChannels   map[string]chan *grpc.Task                       // 按插件名称分发的任务通道
	taskChMu       sync.RWMutex                                     // 任务通道锁
	onConfigUpdate func(*grpc.AgentConfig, *grpc.CertificateBundle) // 配置更新回调 (agentConfig, certBundle)
	cacheMgr       *cache.Manager                                   // 缓存管理器
	mu             sync.RWMutex
	isConnected    bool // 连接状态
	connectedMu    sync.RWMutex
}

// NewManager 创建新的传输管理器
func NewManager(cfg *config.Config, logger *zap.Logger, connMgr *connection.Manager, agentID string) (*Manager, error) {
	// 创建缓存管理器
	cacheDir := cfg.GetWorkDir() + "/cache"
	cacheMgr, err := cache.NewManager(cacheDir, 100*1024*1024, 7*24*time.Hour, logger) // 100MB, 7天
	if err != nil {
		return nil, fmt.Errorf("failed to create cache manager: %w", err)
	}

	return &Manager{
		cfg:            cfg,
		logger:         logger,
		connMgr:        connMgr,
		agentID:        agentID,
		sendBuffer:     make(chan *grpc.PackagedData, 2048),
		pluginConfigCh: make(chan []*grpc.Config, 10),
		agentUpdateCh:  make(chan *grpc.AgentUpdate, 10),
		taskCh:         make(chan *grpc.Task, 100),
		taskChannels:   make(map[string]chan *grpc.Task),
		cacheMgr:       cacheMgr,
		isConnected:    false,
	}, nil
}

// SetConfigUpdateCallback 设置配置更新回调
func (m *Manager) SetConfigUpdateCallback(callback func(*grpc.AgentConfig, *grpc.CertificateBundle)) {
	m.onConfigUpdate = callback
}

// Startup 启动传输模块（创建新的管理器）
func Startup(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *zap.Logger, connMgr *connection.Manager, agentID string) {
	mgr, err := NewManager(cfg, logger, connMgr, agentID)
	if err != nil {
		logger.Error("failed to create transport manager", zap.Error(err))
		wg.Done()
		return
	}
	StartupWithManager(ctx, wg, mgr)
}

// StartupWithManager 启动传输模块（使用已创建的管理器）
func StartupWithManager(ctx context.Context, wg *sync.WaitGroup, mgr *Manager) {
	defer wg.Done()

	// 启动缓存清理循环
	go mgr.cacheMgr.StartCleanupLoop(ctx)

	// 启动缓存重试循环
	go mgr.retryCachedData(ctx)

	mgr.logger.Info("transport module starting, attempting to connect...")

	// 指数退避重试配置
	retryDelay := 1 * time.Second    // 初始延迟1秒
	maxRetryDelay := 10 * time.Second // 最大延迟10秒
	retryCount := 0

	for {
		select {
		case <-ctx.Done():
			mgr.logger.Info("transport module shutting down")
			return
		default:
			// 获取连接
			mgr.logger.Debug("attempting to get connection",
				zap.Int("retry_count", retryCount),
				zap.Duration("retry_delay", retryDelay),
			)
			conn, err := mgr.connMgr.GetConnection(ctx)
			if err != nil {
				mgr.setConnected(false)
				retryCount++
				mgr.logger.Error("failed to get connection",
					zap.Error(err),
					zap.Int("retry_count", retryCount),
					zap.Duration("retry_delay", retryDelay),
				)
				// 指数退避：延迟时间 = min(初始延迟 * 2^(重试次数-1), 最大延迟)
				time.Sleep(retryDelay)
				retryDelay = retryDelay * 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
				continue
			}

			// 连接成功，重置重试计数和延迟
			retryCount = 0
			retryDelay = 1 * time.Second
			mgr.logger.Debug("connection obtained successfully")

			// 创建 gRPC 客户端
			mgr.logger.Debug("creating gRPC Transfer client")
			client := grpc.NewTransferClient(conn)
			stream, err := client.Transfer(ctx)
			if err != nil {
				mgr.setConnected(false)
				retryCount++
				mgr.logger.Error("failed to create stream",
					zap.Error(err),
					zap.Int("retry_count", retryCount),
					zap.Duration("retry_delay", retryDelay),
				)
				// 指数退避
				time.Sleep(retryDelay)
				retryDelay = retryDelay * 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
				continue
			}

			mgr.setConnected(true)
			mgr.logger.Info("gRPC stream established successfully",
				zap.String("agent_id", mgr.agentID),
			)

			// 连接建立后，先发送缓存的数据
			if err := mgr.sendCachedData(ctx, stream); err != nil {
				mgr.logger.Warn("failed to send cached data", zap.Error(err))
			}

			// 启动发送和接收 goroutine
			subWg := &sync.WaitGroup{}
			subWg.Add(2)

			go mgr.sendData(ctx, subWg, stream)
			go mgr.receiveCommands(ctx, subWg, stream)

			// 等待连接断开
			subWg.Wait()
			mgr.setConnected(false)
			mgr.logger.Warn("gRPC stream disconnected, reconnecting...",
				zap.String("agent_id", mgr.agentID),
			)
			// 连接断开后，重置重试延迟为初始值（快速重连）
			retryDelay = 1 * time.Second
			retryCount = 0
		}
	}
}

// sendWithTimeout 带超时的发送，防止 gRPC Send 因 server 反压永久阻塞
func (m *Manager) sendWithTimeout(stream grpc.Transfer_TransferClient, data *grpc.PackagedData, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- stream.Send(data)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("send timeout after %s", timeout)
	}
}

// sendData 发送数据到 Server
func (m *Manager) sendData(ctx context.Context, wg *sync.WaitGroup, stream grpc.Transfer_TransferClient) {
	defer wg.Done()

	m.logger.Debug("sendData goroutine started, waiting for data to send...")

	sendTimeout := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			m.logger.Debug("sendData goroutine stopping (context canceled)")
			return
		case data := <-m.sendBuffer:
			m.logger.Debug("sending data to server",
				zap.String("agent_id", data.AgentId),
				zap.String("hostname", data.Hostname),
				zap.Int("record_count", len(data.Records)),
			)
			if err := m.sendWithTimeout(stream, data, sendTimeout); err != nil {
				m.logger.Error("failed to send data, dropping stale buffer data",
					zap.Error(err),
					zap.String("error_type", fmt.Sprintf("%T", err)),
					zap.Int("record_count", len(data.Records)),
				)
				// 发送失败，丢弃 buffer 中的旧数据（状态快照类数据重连后会发最新的）
				m.drainSendBuffer()
				return
			}
			m.logger.Debug("data sent successfully",
				zap.Int("record_count", len(data.Records)),
				zap.String("agent_id", data.AgentId),
			)
		}
	}
}

// drainSendBuffer 清空 sendBuffer 中的剩余数据（状态快照类数据无需缓存，重连后会发最新的）
func (m *Manager) drainSendBuffer() {
	drained := 0
	for {
		select {
		case <-m.sendBuffer:
			drained++
		default:
			if drained > 0 {
				m.logger.Debug("drained stale data from send buffer", zap.Int("count", drained))
			}
			return
		}
	}
}

// receiveCommands 接收 Server 命令
func (m *Manager) receiveCommands(ctx context.Context, wg *sync.WaitGroup, stream grpc.Transfer_TransferClient) {
	defer wg.Done()

	m.logger.Debug("receiveCommands goroutine started, waiting for commands...")

	for {
		select {
		case <-ctx.Done():
			m.logger.Debug("receiveCommands goroutine stopping (context canceled)")
			return
		default:
			m.logger.Debug("waiting to receive command from server...")
			cmd, err := stream.Recv()
			if err != nil {
				if err != context.Canceled {
					m.logger.Error("failed to receive command",
						zap.Error(err),
						zap.String("error_type", fmt.Sprintf("%T", err)),
					)
				} else {
					m.logger.Debug("receiveCommands canceled by context")
				}
				return
			}

			m.logger.Debug("received command from server",
				zap.Int("task_count", len(cmd.Tasks)),
				zap.Int("config_count", len(cmd.Configs)),
				zap.Bool("has_agent_config", cmd.AgentConfig != nil),
				zap.Bool("has_certificate_bundle", cmd.CertificateBundle != nil),
			)

			// 处理 Agent 配置更新
			if cmd.AgentConfig != nil {
				m.logger.Info("received agent config update from server",
					zap.Int32("heartbeat_interval", cmd.AgentConfig.HeartbeatInterval),
					zap.String("work_dir", cmd.AgentConfig.WorkDir),
					zap.String("product", cmd.AgentConfig.Product),
					zap.String("version", cmd.AgentConfig.Version),
				)
				// 通知配置管理器更新配置（通过回调函数）
				if m.onConfigUpdate != nil {
					m.onConfigUpdate(cmd.AgentConfig, nil)
				} else {
					m.logger.Warn("agent config update callback not set")
				}
			}

			// 处理证书包更新（首次连接时）
			if cmd.CertificateBundle != nil {
				caCertLen := len(cmd.CertificateBundle.CaCert)
				clientCertLen := len(cmd.CertificateBundle.ClientCert)
				clientKeyLen := len(cmd.CertificateBundle.ClientKey)
				m.logger.Info("received certificate bundle from server",
					zap.Int("ca_cert_size", caCertLen),
					zap.Int("client_cert_size", clientCertLen),
					zap.Int("client_key_size", clientKeyLen),
				)
				// 通知配置管理器更新证书（通过回调函数）
				if m.onConfigUpdate != nil {
					m.onConfigUpdate(nil, cmd.CertificateBundle)
				} else {
					m.logger.Warn("certificate bundle update callback not set")
				}
			}

			// 处理插件配置更新
			if len(cmd.Configs) > 0 {
				m.logger.Info("received plugin configs from server", zap.Int("count", len(cmd.Configs)))
				select {
				case m.pluginConfigCh <- cmd.Configs:
				default:
					m.logger.Warn("plugin config channel full, dropping configs")
				}
			}

			// 处理 Agent 更新命令
			if cmd.AgentUpdate != nil {
				m.logger.Info("received agent update command from server",
					zap.String("version", cmd.AgentUpdate.Version),
					zap.String("download_url", cmd.AgentUpdate.DownloadUrl),
					zap.String("sha256", cmd.AgentUpdate.Sha256),
					zap.String("pkg_type", cmd.AgentUpdate.PkgType),
					zap.String("arch", cmd.AgentUpdate.Arch),
					zap.Bool("force", cmd.AgentUpdate.Force),
				)
				select {
				case m.agentUpdateCh <- cmd.AgentUpdate:
					m.logger.Debug("agent update command dispatched to channel")
				default:
					m.logger.Warn("agent update channel full, dropping update command")
				}
			}

			// 处理 Agent 重启命令
			if cmd.AgentRestart {
				m.logger.Info("received agent restart command from server")
				go func() {
					time.Sleep(2 * time.Second)
					restartCmd := exec.Command("systemctl", "restart", "mxsec-agent")
					if err := restartCmd.Start(); err != nil {
						m.logger.Error("failed to restart agent", zap.Error(err))
						os.Exit(0)
					}
				}()
			}

			// 处理任务（按插件名称分发到对应通道）
			if len(cmd.Tasks) > 0 {
				m.logger.Info("received tasks from server", zap.Int("count", len(cmd.Tasks)))
				for _, task := range cmd.Tasks {
					// 优先尝试分发到专用通道
					m.taskChMu.RLock()
					ch, ok := m.taskChannels[task.ObjectName]
					m.taskChMu.RUnlock()

					if ok {
						// 找到专用通道，发送到该通道
						select {
						case ch <- task:
							m.logger.Debug("task dispatched to plugin channel",
								zap.String("object_name", task.ObjectName),
								zap.String("token", task.Token))
						default:
							m.logger.Warn("plugin task channel full, dropping task",
								zap.String("object_name", task.ObjectName))
						}
					} else {
						// 没有专用通道，发送到通用通道（兼容旧代码）
						select {
						case m.taskCh <- task:
							m.logger.Debug("task dispatched to general channel",
								zap.String("object_name", task.ObjectName))
						default:
							m.logger.Warn("task channel full, dropping task",
								zap.String("object_name", task.ObjectName))
						}
					}
				}
			}

		}
	}
}

// SendHeartbeat 发送心跳数据
func (m *Manager) SendHeartbeat(data *grpc.PackagedData) error {
	// 检查连接状态
	if !m.IsConnected() {
		// 连接未建立，直接丢弃（心跳是状态快照，旧数据无价值，重连后会发最新的）
		m.logger.Debug("connection not established, dropping heartbeat (will send fresh data after reconnect)",
			zap.String("agent_id", data.AgentId),
		)
		return nil
	}

	// 连接已建立，尝试发送到缓冲区
	select {
	case m.sendBuffer <- data:
		return nil
	default:
		// 缓冲区满，丢弃最旧的一条，保留最新心跳（心跳时效性强，新的比旧的有价值）
		select {
		case <-m.sendBuffer:
			m.logger.Warn("send buffer full, dropped oldest data to make room for new heartbeat",
				zap.String("agent_id", data.AgentId),
			)
		default:
		}
		// 再次尝试放入
		select {
		case m.sendBuffer <- data:
			return nil
		default:
			m.logger.Warn("send buffer still full after drop, discarding heartbeat",
				zap.String("agent_id", data.AgentId),
			)
			return fmt.Errorf("send buffer full, heartbeat discarded")
		}
	}
}

// GetPluginConfigChannel 获取插件配置通道
func (m *Manager) GetPluginConfigChannel() <-chan []*grpc.Config {
	return m.pluginConfigCh
}

// GetAgentUpdateChannel 获取 Agent 更新通道
func (m *Manager) GetAgentUpdateChannel() <-chan *grpc.AgentUpdate {
	return m.agentUpdateCh
}

// SendPluginData 发送插件数据到 Server
func (m *Manager) SendPluginData(pluginName string, record *bridge.Record) error {
	// 序列化 Record
	recordData, err := proto.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal plugin record: %w", err)
	}

	// 构建 PackagedData
	packagedData := &grpc.PackagedData{
		Records: []*grpc.EncodedRecord{
			{
				DataType:  record.DataType,
				Timestamp: record.Timestamp,
				Data:      recordData,
			},
		},
		AgentId: m.agentID,
	}

	// 发送到缓冲区
	select {
	case m.sendBuffer <- packagedData:
		return nil
	default:
		// 缓冲区满，丢弃（资产数据是状态快照，下次采集会重新上报）
		m.logger.Warn("send buffer full, dropping plugin data (will re-collect next cycle)", zap.String("plugin", pluginName))
		return nil
	}
}

// sendCachedData 连接建立后清空旧缓存（心跳和资产数据都是状态快照，旧数据无价值，重放会导致旧版本号覆盖 DB）
func (m *Manager) sendCachedData(ctx context.Context, stream grpc.Transfer_TransferClient) error {
	purgedCount := 0
	for {
		_, filePath, err := m.cacheMgr.Get()
		if err != nil {
			m.logger.Error("failed to get cached data during purge", zap.Error(err))
			break
		}
		if filePath == "" {
			break // 没有缓存数据了
		}
		if err := m.cacheMgr.Remove(filePath); err != nil {
			m.logger.Warn("failed to remove cached file during purge", zap.String("file", filePath), zap.Error(err))
		}
		purgedCount++
	}

	if purgedCount > 0 {
		m.logger.Debug("purged stale cached data after reconnect (will send fresh heartbeat instead)",
			zap.Int("purged_count", purgedCount),
		)
	}

	return nil
}

// retryCachedData 定期清理残留缓存（正常情况下 sendCachedData 已清空，这里做兜底）
func (m *Manager) retryCachedData(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !m.IsConnected() {
				continue
			}

			// 清理残留缓存文件
			purgedCount := 0
			for i := 0; i < 50; i++ {
				_, filePath, err := m.cacheMgr.Get()
				if err != nil {
					break
				}
				if filePath == "" {
					break
				}
				if err := m.cacheMgr.Remove(filePath); err != nil {
					m.logger.Warn("failed to remove stale cached file", zap.String("file", filePath), zap.Error(err))
				}
				purgedCount++
			}
			if purgedCount > 0 {
				m.logger.Debug("purged stale cached data in retry loop", zap.Int("purged_count", purgedCount))
			}
		}
	}
}

// setConnected 设置连接状态
func (m *Manager) setConnected(connected bool) {
	m.connectedMu.Lock()
	defer m.connectedMu.Unlock()
	m.isConnected = connected
}

// IsConnected 返回连接状态
func (m *Manager) IsConnected() bool {
	m.connectedMu.RLock()
	defer m.connectedMu.RUnlock()
	return m.isConnected
}

// GetTaskChannel 获取任务通道（供插件管理器使用）
func (m *Manager) GetTaskChannel() <-chan *grpc.Task {
	return m.taskCh
}

// RegisterTaskChannel 为指定插件注册专用任务通道
// 总是创建新 channel，避免旧 sendTask 的 defer UnregisterTaskChannel close 到新 channel
func (m *Manager) RegisterTaskChannel(pluginName string) <-chan *grpc.Task {
	m.taskChMu.Lock()
	defer m.taskChMu.Unlock()

	ch := make(chan *grpc.Task, 100)
	m.taskChannels[pluginName] = ch
	m.logger.Debug("registered task channel for plugin", zap.String("plugin_name", pluginName))
	return ch
}

// UnregisterTaskChannel 注销插件的任务通道
// 仅从 map 中删除，不 close channel（让 GC 回收），避免 close 到新 channel 的风险
func (m *Manager) UnregisterTaskChannel(pluginName string) {
	m.taskChMu.Lock()
	defer m.taskChMu.Unlock()

	if _, ok := m.taskChannels[pluginName]; ok {
		delete(m.taskChannels, pluginName)
		m.logger.Debug("unregistered task channel for plugin", zap.String("plugin_name", pluginName))
	}
}

// GetTaskChannelForPlugin 获取指定插件的任务通道
func (m *Manager) GetTaskChannelForPlugin(pluginName string) <-chan *grpc.Task {
	m.taskChMu.RLock()
	defer m.taskChMu.RUnlock()

	if ch, ok := m.taskChannels[pluginName]; ok {
		return ch
	}
	return nil
}

// SendTaskToPlugin 向指定插件的任务通道发送任务（用于任务重试）
func (m *Manager) SendTaskToPlugin(pluginName string, task *grpc.Task) error {
	m.taskChMu.RLock()
	ch, ok := m.taskChannels[pluginName]
	m.taskChMu.RUnlock()

	if !ok {
		return fmt.Errorf("task channel not found for plugin: %s", pluginName)
	}

	select {
	case ch <- task:
		m.logger.Debug("task sent to plugin channel",
			zap.String("plugin", pluginName),
			zap.String("token", task.Token))
		return nil
	default:
		return fmt.Errorf("task channel full for plugin: %s", pluginName)
	}
}
