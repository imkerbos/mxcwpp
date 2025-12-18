// Package transfer 实现 Transfer gRPC 服务
package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/api/proto/bridge"
	grpcProto "github.com/mxcsec-platform/mxcsec-platform/api/proto/grpc"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/agentcenter/service"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/config"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/manager/biz"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// Connection 表示一个 Agent 连接
type Connection struct {
	AgentID  string
	Hostname string
	IPv4     []string
	IPv6     []string
	Version  string
	LastSeen time.Time
	stream   grpc.BidiStreamingServer[grpcProto.PackagedData, grpcProto.Command]
	ctx      context.Context
	cancel   context.CancelFunc
	sendCh   chan *grpcProto.Command
	mu       sync.RWMutex
}

// Service 是 Transfer 服务实现
type Service struct {
	grpcProto.UnimplementedTransferServer
	db               *gorm.DB
	logger           *zap.Logger
	cfg              *config.Config
	assetService     *service.AssetService
	metricsBuffer    *service.MetricsBuffer
	prometheusClient *service.PrometheusClient

	// 连接管理
	connections map[string]*Connection
	connMu      sync.RWMutex
}

// NewService 创建 Transfer 服务实例
func NewService(db *gorm.DB, logger *zap.Logger, cfg *config.Config) *Service {
	// 初始化资产服务
	assetService := service.NewAssetService(db, logger)

	var metricsBuffer *service.MetricsBuffer
	var prometheusClient *service.PrometheusClient

	// 根据配置初始化监控存储（二选一：MySQL 或 Prometheus）
	if cfg.Metrics.Prometheus.Enabled {
		// 验证 Prometheus 配置
		if cfg.Metrics.Prometheus.RemoteWriteURL == "" && cfg.Metrics.Prometheus.PushgatewayURL == "" {
			logger.Warn("Prometheus 已启用但未配置 URL，将回退到 MySQL 存储",
				zap.String("hint", "请配置 remote_write_url 或 pushgateway_url"),
			)
			// 回退到 MySQL 存储
			metricsBuffer = service.NewMetricsBuffer(
				db,
				logger,
				cfg.Metrics.MySQL.BatchSize,
				cfg.Metrics.MySQL.FlushInterval,
			)
			logger.Info("MySQL 监控指标存储已启用（Prometheus 配置无效，已回退）",
				zap.Int("retention_days", cfg.Metrics.MySQL.RetentionDays),
			)
		} else {
			// 启用 Prometheus：只使用 Prometheus，不使用 MySQL
			prometheusClient = service.NewPrometheusClient(
				cfg.Metrics.Prometheus.RemoteWriteURL,
				cfg.Metrics.Prometheus.PushgatewayURL,
				cfg.Metrics.Prometheus.JobName,
				cfg.Metrics.Prometheus.Timeout,
				logger,
			)
			logger.Info("Prometheus 监控指标存储已启用（MySQL 已禁用）",
				zap.String("remote_write_url", cfg.Metrics.Prometheus.RemoteWriteURL),
				zap.String("pushgateway_url", cfg.Metrics.Prometheus.PushgatewayURL),
				zap.String("job_name", cfg.Metrics.Prometheus.JobName),
				zap.String("note", "需要配置外部 Prometheus 服务，本项目不自动拉起"),
			)
		}
	} else {
		// 默认：使用 MySQL 存储
		metricsBuffer = service.NewMetricsBuffer(
			db,
			logger,
			cfg.Metrics.MySQL.BatchSize,
			cfg.Metrics.MySQL.FlushInterval,
		)
		logger.Info("MySQL 监控指标存储已启用（默认）",
			zap.Int("retention_days", cfg.Metrics.MySQL.RetentionDays),
			zap.Int("batch_size", cfg.Metrics.MySQL.BatchSize),
		)
	}

	return &Service{
		db:               db,
		logger:           logger,
		cfg:              cfg,
		assetService:     assetService,
		metricsBuffer:    metricsBuffer,
		prometheusClient: prometheusClient,
		connections:      make(map[string]*Connection),
	}
}

// Transfer 实现双向流 RPC
func (s *Service) Transfer(stream grpc.BidiStreamingServer[grpcProto.PackagedData, grpcProto.Command]) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 接收第一个 PackagedData 以获取 Agent ID
	firstData, err := stream.Recv()
	if err != nil {
		if err == io.EOF {
			return nil
		}
		return status.Errorf(codes.Internal, "接收数据失败: %v", err)
	}

	agentID := firstData.AgentId
	if agentID == "" {
		return status.Errorf(codes.InvalidArgument, "Agent ID 不能为空")
	}

	s.logger.Info("Agent 连接",
		zap.String("agent_id", agentID),
		zap.String("hostname", firstData.Hostname),
		zap.String("version", firstData.Version),
		zap.Strings("ipv4", append(firstData.IntranetIpv4, firstData.ExtranetIpv4...)),
		zap.Strings("ipv6", append(firstData.IntranetIpv6, firstData.ExtranetIpv6...)),
	)

	// 创建连接对象
	conn := &Connection{
		AgentID:  agentID,
		Hostname: firstData.Hostname,
		IPv4:     append(firstData.IntranetIpv4, firstData.ExtranetIpv4...),
		IPv6:     append(firstData.IntranetIpv6, firstData.ExtranetIpv6...),
		Version:  firstData.Version,
		LastSeen: time.Now(),
		stream:   stream,
		ctx:      ctx,
		cancel:   cancel,
		sendCh:   make(chan *grpcProto.Command, 10),
	}

	// 注册连接
	s.registerConnection(agentID, conn)
	defer s.unregisterConnection(agentID)

	// 检查并发送 Agent 上线恢复通知（如果之前离线）
	go s.checkAndSendAgentOnlineNotification(agentID, conn)

	// 检查并下发证书（首次连接时）
	if err := s.sendCertificateBundleIfNeeded(ctx, conn); err != nil {
		s.logger.Error("下发证书包失败", zap.Error(err), zap.String("agent_id", agentID))
		// 证书下发失败不影响连接，继续处理
	}

	// 下发插件配置（首次连接时）
	if err := s.sendPluginConfigsIfNeeded(ctx, conn); err != nil {
		s.logger.Error("下发插件配置失败", zap.Error(err), zap.String("agent_id", agentID))
		// 插件配置下发失败不影响连接，继续处理
	}

	// 处理第一个数据包（心跳）
	if err := s.handlePackagedData(ctx, firstData, conn); err != nil {
		s.logger.Error("处理数据包失败", zap.Error(err), zap.String("agent_id", agentID))
	}

	// 启动发送 goroutine
	go s.sendLoop(conn)

	// 接收循环
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			data, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					s.logger.Info("Agent 断开连接（EOF）",
						zap.String("agent_id", agentID),
						zap.String("hostname", conn.Hostname),
					)
					return nil
				}
				s.logger.Error("接收数据失败",
					zap.Error(err),
					zap.String("agent_id", agentID),
					zap.String("error_type", fmt.Sprintf("%T", err)),
				)
				return status.Errorf(codes.Internal, "接收数据失败: %v", err)
			}

			s.logger.Debug("收到Agent数据",
				zap.String("agent_id", agentID),
				zap.String("hostname", data.Hostname),
				zap.Int("record_count", len(data.Records)),
			)

			// 更新连接信息
			conn.mu.Lock()
			conn.LastSeen = time.Now()
			conn.Hostname = data.Hostname
			conn.IPv4 = append(data.IntranetIpv4, data.ExtranetIpv4...)
			conn.IPv6 = append(data.IntranetIpv6, data.ExtranetIpv6...)
			conn.mu.Unlock()

			// 处理数据包
			if err := s.handlePackagedData(ctx, data, conn); err != nil {
				s.logger.Error("处理数据包失败", zap.Error(err), zap.String("agent_id", agentID))
				// 继续处理下一个数据包，不中断连接
			}
		}
	}
}

