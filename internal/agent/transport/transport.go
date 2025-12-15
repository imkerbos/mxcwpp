// Package transport 提供 gRPC 传输功能（双向流）
package transport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/mxcsec-platform/mxcsec-platform/api/proto/bridge"
	"github.com/mxcsec-platform/mxcsec-platform/api/proto/grpc"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/cache"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/config"
	"github.com/mxcsec-platform/mxcsec-platform/internal/agent/connection"
)

// Manager 是传输管理器
type Manager struct {
	cfg            *config.Config
	logger         *zap.Logger
	connMgr        *connection.Manager
	agentID        string
	sendBuffer     chan *grpc.PackagedData
	recvBuffer     chan *grpc.Command
	pluginConfigCh chan []*grpc.Config                              // 插件配置通道
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
		sendBuffer:     make(chan *grpc.PackagedData, 100),
		recvBuffer:     make(chan *grpc.Command, 100),
		pluginConfigCh: make(chan []*grpc.Config, 10),
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
	retryDelay := 1 * time.Second     // 初始延迟1秒
	maxRetryDelay := 60 * time.Second // 最大延迟60秒
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

// sendData 发送数据到 Server
func (m *Manager) sendData(ctx context.Context, wg *sync.WaitGroup, stream grpc.Transfer_TransferClient) {
	defer wg.Done()

	m.logger.Info("sendData goroutine started, waiting for data to send...")

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Debug("sendData goroutine stopping (context canceled)")
			return
		case data := <-m.sendBuffer:
			// 直接从 channel 读取数据（优先处理）
			m.logger.Info("sending data to server",
				zap.String("agent_id", data.AgentId),
				zap.String("hostname", data.Hostname),
				zap.Int("record_count", len(data.Records)),
			)
			if err := stream.Send(data); err != nil {
				m.logger.Error("failed to send data, caching",
					zap.Error(err),
					zap.String("error_type", fmt.Sprintf("%T", err)),
					zap.Int("record_count", len(data.Records)),
				)
				// 发送失败，写入缓存
				if err := m.cacheMgr.Put(data); err != nil {
					m.logger.Error("failed to cache data", zap.Error(err))
				} else {
					m.logger.Info("data cached successfully for retry")
				}
				return
			}
			m.logger.Info("data sent successfully",
				zap.Int("record_count", len(data.Records)),
				zap.String("agent_id", data.AgentId),
			)
		case <-ticker.C:
			// 定期检查 sendBuffer（备用，但主要依赖直接 channel 读取）
			select {
			case data := <-m.sendBuffer:
				m.logger.Info("sending data to server (from ticker)",
					zap.String("agent_id", data.AgentId),
					zap.String("hostname", data.Hostname),
					zap.Int("record_count", len(data.Records)),
				)
				if err := stream.Send(data); err != nil {
					m.logger.Error("failed to send data, caching",
						zap.Error(err),
						zap.String("error_type", fmt.Sprintf("%T", err)),
						zap.Int("record_count", len(data.Records)),
					)
					if err := m.cacheMgr.Put(data); err != nil {
						m.logger.Error("failed to cache data", zap.Error(err))
					} else {
						m.logger.Info("data cached successfully for retry")
					}
					return
				}
				m.logger.Info("data sent successfully",
					zap.Int("record_count", len(data.Records)),
					zap.String("agent_id", data.AgentId),
				)
			default:
				// 没有数据，继续等待
			}
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

			// 将命令放入接收缓冲区（用于其他用途）
			select {
			case m.recvBuffer <- cmd:
			default:
				m.logger.Warn("receive buffer full, dropping command")
			}
		}
	}
}

// SendHeartbeat 发送心跳数据
func (m *Manager) SendHeartbeat(data *grpc.PackagedData) error {
	// 检查连接状态
	if !m.IsConnected() {
		// 连接未建立，先缓存数据
		m.logger.Debug("connection not established, caching heartbeat data",
			zap.String("agent_id", data.AgentId),
			zap.Int("record_count", len(data.Records)),
		)
		if err := m.cacheMgr.Put(data); err != nil {
			return fmt.Errorf("failed to cache heartbeat data: %w", err)
		}
		m.logger.Info("heartbeat data cached (will send after connection established)",
			zap.String("agent_id", data.AgentId),
		)
		return nil
	}

	// 连接已建立，尝试发送到缓冲区
	select {
	case m.sendBuffer <- data:
		return nil
	default:
		// 缓冲区满，尝试写入缓存
		m.logger.Warn("send buffer full, caching heartbeat data",
			zap.String("agent_id", data.AgentId),
		)
		if err := m.cacheMgr.Put(data); err != nil {
			return fmt.Errorf("send buffer full and cache failed: %w", err)
		}
		return fmt.Errorf("send buffer full, data cached")
	}
}

