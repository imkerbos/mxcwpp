// Package service 提供 AgentCenter 的业务逻辑服务
package service

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/api/proto/bridge"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
	"github.com/imkerbos/mxsec-platform/plugins/collector/engine"
)

// AssetService 资产数据处理服务
type AssetService struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewAssetService 创建资产服务
func NewAssetService(db *gorm.DB, logger *zap.Logger) *AssetService {
	return &AssetService{
		db:     db,
		logger: logger,
	}
}

// HandleAssetData 处理资产数据
func (s *AssetService) HandleAssetData(hostID string, dataType int32, data []byte) error {
	// 解析 bridge.Record
	bridgeRecord := &bridge.Record{}
	if err := proto.Unmarshal(data, bridgeRecord); err != nil {
		return fmt.Errorf("failed to unmarshal bridge record: %w", err)
	}

	// 从 Payload 中提取 JSON 数据
	jsonData, ok := bridgeRecord.Data.Fields["data"]
	if !ok {
		return fmt.Errorf("missing data field in payload")
	}

	// 根据 data_type 路由到不同的处理器
	switch dataType {
	case 5050: // 进程数据
		return s.handleProcessData(hostID, jsonData)
	case 5051: // 端口数据
		return s.handlePortData(hostID, jsonData)
	case 5052: // 账户数据
		return s.handleUserData(hostID, jsonData)
	case 5053: // 软件包数据
		return s.handleSoftwareData(hostID, jsonData)
	case 5054: // 容器数据
		return s.handleContainerData(hostID, jsonData)
	case 5055: // 应用数据
		return s.handleAppData(hostID, jsonData)
	case 5056: // 网络接口数据
		return s.handleNetInterfaceData(hostID, jsonData)
	case 5057: // 磁盘数据
		return s.handleVolumeData(hostID, jsonData)
	case 5058: // 内核模块数据
		return s.handleKmodData(hostID, jsonData)
	case 5059: // 系统服务数据
		return s.handleServiceData(hostID, jsonData)
	case 5060: // 定时任务数据
		return s.handleCronData(hostID, jsonData)
	default:
		s.logger.Warn("unknown asset data type",
			zap.Int32("data_type", dataType),
			zap.String("host_id", hostID))
		return nil
	}
}