// handlePackagedData 处理 PackagedData
func (s *Service) handlePackagedData(ctx context.Context, data *grpcProto.PackagedData, conn *Connection) error {
	// 处理心跳数据（从 PackagedData 中提取）
	if err := s.handleHeartbeat(ctx, data, conn); err != nil {
		s.logger.Error("处理心跳失败", zap.Error(err), zap.String("agent_id", conn.AgentID))
	}

	// 处理 EncodedRecord 列表
	for _, record := range data.Records {
		if err := s.handleEncodedRecord(ctx, record, conn); err != nil {
			s.logger.Error("处理记录失败",
				zap.Error(err),
				zap.String("agent_id", conn.AgentID),
				zap.Int32("data_type", record.DataType),
			)
			// 继续处理下一个记录
		}
	}

	return nil
}

// handleHeartbeat 处理心跳数据
func (s *Service) handleHeartbeat(ctx context.Context, data *grpcProto.PackagedData, conn *Connection) error {
	// 解析心跳记录中的额外字段
	var osInfo map[string]string
	var hardwareInfo map[string]string
	var networkInfo map[string]string
	var systemBootTime *time.Time
	var agentStartTime *time.Time
	var isContainer bool
	var containerID string
	var businessLine string
	var runtimeType model.RuntimeType = model.RuntimeTypeVM // 默认为 VM
	var podName, podNamespace, podUID string
	var hasHeartbeatData bool // 是否包含心跳数据
	if len(data.Records) > 0 {
		for _, record := range data.Records {
			if record.DataType == 1000 { // 心跳数据类型
				hasHeartbeatData = true
				// 解析 bridge.Record 获取OS、硬件和网络信息
				var bridgeRecord bridge.Record
				if err := proto.Unmarshal(record.Data, &bridgeRecord); err == nil {
					if bridgeRecord.Data != nil && bridgeRecord.Data.Fields != nil {
						fields := bridgeRecord.Data.Fields
						// OS信息
						osInfo = map[string]string{
							"os_family":      fields["os_family"],
							"os_version":     fields["os_version"],
							"kernel_version": fields["kernel"],
							"arch":           fields["arch"],
						}
						// 硬件信息
						hardwareInfo = map[string]string{
							"device_model":  fields["device_model"],
							"manufacturer":  fields["manufacturer"],
							"device_serial": fields["device_serial"],
							"cpu_info":      fields["cpu_info"],
							"memory_size":   fields["memory_size"],
							"system_load":   fields["system_load"],
						}
						// 网络信息
						networkInfo = map[string]string{
							"default_gateway": fields["default_gateway"],
							"dns_servers":     fields["dns_servers"],
							"network_mode":    fields["network_mode"],
						}
						// 磁盘和网卡信息（JSON 格式）
						if diskInfoStr, ok := fields["disk_info"]; ok && diskInfoStr != "" {
							networkInfo["disk_info"] = diskInfoStr
							s.logger.Debug("收到磁盘信息",
								zap.String("agent_id", conn.AgentID),
								zap.String("disk_info_length", fmt.Sprintf("%d", len(diskInfoStr))))
						} else {
							s.logger.Debug("未收到磁盘信息",
								zap.String("agent_id", conn.AgentID),
								zap.Bool("field_exists", ok))
						}
						if networkInterfacesStr, ok := fields["network_interfaces"]; ok && networkInterfacesStr != "" {
							networkInfo["network_interfaces"] = networkInterfacesStr
							s.logger.Debug("收到网卡信息",
								zap.String("agent_id", conn.AgentID),
								zap.String("network_interfaces_length", fmt.Sprintf("%d", len(networkInterfacesStr))))
						} else {
							s.logger.Debug("未收到网卡信息",
								zap.String("agent_id", conn.AgentID),
								zap.Bool("field_exists", ok))
						}
						// 解析系统启动时间
						if bootTimeStr, ok := fields["system_boot_time"]; ok && bootTimeStr != "" {
							if bootTime, err := time.Parse(time.RFC3339, bootTimeStr); err == nil {
								systemBootTime = &bootTime
							}
						}
						// 解析客户端启动时间
						if startTimeStr, ok := fields["agent_start_time"]; ok && startTimeStr != "" {
							if startTime, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
								agentStartTime = &startTime
							}
						}
						// 解析运行时类型
						if rtStr, ok := fields["runtime_type"]; ok && rtStr != "" {
							switch rtStr {
							case "vm":
								runtimeType = model.RuntimeTypeVM
							case "docker":
								runtimeType = model.RuntimeTypeDocker
							case "k8s":
								runtimeType = model.RuntimeTypeK8s
							default:
								runtimeType = model.RuntimeTypeVM
							}
						}
						// 解析容器环境标识
						if isContainerStr, ok := fields["is_container"]; ok && isContainerStr == "true" {
							isContainer = true
							if cid, ok := fields["container_id"]; ok && cid != "" {
								containerID = cid
							}
							// 如果是容器但 runtime_type 仍为默认值 VM，则修正为 Docker
							if runtimeType == model.RuntimeTypeVM {
								runtimeType = model.RuntimeTypeDocker
								s.logger.Debug("修正容器运行时类型",
									zap.String("agent_id", conn.AgentID),
									zap.String("runtime_type", string(runtimeType)))
							}
						}
						// 解析 K8s 相关字段
						if pn, ok := fields["pod_name"]; ok && pn != "" {
							podName = pn
						}
						if pns, ok := fields["pod_namespace"]; ok && pns != "" {
							podNamespace = pns
						}
						if puid, ok := fields["pod_uid"]; ok && puid != "" {
							podUID = puid
						}
						// 解析业务线（如果 Agent 提供了）
						if bl, ok := fields["business_line"]; ok && bl != "" {
							businessLine = bl
							s.logger.Debug("收到业务线信息",
								zap.String("agent_id", conn.AgentID),
								zap.String("business_line", businessLine))
						}
						// 解析并存储插件状态
						if pluginStatsStr, ok := fields["plugin_stats"]; ok && pluginStatsStr != "" {
							if err := s.storeHostPlugins(ctx, conn.AgentID, pluginStatsStr); err != nil {
								s.logger.Warn("存储插件状态失败",
									zap.String("agent_id", conn.AgentID),
									zap.Error(err))
							}
						}
					}
				}
				// 存储资源监控数据
				if err := s.storeHostMetrics(ctx, conn.AgentID, record); err != nil {
					s.logger.Warn("failed to store host metrics", zap.String("agent_id", conn.AgentID), zap.Error(err))
					// 不返回错误，避免影响心跳处理
				}
			}
		}
	}

	// 解析DNS服务器列表
	var dnsServers model.StringArray
	if dnsServersStr, ok := networkInfo["dns_servers"]; ok && dnsServersStr != "" {
		servers := strings.Split(dnsServersStr, ",")
		// 去除空格
		for i, s := range servers {
			servers[i] = strings.TrimSpace(s)
		}
		dnsServers = model.StringArray(servers)
	}

	// 更新或创建主机记录
	nowLocal := model.ToLocalTime(time.Now())
	host := &model.Host{
		HostID:        conn.AgentID,
		Hostname:      data.Hostname,
		IPv4:          model.StringArray(append(data.IntranetIpv4, data.ExtranetIpv4...)),
		IPv6:          model.StringArray(append(data.IntranetIpv6, data.ExtranetIpv6...)),
		Status:        model.HostStatusOnline,
		LastHeartbeat: &nowLocal,
		AgentVersion:  data.Version, // Agent 版本号
		// OS信息
		OSFamily:      osInfo["os_family"],
		OSVersion:     osInfo["os_version"],
		KernelVersion: osInfo["kernel_version"],
		Arch:          osInfo["arch"],
		// 硬件信息
		DeviceModel:  hardwareInfo["device_model"],
		Manufacturer: hardwareInfo["manufacturer"],
		DeviceSerial: hardwareInfo["device_serial"],
		CPUInfo:      hardwareInfo["cpu_info"],
		MemorySize:   hardwareInfo["memory_size"],
		SystemLoad:   hardwareInfo["system_load"],
		// 网络信息
		DefaultGateway: networkInfo["default_gateway"],
		DNSServers:     dnsServers,
		NetworkMode:    networkInfo["network_mode"],
		// 磁盘和网卡信息
		DiskInfo:          networkInfo["disk_info"],
		NetworkInterfaces: networkInfo["network_interfaces"],
		// 运行时环境
		RuntimeType: runtimeType,
		IsContainer: isContainer,
		ContainerID: containerID,
		// K8s 相关字段
		PodName:      podName,
		PodNamespace: podNamespace,
		PodUID:       podUID,
		// 时间信息
		SystemBootTime: model.ToLocalTimePtr(systemBootTime),
		AgentStartTime: model.ToLocalTimePtr(agentStartTime),
		// 业务线（如果 Agent 提供了，则使用；否则保持现有值）
		BusinessLine: businessLine,
	}

	// 使用 Save 方法（如果不存在则创建，存在则更新）
	result := s.db.Where("host_id = ?", conn.AgentID).FirstOrCreate(host)
	if result.Error != nil {
		return fmt.Errorf("查询主机失败: %w", result.Error)
	}

	// 如果主机已存在，更新字段
	if result.RowsAffected == 0 {
		updates := map[string]interface{}{
			"hostname":       data.Hostname,
			"ipv4":           model.StringArray(append(data.IntranetIpv4, data.ExtranetIpv4...)),
			"ipv6":           model.StringArray(append(data.IntranetIpv6, data.ExtranetIpv6...)),
			"status":         model.HostStatusOnline,
			"last_heartbeat": time.Now(),
			"agent_version":  data.Version, // Agent 版本号
		}
		// 只有当数据包中包含心跳数据时才更新这些字段
		if hasHeartbeatData {
			updates["os_family"] = osInfo["os_family"]
			updates["os_version"] = osInfo["os_version"]
			updates["kernel_version"] = osInfo["kernel_version"]
			updates["arch"] = osInfo["arch"]
			updates["device_model"] = hardwareInfo["device_model"]
			updates["manufacturer"] = hardwareInfo["manufacturer"]
			updates["device_serial"] = hardwareInfo["device_serial"]
			updates["cpu_info"] = hardwareInfo["cpu_info"]
			updates["memory_size"] = hardwareInfo["memory_size"]
			updates["system_load"] = hardwareInfo["system_load"]
			updates["default_gateway"] = networkInfo["default_gateway"]
			updates["dns_servers"] = dnsServers
			updates["network_mode"] = networkInfo["network_mode"]
			updates["disk_info"] = networkInfo["disk_info"]
			updates["network_interfaces"] = networkInfo["network_interfaces"]
			updates["runtime_type"] = runtimeType
			updates["is_container"] = isContainer
			updates["container_id"] = containerID
			updates["pod_name"] = podName
			updates["pod_namespace"] = podNamespace
			updates["pod_uid"] = podUID
			updates["system_boot_time"] = systemBootTime
			updates["agent_start_time"] = agentStartTime
		}
		// 如果 Agent 提供了业务线，则更新（仅在首次设置或 Agent 明确提供时更新）
		if businessLine != "" {
			updates["business_line"] = businessLine
		}
		// 只更新非空字段（但 agent_version 只有在非空时才更新）
		cleanUpdates := make(map[string]interface{})
		for k, v := range updates {
			if v == nil {
				continue
			}
			// agent_version 字段只有在非空时才更新（避免覆盖已有版本为空）
			if k == "agent_version" {
				if str, ok := v.(string); ok && str != "" {
					cleanUpdates[k] = v
				}
				continue
			}
			// 对于字符串，检查是否为空
			if str, ok := v.(string); ok {
				if str == "" {
					continue
				}
			}
			// 对于字符串数组，检查是否为空
			if strArray, ok := v.(model.StringArray); ok {
				if len(strArray) > 0 {
					cleanUpdates[k] = v
				}
			} else {
				// 对于时间指针，只有非 nil 时才更新
				if _, ok := v.(*time.Time); ok {
					cleanUpdates[k] = v
				} else {
					cleanUpdates[k] = v
				}
			}
		}
		if err := s.db.Model(&model.Host{}).Where("host_id = ?", conn.AgentID).Updates(cleanUpdates).Error; err != nil {
			return fmt.Errorf("更新主机失败: %w", err)
		}
	}

	s.logger.Debug("心跳处理完成",
		zap.String("agent_id", conn.AgentID),
		zap.String("hostname", data.Hostname),
		zap.Bool("has_disk_info", networkInfo["disk_info"] != ""),
		zap.Bool("has_network_interfaces", networkInfo["network_interfaces"] != ""),
	)

	return nil
}