// ReceiveCommand 接收命令（非阻塞）
func (m *Manager) ReceiveCommand() (*grpc.Command, error) {
	select {
	case cmd := <-m.recvBuffer:
		return cmd, nil
	default:
		return nil, nil
	}
}

// GetPluginConfigChannel 获取插件配置通道
func (m *Manager) GetPluginConfigChannel() <-chan []*grpc.Config {
	return m.pluginConfigCh
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
		// 缓冲区满，尝试写入缓存
		if err := m.cacheMgr.Put(packagedData); err != nil {
			return fmt.Errorf("send buffer full and cache failed: %w", err)
		}
		m.logger.Debug("plugin data cached due to buffer full", zap.String("plugin", pluginName))
		return nil // 已缓存，不返回错误
	}
}

// sendCachedData 连接建立后立即发送缓存的数据
func (m *Manager) sendCachedData(ctx context.Context, stream grpc.Transfer_TransferClient) error {
	m.logger.Info("sending cached data after connection established")

	// 尝试发送缓存的数据（最多每次发送20条，避免阻塞）
	sentCount := 0
	maxSend := 20

	for i := 0; i < maxSend; i++ {
		data, filePath, err := m.cacheMgr.Get()
		if err != nil {
			m.logger.Error("failed to get cached data", zap.Error(err))
			break
		}
		if data == nil {
			break // 没有缓存数据
		}

		// 直接发送到流
		m.logger.Info("sending cached data to server",
			zap.String("agent_id", data.AgentId),
			zap.Int("record_count", len(data.Records)),
			zap.String("cache_file", filePath),
		)

		if err := stream.Send(data); err != nil {
			m.logger.Error("failed to send cached data, keeping in cache",
				zap.Error(err),
				zap.String("cache_file", filePath),
			)
			// 发送失败，数据仍在缓存中，下次重试时会再次尝试
			break
		}

		// 发送成功，删除缓存文件
		if err := m.cacheMgr.Remove(filePath); err != nil {
			m.logger.Warn("failed to remove cached file", zap.String("file", filePath), zap.Error(err))
		} else {
			m.logger.Info("cached data sent successfully and removed",
				zap.String("file", filePath),
			)
		}
		sentCount++
	}

	if sentCount > 0 {
		m.logger.Info("sent cached data after connection established",
			zap.Int("count", sentCount),
		)
	}

	return nil
}

// retryCachedData 定期重试发送缓存的数据
func (m *Manager) retryCachedData(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 只有在连接时才尝试发送缓存的数据
			if !m.IsConnected() {
				continue
			}

			// 尝试发送缓存的数据（最多每次发送10条）
			for i := 0; i < 10; i++ {
				data, filePath, err := m.cacheMgr.Get()
				if err != nil {
					m.logger.Error("failed to get cached data", zap.Error(err))
					break
				}
				if data == nil {
					break // 没有缓存数据
				}

				// 尝试发送
				select {
				case m.sendBuffer <- data:
					// 发送成功，删除缓存文件
					if err := m.cacheMgr.Remove(filePath); err != nil {
						m.logger.Warn("failed to remove cached file", zap.String("file", filePath), zap.Error(err))
					} else {
						m.logger.Debug("cached data sent successfully", zap.String("file", filePath))
					}
				default:
					// 缓冲区满，放回缓存（下次再试）
					return
				}
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
func (m *Manager) RegisterTaskChannel(pluginName string) <-chan *grpc.Task {
	m.taskChMu.Lock()
	defer m.taskChMu.Unlock()

	// 如果已存在，直接返回
	if ch, ok := m.taskChannels[pluginName]; ok {
		return ch
	}

	// 创建新的任务通道
	ch := make(chan *grpc.Task, 100)
	m.taskChannels[pluginName] = ch
	m.logger.Info("registered task channel for plugin", zap.String("plugin_name", pluginName))
	return ch
}

// UnregisterTaskChannel 注销插件的任务通道
func (m *Manager) UnregisterTaskChannel(pluginName string) {
	m.taskChMu.Lock()
	defer m.taskChMu.Unlock()

	if ch, ok := m.taskChannels[pluginName]; ok {
		close(ch)
		delete(m.taskChannels, pluginName)
		m.logger.Info("unregistered task channel for plugin", zap.String("plugin_name", pluginName))
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