// handleProcessData 处理进程数据
func (s *AssetService) handleProcessData(hostID, jsonData string) error {
	// 解析 JSON 数据（可能是数组）
	var assets []engine.ProcessAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		// 尝试解析单个对象
		var asset engine.ProcessAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal process data: %w", err)
		}
		assets = []engine.ProcessAsset{asset}
	}

	// 批量插入数据库（使用 ON DUPLICATE KEY UPDATE 或先删除再插入）
	// 策略：删除该主机的旧数据，插入新数据（全量替换）
	if err := s.db.Where("host_id = ?", hostID).Delete(&model.Process{}).Error; err != nil {
		return fmt.Errorf("failed to delete old process data: %w", err)
	}

	// 转换为数据库模型并插入
	for _, asset := range assets {
		process := &model.Process{
			ID:          fmt.Sprintf("%s-%s", hostID, asset.PID),
			HostID:      hostID,
			PID:         asset.PID,
			PPID:        asset.PPID,
			Cmdline:     asset.Cmdline,
			Exe:         asset.Exe,
			ExeHash:     asset.ExeHash,
			ContainerID: asset.ContainerID,
			UID:         asset.UID,
			GID:         asset.GID,
			Username:    asset.Username,
			Groupname:   asset.Groupname,
			CollectedAt: model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(process).Error; err != nil {
			s.logger.Error("failed to create process",
				zap.String("host_id", hostID),
				zap.String("pid", asset.PID),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed process data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handlePortData 处理端口数据
func (s *AssetService) handlePortData(hostID, jsonData string) error {
	// 解析 JSON 数据（可能是数组）
	var assets []engine.PortAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		// 尝试解析单个对象
		var asset engine.PortAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal port data: %w", err)
		}
		assets = []engine.PortAsset{asset}
	}

	// 删除旧数据
	if err := s.db.Where("host_id = ?", hostID).Delete(&model.Port{}).Error; err != nil {
		return fmt.Errorf("failed to delete old port data: %w", err)
	}

	// 转换为数据库模型并插入
	for _, asset := range assets {
		port := &model.Port{
			ID:          fmt.Sprintf("%s-%s-%d", hostID, asset.Protocol, asset.Port),
			HostID:      hostID,
			Protocol:    asset.Protocol,
			Port:        asset.Port,
			State:       asset.State,
			PID:         asset.PID,
			ProcessName: asset.ProcessName,
			ContainerID: asset.ContainerID,
			CollectedAt: model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(port).Error; err != nil {
			s.logger.Error("failed to create port",
				zap.String("host_id", hostID),
				zap.String("protocol", asset.Protocol),
				zap.Int("port", asset.Port),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed port data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleUserData 处理账户数据
func (s *AssetService) handleUserData(hostID, jsonData string) error {
	// 解析 JSON 数据（可能是数组）
	var assets []engine.UserAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		// 尝试解析单个对象
		var asset engine.UserAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal user data: %w", err)
		}
		assets = []engine.UserAsset{asset}
	}

	// 删除旧数据
	if err := s.db.Where("host_id = ?", hostID).Delete(&model.AssetUser{}).Error; err != nil {
		return fmt.Errorf("failed to delete old user data: %w", err)
	}

	// 转换为数据库模型并插入
	for _, asset := range assets {
		user := &model.AssetUser{
			ID:          fmt.Sprintf("%s-%s", hostID, asset.Username),
			HostID:      hostID,
			Username:    asset.Username,
			UID:         asset.UID,
			GID:         asset.GID,
			Groupname:   asset.Groupname,
			HomeDir:     asset.HomeDir,
			Shell:       asset.Shell,
			Comment:     asset.Comment,
			HasPassword: asset.HasPassword,
			CollectedAt: model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(user).Error; err != nil {
			s.logger.Error("failed to create user",
				zap.String("host_id", hostID),
				zap.String("username", asset.Username),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed user data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleSoftwareData 处理软件包数据
func (s *AssetService) handleSoftwareData(hostID, jsonData string) error {
	var assets []engine.SoftwareAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		var asset engine.SoftwareAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal software data: %w", err)
		}
		assets = []engine.SoftwareAsset{asset}
	}

	if err := s.db.Where("host_id = ?", hostID).Delete(&model.Software{}).Error; err != nil {
		return fmt.Errorf("failed to delete old software data: %w", err)
	}

	for _, asset := range assets {
		software := &model.Software{
			ID:           fmt.Sprintf("%s-%s-%s", hostID, asset.PackageType, asset.Name),
			HostID:       hostID,
			Name:         asset.Name,
			Version:      asset.Version,
			Architecture: asset.Architecture,
			PackageType:  asset.PackageType,
			Vendor:       asset.Vendor,
			InstallTime:  asset.InstallTime,
			CollectedAt:  model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(software).Error; err != nil {
			s.logger.Error("failed to create software",
				zap.String("host_id", hostID),
				zap.String("name", asset.Name),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed software data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleContainerData 处理容器数据
func (s *AssetService) handleContainerData(hostID, jsonData string) error {
	var assets []engine.ContainerAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		var asset engine.ContainerAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal container data: %w", err)
		}
		assets = []engine.ContainerAsset{asset}
	}

	if err := s.db.Where("host_id = ?", hostID).Delete(&model.Container{}).Error; err != nil {
		return fmt.Errorf("failed to delete old container data: %w", err)
	}

	for _, asset := range assets {
		container := &model.Container{
			ID:            fmt.Sprintf("%s-%s", hostID, asset.ContainerID),
			HostID:        hostID,
			ContainerID:   asset.ContainerID,
			ContainerName: asset.ContainerName,
			Image:         asset.Image,
			ImageID:       asset.ImageID,
			Runtime:       asset.Runtime,
			Status:        asset.Status,
			CreatedAt:     asset.CreatedAt,
			CollectedAt:   model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(container).Error; err != nil {
			s.logger.Error("failed to create container",
				zap.String("host_id", hostID),
				zap.String("container_id", asset.ContainerID),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed container data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleAppData 处理应用数据
func (s *AssetService) handleAppData(hostID, jsonData string) error {
	var assets []engine.AppAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		var asset engine.AppAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal app data: %w", err)
		}
		assets = []engine.AppAsset{asset}
	}

	if err := s.db.Where("host_id = ?", hostID).Delete(&model.App{}).Error; err != nil {
		return fmt.Errorf("failed to delete old app data: %w", err)
	}

	for _, asset := range assets {
		app := &model.App{
			ID:          fmt.Sprintf("%s-%s-%s", hostID, asset.AppType, asset.AppName),
			HostID:      hostID,
			AppType:     asset.AppType,
			AppName:     asset.AppName,
			Version:     asset.Version,
			Port:        asset.Port,
			ProcessID:   asset.ProcessID,
			ConfigPath:  asset.ConfigPath,
			DataPath:    asset.DataPath,
			CollectedAt: model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(app).Error; err != nil {
			s.logger.Error("failed to create app",
				zap.String("host_id", hostID),
				zap.String("app_type", asset.AppType),
				zap.String("app_name", asset.AppName),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed app data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleNetInterfaceData 处理网络接口数据
func (s *AssetService) handleNetInterfaceData(hostID, jsonData string) error {
	var assets []engine.NetInterfaceAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		var asset engine.NetInterfaceAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal network interface data: %w", err)
		}
		assets = []engine.NetInterfaceAsset{asset}
	}

	if err := s.db.Where("host_id = ?", hostID).Delete(&model.NetInterface{}).Error; err != nil {
		return fmt.Errorf("failed to delete old network interface data: %w", err)
	}

	for _, asset := range assets {
		netInterface := &model.NetInterface{
			ID:            fmt.Sprintf("%s-%s", hostID, asset.InterfaceName),
			HostID:        hostID,
			InterfaceName: asset.InterfaceName,
			MACAddress:    asset.MACAddress,
			IPv4Addresses: model.StringArray(asset.IPv4Addresses),
			IPv6Addresses: model.StringArray(asset.IPv6Addresses),
			MTU:           asset.MTU,
			State:         asset.State,
			CollectedAt:   model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(netInterface).Error; err != nil {
			s.logger.Error("failed to create network interface",
				zap.String("host_id", hostID),
				zap.String("interface_name", asset.InterfaceName),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed network interface data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleVolumeData 处理磁盘数据
func (s *AssetService) handleVolumeData(hostID, jsonData string) error {
	var assets []engine.VolumeAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		var asset engine.VolumeAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal volume data: %w", err)
		}
		assets = []engine.VolumeAsset{asset}
	}

	if err := s.db.Where("host_id = ?", hostID).Delete(&model.Volume{}).Error; err != nil {
		return fmt.Errorf("failed to delete old volume data: %w", err)
	}

	for _, asset := range assets {
		volume := &model.Volume{
			ID:            fmt.Sprintf("%s-%s", hostID, asset.MountPoint),
			HostID:        hostID,
			Device:        asset.Device,
			MountPoint:    asset.MountPoint,
			FileSystem:    asset.FileSystem,
			TotalSize:     asset.TotalSize,
			UsedSize:      asset.UsedSize,
			AvailableSize: asset.AvailableSize,
			UsagePercent:  asset.UsagePercent,
			CollectedAt:   model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(volume).Error; err != nil {
			s.logger.Error("failed to create volume",
				zap.String("host_id", hostID),
				zap.String("mount_point", asset.MountPoint),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed volume data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleKmodData 处理内核模块数据
func (s *AssetService) handleKmodData(hostID, jsonData string) error {
	var assets []engine.KmodAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		var asset engine.KmodAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal kernel module data: %w", err)
		}
		assets = []engine.KmodAsset{asset}
	}

	if err := s.db.Where("host_id = ?", hostID).Delete(&model.Kmod{}).Error; err != nil {
		return fmt.Errorf("failed to delete old kernel module data: %w", err)
	}

	for _, asset := range assets {
		kmod := &model.Kmod{
			ID:          fmt.Sprintf("%s-%s", hostID, asset.ModuleName),
			HostID:      hostID,
			ModuleName:  asset.ModuleName,
			Size:        asset.Size,
			UsedBy:      asset.UsedBy,
			State:       asset.State,
			CollectedAt: model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(kmod).Error; err != nil {
			s.logger.Error("failed to create kernel module",
				zap.String("host_id", hostID),
				zap.String("module_name", asset.ModuleName),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed kernel module data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleServiceData 处理系统服务数据
func (s *AssetService) handleServiceData(hostID, jsonData string) error {
	var assets []engine.ServiceAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		var asset engine.ServiceAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal service data: %w", err)
		}
		assets = []engine.ServiceAsset{asset}
	}

	if err := s.db.Where("host_id = ?", hostID).Delete(&model.Service{}).Error; err != nil {
		return fmt.Errorf("failed to delete old service data: %w", err)
	}

	for _, asset := range assets {
		service := &model.Service{
			ID:          fmt.Sprintf("%s-%s", hostID, asset.ServiceName),
			HostID:      hostID,
			ServiceName: asset.ServiceName,
			ServiceType: asset.ServiceType,
			Status:      asset.Status,
			Enabled:     asset.Enabled,
			Description: asset.Description,
			CollectedAt: model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(service).Error; err != nil {
			s.logger.Error("failed to create service",
				zap.String("host_id", hostID),
				zap.String("service_name", asset.ServiceName),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed service data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}

// handleCronData 处理定时任务数据
func (s *AssetService) handleCronData(hostID, jsonData string) error {
	var assets []engine.CronAsset
	if err := json.Unmarshal([]byte(jsonData), &assets); err != nil {
		var asset engine.CronAsset
		if err2 := json.Unmarshal([]byte(jsonData), &asset); err2 != nil {
			return fmt.Errorf("failed to unmarshal cron data: %w", err)
		}
		assets = []engine.CronAsset{asset}
	}

	if err := s.db.Where("host_id = ?", hostID).Delete(&model.Cron{}).Error; err != nil {
		return fmt.Errorf("failed to delete old cron data: %w", err)
	}

	for _, asset := range assets {
		cron := &model.Cron{
			ID:          fmt.Sprintf("%s-%s-%s", hostID, asset.User, asset.Schedule),
			HostID:      hostID,
			User:        asset.User,
			Schedule:    asset.Schedule,
			Command:     asset.Command,
			CronType:    asset.CronType,
			Enabled:     asset.Enabled,
			CollectedAt: model.ToLocalTime(asset.CollectedAt),
		}

		if err := s.db.Create(cron).Error; err != nil {
			s.logger.Error("failed to create cron",
				zap.String("host_id", hostID),
				zap.String("user", asset.User),
				zap.String("schedule", asset.Schedule),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("processed cron data",
		zap.String("host_id", hostID),
		zap.Int("count", len(assets)))

	return nil
}