// storeHostPlugins 存储主机插件状态
func (s *Service) storeHostPlugins(ctx context.Context, hostID string, pluginStatsJSON string) error {
	// 解析插件状态 JSON
	// 格式: {"baseline": {"status": "running", "version": "1.0.3", "start_time": 1234567890}, ...}
	var pluginStats map[string]struct {
		Status    string `json:"status"`
		Version   string `json:"version"`
		StartTime int64  `json:"start_time"`
	}

	if err := json.Unmarshal([]byte(pluginStatsJSON), &pluginStats); err != nil {
		return fmt.Errorf("解析插件状态 JSON 失败: %w", err)
	}

	if len(pluginStats) == 0 {
		return nil
	}

	s.logger.Debug("收到插件状态",
		zap.String("host_id", hostID),
		zap.Int("plugin_count", len(pluginStats)))

	// 更新或创建每个插件的状态
	for name, stats := range pluginStats {
		// 转换状态
		var status model.HostPluginStatus
		switch stats.Status {
		case "running":
			status = model.HostPluginStatusRunning
		case "stopped":
			status = model.HostPluginStatusStopped
		case "error":
			status = model.HostPluginStatusError
		default:
			status = model.HostPluginStatusRunning
		}

		// 转换启动时间
		var startTime *model.LocalTime
		if stats.StartTime > 0 {
			t := model.ToLocalTime(time.Unix(stats.StartTime, 0))
			startTime = &t
		}

		// 使用 UPSERT 更新或创建插件记录
		hostPlugin := model.HostPlugin{
			HostID:    hostID,
			Name:      name,
			Version:   stats.Version,
			Status:    status,
			StartTime: startTime,
		}

		// 查找现有记录
		var existing model.HostPlugin
		result := s.db.Where("host_id = ? AND name = ?", hostID, name).First(&existing)
		if result.Error == nil {
			// 记录存在，更新
			updates := map[string]interface{}{
				"version":    stats.Version,
				"status":     status,
				"start_time": startTime,
			}
			if err := s.db.Model(&existing).Updates(updates).Error; err != nil {
				s.logger.Error("更新插件状态失败",
					zap.String("host_id", hostID),
					zap.String("plugin_name", name),
					zap.Error(err))
				continue
			}
		} else {
			// 记录不存在，创建
			if err := s.db.Create(&hostPlugin).Error; err != nil {
				s.logger.Error("创建插件状态失败",
					zap.String("host_id", hostID),
					zap.String("plugin_name", name),
					zap.Error(err))
				continue
			}
		}

		s.logger.Debug("更新插件状态成功",
			zap.String("host_id", hostID),
			zap.String("plugin_name", name),
			zap.String("version", stats.Version),
			zap.String("status", string(status)))
	}

	return nil
}

// storeHostMetrics 存储主机监控指标
func (s *Service) storeHostMetrics(ctx context.Context, hostID string, record *grpcProto.EncodedRecord) error {
	// 解析 bridge.Record
	var bridgeRecord bridge.Record
	if err := proto.Unmarshal(record.Data, &bridgeRecord); err != nil {
		return fmt.Errorf("failed to unmarshal bridge record: %w", err)
	}

	// 提取资源指标字段
	fields := bridgeRecord.Data.Fields
	if fields == nil {
		return nil // 没有监控数据，跳过
	}

	// 检查是否有资源监控数据
	hasMetrics := false
	metric := &model.HostMetric{
		HostID:      hostID,
		CollectedAt: model.ToLocalTime(time.Unix(0, record.Timestamp)),
	}

	// 解析 CPU 使用率
	if cpuUsageStr := fields["cpu_usage_detailed"]; cpuUsageStr != "" {
		if cpuUsage := parseFloat(cpuUsageStr); cpuUsage != nil {
			metric.CPUUsage = cpuUsage
			hasMetrics = true
		}
	}

	// 解析内存使用率
	if memUsageStr := fields["mem_usage_detailed"]; memUsageStr != "" {
		if memUsage := parseFloat(memUsageStr); memUsage != nil {
			metric.MemUsage = memUsage
			hasMetrics = true
		}
	}

	// 解析磁盘使用率
	if diskUsageStr := fields["disk_usage"]; diskUsageStr != "" {
		if diskUsage := parseFloat(diskUsageStr); diskUsage != nil {
			metric.DiskUsage = diskUsage
			hasMetrics = true
		}
	}

	// 解析网络统计
	if netBytesSentStr := fields["net_bytes_sent"]; netBytesSentStr != "" {
		if netBytesSent := parseInt(netBytesSentStr); netBytesSent != nil {
			metric.NetBytesSent = netBytesSent
			hasMetrics = true
		}
	}

	if netBytesRecvStr := fields["net_bytes_recv"]; netBytesRecvStr != "" {
		if netBytesRecv := parseInt(netBytesRecvStr); netBytesRecv != nil {
			metric.NetBytesRecv = netBytesRecv
			hasMetrics = true
		}
	}

	// 存储监控数据（二选一：MySQL 或 Prometheus）
	if hasMetrics {
		var err error

		// 优先使用 Prometheus（如果启用），否则使用 MySQL
		if s.prometheusClient != nil {
			// 写入 Prometheus
			metricsMap := make(map[string]float64)
			if metric.CPUUsage != nil {
				metricsMap["cpu_usage"] = *metric.CPUUsage
			}
			if metric.MemUsage != nil {
				metricsMap["mem_usage"] = *metric.MemUsage
			}
			if metric.DiskUsage != nil {
				metricsMap["disk_usage"] = *metric.DiskUsage
			}
			if metric.NetBytesSent != nil {
				metricsMap["net_bytes_sent"] = float64(*metric.NetBytesSent)
			}
			if metric.NetBytesRecv != nil {
				metricsMap["net_bytes_recv"] = float64(*metric.NetBytesRecv)
			}

			if len(metricsMap) > 0 {
				err = s.prometheusClient.WriteMetrics(ctx, hostID, metricsMap, metric.CollectedAt.Time())
			}
		} else if s.metricsBuffer != nil {
			// 写入 MySQL（默认）
			err = s.metricsBuffer.Add(metric)
		}

		// 如果有错误，记录日志但不返回错误（避免影响心跳处理）
		if err != nil {
			s.logger.Warn("监控数据存储失败",
				zap.String("host_id", hostID),
				zap.Error(err),
			)
		}
	}

	return nil
}

// parseFloat 解析浮点数
func parseFloat(s string) *float64 {
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return nil
	}
	return &f
}

// parseInt 解析整数
func parseInt(s string) *uint64 {
	var i uint64
	if _, err := fmt.Sscanf(s, "%d", &i); err != nil {
		return nil
	}
	return &i
}

// handleEncodedRecord 处理 EncodedRecord
func (s *Service) handleEncodedRecord(ctx context.Context, record *grpcProto.EncodedRecord, conn *Connection) error {
	// 根据 data_type 路由到不同的处理器
	switch record.DataType {
	case 1000: // Agent 心跳（已在 handleHeartbeat 中处理）
		// 心跳数据通常不在这里处理，因为已经在 handleHeartbeat 中处理了
		return nil

	case 8000: // 基线检查结果
		return s.handleBaselineResult(ctx, record, conn)

	case 8001: // 任务完成信号
		return s.handleTaskCompletion(ctx, record, conn)

	case 5050, 5051, 5052, 5053, 5054, 5055, 5056, 5057, 5058, 5059, 5060:
		// 资产数据
		return s.assetService.HandleAssetData(conn.AgentID, record.DataType, record.Data)

	default:
		s.logger.Debug("未知数据类型",
			zap.String("agent_id", conn.AgentID),
			zap.Int32("data_type", record.DataType),
		)
		return nil
	}
}

// handleBaselineResult 处理基线检查结果
func (s *Service) handleBaselineResult(ctx context.Context, record *grpcProto.EncodedRecord, conn *Connection) error {
	// 解析 EncodedRecord.data 为 bridge.Record
	bridgeRecord := &bridge.Record{}
	if err := proto.Unmarshal(record.Data, bridgeRecord); err != nil {
		return fmt.Errorf("解析 Record 失败: %w", err)
	}

	// 从 Payload 中提取字段
	if bridgeRecord.Data == nil {
		return fmt.Errorf("Record.Data 为空")
	}
	fields := bridgeRecord.Data.Fields

	// 提取必要字段
	resultID := fields["result_id"]
	if resultID == "" {
		// 如果没有 result_id，生成一个（使用 UUID，确保不超过 64 字符）
		resultID = fmt.Sprintf("%s-%s-%d", conn.AgentID[:8], fields["rule_id"][:8], time.Now().UnixNano()%1000000000)
		// 如果还是太长，使用更短的格式
		if len(resultID) > 64 {
			resultID = fmt.Sprintf("%s-%s-%d", conn.AgentID[:8], fields["rule_id"][:8], time.Now().Unix()%1000000)
		}
	}
	hostID := conn.AgentID
	policyID := fields["policy_id"]
	ruleID := fields["rule_id"]
	taskID := fields["task_id"]
	status := fields["status"]
	severity := fields["severity"]
	category := fields["category"]
	title := fields["title"]
	actual := fields["actual"]
	expected := fields["expected"]
	fixSuggestion := fields["fix_suggestion"]

	// 解析时间戳
	timestamp := time.Unix(0, record.Timestamp)
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// 转换为 ResultStatus
	var resultStatus model.ResultStatus
	switch status {
	case "pass":
		resultStatus = model.ResultStatusPass
	case "fail":
		resultStatus = model.ResultStatusFail
	case "error":
		resultStatus = model.ResultStatusError
	case "na":
		resultStatus = model.ResultStatusNA
	default:
		resultStatus = model.ResultStatusError
	}

	// 创建 ScanResult
	scanResult := &model.ScanResult{
		ResultID:      resultID,
		HostID:        hostID,
		PolicyID:      policyID,
		RuleID:        ruleID,
		TaskID:        taskID,
		Status:        resultStatus,
		Severity:      severity,
		Category:      category,
		Title:         title,
		Actual:        actual,
		Expected:      expected,
		FixSuggestion: fixSuggestion,
		CheckedAt:     model.ToLocalTime(timestamp),
	}

	// 保存到数据库（使用 UPSERT 去重：基于 host_id + rule_id + task_id 唯一约束）
	// 如果同一任务、同一主机、同一规则已有结果，则更新
	var existingResult model.ScanResult
	queryCondition := s.db.Where("host_id = ? AND rule_id = ?", hostID, ruleID)
	if taskID != "" {
		queryCondition = queryCondition.Where("task_id = ?", taskID)
	}
	err := queryCondition.First(&existingResult).Error

	if err == gorm.ErrRecordNotFound {
		// 不存在，创建新记录
		if err := s.db.Create(scanResult).Error; err != nil {
			return fmt.Errorf("保存检测结果失败: %w", err)
		}
		s.logger.Info("检测结果已保存",
			zap.String("agent_id", conn.AgentID),
			zap.String("result_id", resultID),
			zap.String("rule_id", ruleID),
			zap.String("status", string(resultStatus)),
		)
	} else if err == nil {
		// 已存在，更新记录
		existingResult.Status = scanResult.Status
		existingResult.Actual = scanResult.Actual
		existingResult.Expected = scanResult.Expected
		existingResult.CheckedAt = scanResult.CheckedAt
		existingResult.Severity = scanResult.Severity
		existingResult.FixSuggestion = scanResult.FixSuggestion

		if err := s.db.Save(&existingResult).Error; err != nil {
			return fmt.Errorf("更新检测结果失败: %w", err)
		}
		// 使用已存在的 result_id 继续后续处理
		scanResult = &existingResult
		s.logger.Debug("检测结果已更新",
			zap.String("agent_id", conn.AgentID),
			zap.String("result_id", existingResult.ResultID),
			zap.String("rule_id", ruleID),
			zap.String("status", string(resultStatus)),
		)
	} else {
		return fmt.Errorf("查询检测结果失败: %w", err)
	}

	// 如果检测结果为 fail，创建或更新告警
	if resultStatus == model.ResultStatusFail {
		if err := s.createOrUpdateAlert(scanResult, conn); err != nil {
			s.logger.Warn("创建或更新告警失败",
				zap.String("result_id", resultID),
				zap.Error(err),
			)
			// 不中断流程，告警创建失败不影响检测结果保存
		}
	} else if resultStatus == model.ResultStatusPass {
		// 如果检测结果为 pass，检查是否有活跃告警需要恢复
		if err := s.resolveAlertIfExists(scanResult, conn); err != nil {
			s.logger.Warn("解决告警失败",
				zap.String("result_id", resultID),
				zap.Error(err),
			)
		}
	}

	return nil
}

// createOrUpdateAlert 创建或更新告警
func (s *Service) createOrUpdateAlert(scanResult *model.ScanResult, conn *Connection) error {
	// 查询是否已存在告警
	var existingAlert model.Alert
	err := s.db.Where("result_id = ?", scanResult.ResultID).First(&existingAlert).Error

	now := model.Now()

	if err == gorm.ErrRecordNotFound {
		// 创建新告警
		alert := &model.Alert{
			ResultID:      scanResult.ResultID,
			HostID:        scanResult.HostID,
			RuleID:        scanResult.RuleID,
			PolicyID:      scanResult.PolicyID,
			Severity:      scanResult.Severity,
			Category:      scanResult.Category,
			Title:         scanResult.Title,
			Description:   "", // 可以从 Rule 中获取
			Actual:        scanResult.Actual,
			Expected:      scanResult.Expected,
			FixSuggestion: scanResult.FixSuggestion,
			Status:        model.AlertStatusActive,
			FirstSeenAt:   now,
			LastSeenAt:    now,
		}

		if err := s.db.Create(alert).Error; err != nil {
			return fmt.Errorf("创建告警失败: %w", err)
		}

		// 发送告警通知
		s.sendAlertNotification(alert, conn)

		s.logger.Info("告警已创建",
			zap.Uint("alert_id", alert.ID),
			zap.String("result_id", scanResult.ResultID),
		)
	} else if err == nil {
		// 更新现有告警（更新最后发现时间）
		existingAlert.LastSeenAt = now
		// 如果告警已被解决或忽略，重新激活
		if existingAlert.Status != model.AlertStatusActive {
			existingAlert.Status = model.AlertStatusActive
			existingAlert.ResolvedAt = nil
			existingAlert.ResolvedBy = ""
			existingAlert.ResolveReason = ""
		}

		if err := s.db.Save(&existingAlert).Error; err != nil {
			return fmt.Errorf("更新告警失败: %w", err)
		}

		s.logger.Debug("告警已更新",
			zap.Uint("alert_id", existingAlert.ID),
			zap.String("result_id", scanResult.ResultID),
		)
	} else {
		return fmt.Errorf("查询告警失败: %w", err)
	}

	return nil
}

// resolveAlertIfExists 如果存在活跃告警则解决并发送恢复通知
func (s *Service) resolveAlertIfExists(scanResult *model.ScanResult, conn *Connection) error {
	// 查询是否存在活跃告警
	var existingAlert model.Alert
	err := s.db.Where("result_id = ? AND status = ?", scanResult.ResultID, model.AlertStatusActive).First(&existingAlert).Error

	if err == gorm.ErrRecordNotFound {
		// 没有活跃告警，无需处理
		return nil
	}
	if err != nil {
		return fmt.Errorf("查询告警失败: %w", err)
	}

	// 解决告警
	now := model.Now()
	existingAlert.Status = model.AlertStatusResolved
	existingAlert.ResolvedAt = &now
	existingAlert.ResolvedBy = "system" // 系统自动解决
	existingAlert.ResolveReason = "检测通过，问题已修复"

	if err := s.db.Save(&existingAlert).Error; err != nil {
		return fmt.Errorf("更新告警状态失败: %w", err)
	}

	s.logger.Info("告警已自动解决",
		zap.Uint("alert_id", existingAlert.ID),
		zap.String("result_id", scanResult.ResultID),
	)

	// 发送告警恢复通知（异步，不阻塞）
	go func() {
		// 查询主机信息
		var host model.Host
		if err := s.db.First(&host, "host_id = ?", existingAlert.HostID).Error; err != nil {
			s.logger.Warn("查询主机信息失败", zap.String("host_id", existingAlert.HostID), zap.Error(err))
			return
		}

		// 查询规则信息
		var rule model.Rule
		if err := s.db.First(&rule, "rule_id = ?", existingAlert.RuleID).Error; err != nil {
			s.logger.Warn("查询规则信息失败", zap.String("rule_id", existingAlert.RuleID), zap.Error(err))
		}

		// 获取主机 IP
		hostIP := ""
		if len(host.IPv4) > 0 {
			hostIP = strings.Join(host.IPv4, ",")
		} else if len(conn.IPv4) > 0 {
			hostIP = strings.Join(conn.IPv4, ",")
		}

		// 构建恢复数据
		resolvedData := &biz.AlertResolvedData{
			HostID:      existingAlert.HostID,
			Hostname:    host.Hostname,
			IP:          hostIP,
			OSFamily:    host.OSFamily,
			OSVersion:   host.OSVersion,
			RuleID:      existingAlert.RuleID,
			RuleName:    rule.Title,
			Category:    existingAlert.Category,
			Severity:    existingAlert.Severity,
			Title:       existingAlert.Title,
			FirstSeenAt: existingAlert.FirstSeenAt.Time(),
			ResolvedAt:  time.Now(),
			ResultID:    existingAlert.ResultID,
		}

		notificationService := biz.NewNotificationService(s.db, s.logger)
		if err := notificationService.SendAlertResolvedNotification(resolvedData); err != nil {
			s.logger.Warn("发送告警恢复通知失败",
				zap.Uint("alert_id", existingAlert.ID),
				zap.Error(err),
			)
		}
	}()

	return nil
}

// handleTaskCompletion 处理任务完成信号
func (s *Service) handleTaskCompletion(ctx context.Context, record *grpcProto.EncodedRecord, conn *Connection) error {
	// 解析 EncodedRecord.data 为 bridge.Record
	bridgeRecord := &bridge.Record{}
	if err := proto.Unmarshal(record.Data, bridgeRecord); err != nil {
		return fmt.Errorf("解析任务完成信号失败: %w", err)
	}

	// 从 Payload 中提取字段
	if bridgeRecord.Data == nil {
		return fmt.Errorf("Record.Data 为空")
	}
	fields := bridgeRecord.Data.Fields

	taskID := fields["task_id"]
	policyID := fields["policy_id"]
	status := fields["status"]
	resultCount := fields["result_count"]
	completedAt := fields["completed_at"]

	if taskID == "" {
		s.logger.Warn("任务完成信号缺少 task_id", zap.String("agent_id", conn.AgentID))
		return nil
	}

	s.logger.Info("收到任务完成信号",
		zap.String("agent_id", conn.AgentID),
		zap.String("task_id", taskID),
		zap.String("policy_id", policyID),
		zap.String("status", status),
		zap.String("result_count", resultCount),
		zap.String("completed_at", completedAt),
	)

	// 更新任务状态
	// 注意：一个任务可能分发给多个主机，我们需要跟踪每个主机的完成状态
	// 这里简化处理：当收到任何主机的完成信号时，检查是否所有主机都已完成
	// 如果全部完成，则更新任务状态为 completed

	// 查询任务
	var task model.ScanTask
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			s.logger.Warn("任务不存在", zap.String("task_id", taskID))
			return nil
		}
		return fmt.Errorf("查询任务失败: %w", err)
	}

	// 如果任务已经完成或失败，不再处理
	if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
		s.logger.Debug("任务已完成/失败/取消，忽略完成信号",
			zap.String("task_id", taskID),
			zap.String("status", string(task.Status)),
		)
		return nil
	}

	// 增加已完成主机数
	if err := s.db.Model(&task).Update("completed_host_count", gorm.Expr("completed_host_count + 1")).Error; err != nil {
		s.logger.Error("更新任务完成主机数失败", zap.String("task_id", taskID), zap.Error(err))
	}

	// 重新查询任务以获取最新的完成主机数
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return fmt.Errorf("查询任务失败: %w", err)
	}

	s.logger.Info("收到主机任务完成信号",
		zap.String("task_id", taskID),
		zap.String("host_id", conn.AgentID),
		zap.Int("completed_host_count", task.CompletedHostCount),
		zap.Int("dispatched_host_count", task.DispatchedHostCount),
	)

	// 检查是否所有主机都已完成
	if task.DispatchedHostCount > 0 && task.CompletedHostCount >= task.DispatchedHostCount {
		// 所有主机都已返回结果，标记任务为 completed
		now := model.Now()
		updates := map[string]interface{}{
			"status":       model.TaskStatusCompleted,
			"completed_at": &now,
		}

		if err := s.db.Model(&task).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新任务状态失败: %w", err)
		}

		s.logger.Info("任务所有主机都已完成，状态更新为 completed",
			zap.String("task_id", taskID),
			zap.Int("completed_host_count", task.CompletedHostCount),
			zap.Int("dispatched_host_count", task.DispatchedHostCount),
		)
	}

	return nil
}

// sendAlertNotification 发送告警通知（只发送立即通知模式）
func (s *Service) sendAlertNotification(alert *model.Alert, conn *Connection) {
	// 查询主机信息
	var host model.Host
	if err := s.db.First(&host, "host_id = ?", alert.HostID).Error; err != nil {
		s.logger.Warn("查询主机信息失败", zap.String("host_id", alert.HostID), zap.Error(err))
		return
	}

	// 查询规则信息
	var rule model.Rule
	if err := s.db.First(&rule, "rule_id = ?", alert.RuleID).Error; err != nil {
		s.logger.Warn("查询规则信息失败", zap.String("rule_id", alert.RuleID), zap.Error(err))
		// 规则信息不是必须的，继续发送通知
	}

	// 获取主机 IP（优先使用数据库中的 IP，如果没有则使用连接中的 IP）
	hostIP := ""
	if len(host.IPv4) > 0 {
		hostIP = strings.Join(host.IPv4, ",")
	} else if len(conn.IPv4) > 0 {
		hostIP = strings.Join(conn.IPv4, ",")
	}

	// 构建告警数据
	alertData := &biz.AlertData{
		HostID:        alert.HostID,
		Hostname:      host.Hostname,
		IP:            hostIP,
		OSFamily:      host.OSFamily,
		OSVersion:     host.OSVersion,
		BusinessLine:  host.BusinessLine, // 添加业务线
		RuleID:        alert.RuleID,
		RuleName:      rule.Title,
		Category:      alert.Category,
		Severity:      alert.Severity,
		Title:         alert.Title,
		Description:   rule.Description,
		Actual:        alert.Actual,
		Expected:      alert.Expected,
		FixSuggestion: alert.FixSuggestion,
		TaskID:        "", // 可以从 ScanResult 中获取
		PolicyID:      alert.PolicyID,
		CheckedAt:     alert.LastSeenAt.Time(),
		ResultID:      alert.ResultID,
	}

	// 发送通知（异步，不阻塞）
	go func() {
		notificationService := biz.NewNotificationService(s.db, s.logger)
		sent, err := notificationService.SendAlertNotification(alertData)
		if err != nil {
			s.logger.Warn("发送告警通知失败",
				zap.Uint("alert_id", alert.ID),
				zap.Error(err),
			)
		} else if sent {
			// 只有实际发送了通知才更新通知时间和通知次数
			now := model.Now()
			s.db.Model(&model.Alert{}).Where("id = ?", alert.ID).Updates(map[string]interface{}{
				"last_notified_at": &now,
				"notify_count":     gorm.Expr("notify_count + 1"),
			})
			s.logger.Info("告警通知已发送",
				zap.Uint("alert_id", alert.ID),
				zap.String("host_id", alert.HostID),
				zap.String("rule_id", alert.RuleID),
			)
		} else {
			s.logger.Debug("告警无匹配的通知配置",
				zap.Uint("alert_id", alert.ID),
				zap.String("severity", alert.Severity),
			)
		}
	}()
}

// sendLoop 发送循环（向 Agent 发送命令）
func (s *Service) sendLoop(conn *Connection) {
	s.logger.Debug("sendLoop goroutine started", zap.String("agent_id", conn.AgentID))

	for {
		select {
		case <-conn.ctx.Done():
			s.logger.Debug("sendLoop goroutine stopping (context canceled)", zap.String("agent_id", conn.AgentID))
			return
		case cmd := <-conn.sendCh:
			hasCertBundle := cmd.CertificateBundle != nil
			hasAgentConfig := cmd.AgentConfig != nil

			s.logger.Debug("准备发送命令到Agent",
				zap.String("agent_id", conn.AgentID),
				zap.Bool("has_certificate_bundle", hasCertBundle),
				zap.Bool("has_agent_config", hasAgentConfig),
				zap.Int("task_count", len(cmd.Tasks)),
				zap.Int("config_count", len(cmd.Configs)),
			)

			if err := conn.stream.Send(cmd); err != nil {
				s.logger.Error("发送命令失败",
					zap.Error(err),
					zap.String("agent_id", conn.AgentID),
					zap.String("error_type", fmt.Sprintf("%T", err)),
					zap.Bool("has_certificate_bundle", hasCertBundle),
					zap.Bool("has_agent_config", hasAgentConfig),
				)
				return
			}

			s.logger.Debug("命令发送成功",
				zap.String("agent_id", conn.AgentID),
				zap.Bool("has_certificate_bundle", hasCertBundle),
			)
		}
	}
}

// registerConnection 注册连接
func (s *Service) registerConnection(agentID string, conn *Connection) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.connections[agentID] = conn
}

// checkAndSendAgentOnlineNotification 检查并发送 Agent 上线恢复通知
func (s *Service) checkAndSendAgentOnlineNotification(agentID string, conn *Connection) {
	// 查询主机信息
	var host model.Host
	if err := s.db.First(&host, "host_id = ?", agentID).Error; err != nil {
		// 主机不存在（首次注册），不发送恢复通知
		return
	}

	// 检查主机上次心跳时间，如果超过 3 分钟，说明之前离线
	offlineThreshold := 3 * time.Minute
	if host.LastHeartbeat == nil {
		// 首次注册，没有心跳记录，不发送恢复通知
		return
	}
	lastHeartbeat := host.LastHeartbeat.Time()
	if time.Since(lastHeartbeat) < offlineThreshold {
		// 主机最近有心跳，不算离线
		return
	}

	s.logger.Info("检测到 Agent 从离线状态恢复上线",
		zap.String("agent_id", agentID),
		zap.String("hostname", host.Hostname),
		zap.Time("last_heartbeat", lastHeartbeat),
		zap.Duration("offline_duration", time.Since(lastHeartbeat)),
	)

	// 获取主机 IP
	hostIP := ""
	if len(conn.IPv4) > 0 {
		hostIP = strings.Join(conn.IPv4, ",")
	} else if len(host.IPv4) > 0 {
		hostIP = strings.Join(host.IPv4, ",")
	}

	// 构建上线数据
	onlineData := &biz.AgentOnlineData{
		HostID:       agentID,
		Hostname:     host.Hostname,
		IP:           hostIP,
		OSFamily:     host.OSFamily,
		OSVersion:    host.OSVersion,
		OnlineAt:     time.Now(),
		OfflineSince: lastHeartbeat,
	}

	// 发送上线恢复通知
	notificationService := biz.NewNotificationService(s.db, s.logger)
	if err := notificationService.SendAgentOnlineNotification(onlineData); err != nil {
		s.logger.Warn("发送 Agent 上线恢复通知失败",
			zap.String("agent_id", agentID),
			zap.Error(err),
		)
	}
}

// unregisterConnection 注销连接
func (s *Service) unregisterConnection(agentID string) {
	s.connMu.Lock()
	conn, exists := s.connections[agentID]
	delete(s.connections, agentID)
	s.connMu.Unlock()

	// 查询主机信息用于发送离线通知
	var host model.Host
	if err := s.db.First(&host, "host_id = ?", agentID).Error; err != nil {
		s.logger.Warn("查询主机信息失败，跳过离线通知", zap.String("agent_id", agentID), zap.Error(err))
	} else {
		// 发送 Agent 离线通知（异步，不阻塞）
		go func() {
			// 获取主机 IP（优先使用数据库中的 IP，如果没有则使用连接中的 IP）
			hostIP := ""
			if len(host.IPv4) > 0 {
				hostIP = strings.Join(host.IPv4, ",")
			} else if exists && conn != nil && len(conn.IPv4) > 0 {
				hostIP = strings.Join(conn.IPv4, ",")
			}

			offlineData := &biz.AgentOfflineData{
				HostID:       host.HostID,
				Hostname:     host.Hostname,
				IP:           hostIP,
				OSFamily:     host.OSFamily,
				OSVersion:    host.OSVersion,
				LastOnlineAt: host.LastHeartbeat.Time(),
				OfflineAt:    time.Now(),
			}

			notificationService := biz.NewNotificationService(s.db, s.logger)
			if err := notificationService.SendAgentOfflineNotification(offlineData); err != nil {
				s.logger.Warn("发送 Agent 离线通知失败",
					zap.String("agent_id", agentID),
					zap.Error(err),
				)
			}
		}()
	}

	// 更新主机状态为离线
	s.db.Model(&model.Host{}).Where("host_id = ?", agentID).Update("status", model.HostStatusOffline)

	s.logger.Info("Agent 连接已注销", zap.String("agent_id", agentID))
}

// sendCertificateBundleIfNeeded 检查并下发证书包（如果Agent首次连接）
// 理论上，AgentCenter的证书申请后一直使用，然后分发给Agent用于通信
func (s *Service) sendCertificateBundleIfNeeded(ctx context.Context, conn *Connection) error {
	// 读取Server端的证书文件
	caCertPath := s.cfg.MTLS.CACert
	// 客户端证书路径：从server_cert路径推导（例如 server.crt -> client.crt）
	// 如果server_cert是 "certs/server.crt"，则client_cert是 "certs/client.crt"
	serverCertPath := s.cfg.MTLS.ServerCert
	clientCertPath := serverCertPath
	if len(serverCertPath) > 0 {
		// 替换文件名：server.crt -> client.crt, server.key -> client.key
		clientCertPath = strings.Replace(serverCertPath, "server.crt", "client.crt", 1)
		clientCertPath = strings.Replace(clientCertPath, "server.key", "client.crt", 1)
	}
	clientKeyPath := strings.Replace(serverCertPath, "server.crt", "client.key", 1)
	clientKeyPath = strings.Replace(clientKeyPath, "server.key", "client.key", 1)

	s.logger.Debug("检查是否需要下发证书包",
		zap.String("agent_id", conn.AgentID),
		zap.String("ca_cert_path", caCertPath),
		zap.String("client_cert_path", clientCertPath),
		zap.String("client_key_path", clientKeyPath),
	)

	// 读取CA证书（用于Agent验证Server）
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return fmt.Errorf("读取CA证书失败: %w", err)
	}

	// 读取客户端证书（Agent使用）
	clientCert, err := os.ReadFile(clientCertPath)
	if err != nil {
		return fmt.Errorf("读取客户端证书失败: %w", err)
	}

	// 读取客户端密钥（Agent使用）
	clientKey, err := os.ReadFile(clientKeyPath)
	if err != nil {
		return fmt.Errorf("读取客户端密钥失败: %w", err)
	}

	// 构建证书包
	certBundle := &grpcProto.CertificateBundle{
		CaCert:     caCert,
		ClientCert: clientCert,
		ClientKey:  clientKey,
	}

	// 构建命令
	cmd := &grpcProto.Command{
		CertificateBundle: certBundle,
	}

	s.logger.Info("下发证书包到Agent",
		zap.String("agent_id", conn.AgentID),
		zap.Int("ca_cert_size", len(caCert)),
		zap.Int("client_cert_size", len(clientCert)),
		zap.Int("client_key_size", len(clientKey)),
	)

	// 发送证书包
	select {
	case conn.sendCh <- cmd:
		s.logger.Info("证书包已发送到Agent", zap.String("agent_id", conn.AgentID))
		return nil
	case <-conn.ctx.Done():
		return fmt.Errorf("连接已关闭: %s", conn.AgentID)
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("发送队列已满: %s", conn.AgentID)
	}
}

// sendPluginConfigsIfNeeded 下发插件配置给 Agent
func (s *Service) sendPluginConfigsIfNeeded(ctx context.Context, conn *Connection) error {
	// 从数据库查询启用的插件配置
	var pluginConfigs []model.PluginConfig
	if err := s.db.Where("enabled = ?", true).Find(&pluginConfigs).Error; err != nil {
		return fmt.Errorf("查询插件配置失败: %w", err)
	}

	if len(pluginConfigs) == 0 {
		s.logger.Debug("没有启用的插件配置", zap.String("agent_id", conn.AgentID))
		return nil
	}

	// 转换为 gRPC Config 格式
	var configs []*grpcProto.Config
	for _, pc := range pluginConfigs {
		config := &grpcProto.Config{
			Name:         pc.Name,
			Type:         string(pc.Type),
			Version:      pc.Version,
			Sha256:       pc.SHA256,
			Signature:    pc.Signature,
			DownloadUrls: []string(pc.DownloadURLs),
			Detail:       pc.Detail,
		}
		configs = append(configs, config)
	}

	// 构建命令
	cmd := &grpcProto.Command{
		Configs: configs,
	}

	s.logger.Info("下发插件配置到Agent",
		zap.String("agent_id", conn.AgentID),
		zap.Int("plugin_count", len(configs)),
	)

	// 发送插件配置
	select {
	case conn.sendCh <- cmd:
		s.logger.Info("插件配置已发送到Agent",
			zap.String("agent_id", conn.AgentID),
			zap.Int("plugin_count", len(configs)),
		)
		return nil
	case <-conn.ctx.Done():
		return fmt.Errorf("连接已关闭: %s", conn.AgentID)
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("发送队列已满: %s", conn.AgentID)
	}
}

// SendCommand 向指定 Agent 发送命令（供其他模块调用）
func (s *Service) SendCommand(agentID string, cmd *grpcProto.Command) error {
	s.connMu.RLock()
	conn, ok := s.connections[agentID]
	s.connMu.RUnlock()

	if !ok {
		return fmt.Errorf("Agent 未连接: %s", agentID)
	}

	select {
	case conn.sendCh <- cmd:
		return nil
	case <-conn.ctx.Done():
		return fmt.Errorf("连接已关闭: %s", agentID)
	default:
		return fmt.Errorf("发送队列已满: %s", agentID)
	}
}

// BroadcastPluginConfigs 向所有在线 Agent 广播插件配置（用于推送更新）
// 返回成功发送的 Agent 数量和失败的 Agent 列表
func (s *Service) BroadcastPluginConfigs(ctx context.Context) (int, []string, error) {
	// 从数据库查询启用的插件配置
	var pluginConfigs []model.PluginConfig
	if err := s.db.Where("enabled = ?", true).Find(&pluginConfigs).Error; err != nil {
		return 0, nil, fmt.Errorf("查询插件配置失败: %w", err)
	}

	if len(pluginConfigs) == 0 {
		s.logger.Info("没有启用的插件配置，跳过广播")
		return 0, nil, nil
	}

	// 转换为 gRPC Config 格式
	var configs []*grpcProto.Config
	for _, pc := range pluginConfigs {
		config := &grpcProto.Config{
			Name:         pc.Name,
			Type:         string(pc.Type),
			Version:      pc.Version,
			Sha256:       pc.SHA256,
			Signature:    pc.Signature,
			DownloadUrls: []string(pc.DownloadURLs),
			Detail:       pc.Detail,
		}
		configs = append(configs, config)
	}

	// 构建命令
	cmd := &grpcProto.Command{
		Configs: configs,
	}

	// 获取所有在线连接
	s.connMu.RLock()
	connections := make([]*Connection, 0, len(s.connections))
	for _, conn := range s.connections {
		connections = append(connections, conn)
	}
	s.connMu.RUnlock()

	if len(connections) == 0 {
		s.logger.Info("没有在线的 Agent，跳过广播")
		return 0, nil, nil
	}

	s.logger.Info("开始广播插件配置到所有在线 Agent",
		zap.Int("agent_count", len(connections)),
		zap.Int("plugin_count", len(configs)))

	// 向每个连接发送配置
	successCount := 0
	var failedAgents []string

	for _, conn := range connections {
		select {
		case conn.sendCh <- cmd:
			successCount++
			s.logger.Debug("插件配置已发送到 Agent",
				zap.String("agent_id", conn.AgentID))
		case <-conn.ctx.Done():
			failedAgents = append(failedAgents, conn.AgentID)
			s.logger.Warn("发送插件配置失败：连接已关闭",
				zap.String("agent_id", conn.AgentID))
		default:
			failedAgents = append(failedAgents, conn.AgentID)
			s.logger.Warn("发送插件配置失败：队列已满",
				zap.String("agent_id", conn.AgentID))
		}
	}

	s.logger.Info("插件配置广播完成",
		zap.Int("success_count", successCount),
		zap.Int("failed_count", len(failedAgents)))

	return successCount, failedAgents, nil
}

// GetOnlineAgentCount 获取在线 Agent 数量
func (s *Service) GetOnlineAgentCount() int {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return len(s.connections)
}

// GetOnlineAgentIDs 获取所有在线 Agent ID 列表
func (s *Service) GetOnlineAgentIDs() []string {
	s.connMu.RLock()
	defer s.connMu.RUnlock()

	ids := make([]string, 0, len(s.connections))
	for agentID := range s.connections {
		ids = append(ids, agentID)
	}
	return ids
}
