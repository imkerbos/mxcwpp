// Package biz 提供业务逻辑层
package biz

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
	"github.com/imkerbos/mxsec-platform/internal/server/prometheus"
)

// MetricsService 是监控数据查询服务
type MetricsService struct {
	db               *gorm.DB
	prometheusClient *prometheus.Client
	logger           *zap.Logger
}

// NewMetricsService 创建监控数据查询服务
func NewMetricsService(db *gorm.DB, prometheusClient *prometheus.Client, logger *zap.Logger) *MetricsService {
	return &MetricsService{
		db:               db,
		prometheusClient: prometheusClient,
		logger:           logger,
	}
}

// HostMetrics 是主机监控数据
type HostMetrics struct {
	HostID     string             `json:"host_id"`
	Latest     *LatestMetrics     `json:"latest,omitempty"`      // 最新监控数据
	TimeSeries *TimeSeriesMetrics `json:"time_series,omitempty"` // 时间序列数据
	Source     string             `json:"source"`                // 数据源：mysql 或 prometheus
}

// LatestMetrics 是最新监控数据
type LatestMetrics struct {
	CPUUsage     *float64   `json:"cpu_usage,omitempty"`
	MemUsage     *float64   `json:"mem_usage,omitempty"`
	DiskUsage    *float64   `json:"disk_usage,omitempty"`
	NetBytesSent *uint64    `json:"net_bytes_sent,omitempty"`
	NetBytesRecv *uint64    `json:"net_bytes_recv,omitempty"`
	CollectedAt  *time.Time `json:"collected_at,omitempty"`
}

// TimeSeriesMetrics 是时间序列监控数据
type TimeSeriesMetrics struct {
	CPUUsage  []TimeSeriesPoint `json:"cpu_usage,omitempty"`
	MemUsage  []TimeSeriesPoint `json:"mem_usage,omitempty"`
	DiskUsage []TimeSeriesPoint `json:"disk_usage,omitempty"`
}

// TimeSeriesPoint 是时间序列数据点
type TimeSeriesPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// GetHostMetrics 获取主机监控数据
// 根据配置自动选择 MySQL 或 Prometheus 查询
func (s *MetricsService) GetHostMetrics(ctx context.Context, hostID string, startTime, endTime *time.Time) (*HostMetrics, error) {
	// 检查是否配置了 Prometheus
	if s.prometheusClient != nil {
		return s.getHostMetricsFromPrometheus(ctx, hostID, startTime, endTime)
	}

	// 否则使用 MySQL 查询
	return s.getHostMetricsFromMySQL(ctx, hostID, startTime, endTime)
}

// getHostMetricsFromMySQL 从 MySQL 获取主机监控数据
func (s *MetricsService) getHostMetricsFromMySQL(ctx context.Context, hostID string, startTime, endTime *time.Time) (*HostMetrics, error) {
	metrics := &HostMetrics{
		HostID: hostID,
		Source: "mysql",
	}

	// 查询最新监控数据
	var latestMetric model.HostMetric
	if err := s.db.Where("host_id = ?", hostID).
		Order("collected_at DESC").
		Limit(1).
		First(&latestMetric).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("查询最新监控数据失败: %w", err)
		}
		// 没有数据，返回空结果
		return metrics, nil
	}

	collectedAtTime := time.Time(latestMetric.CollectedAt)
	metrics.Latest = &LatestMetrics{
		CPUUsage:     latestMetric.CPUUsage,
		MemUsage:     latestMetric.MemUsage,
		DiskUsage:    latestMetric.DiskUsage,
		NetBytesSent: latestMetric.NetBytesSent,
		NetBytesRecv: latestMetric.NetBytesRecv,
		CollectedAt:  &collectedAtTime,
	}

	// 如果指定了时间范围，查询时间序列数据
	if startTime != nil && endTime != nil {
		timeSeries, err := s.getTimeSeriesFromMySQL(ctx, hostID, *startTime, *endTime)
		if err != nil {
			s.logger.Warn("查询时间序列数据失败", zap.Error(err))
		} else {
			metrics.TimeSeries = timeSeries
		}
	}

	return metrics, nil
}

// getHostMetricsFromPrometheus 从 Prometheus 获取主机监控数据
func (s *MetricsService) getHostMetricsFromPrometheus(ctx context.Context, hostID string, startTime, endTime *time.Time) (*HostMetrics, error) {
	metrics := &HostMetrics{
		HostID: hostID,
		Source: "prometheus",
	}

	labels := map[string]string{
		"host_id": hostID,
	}

	// 查询最新监控数据（即时查询）
	latest, err := s.getLatestMetricsFromPrometheus(ctx, labels)
	if err != nil {
		s.logger.Warn("从 Prometheus 查询最新监控数据失败", zap.Error(err))
	} else {
		metrics.Latest = latest
	}

	// 如果指定了时间范围，查询时间序列数据
	if startTime != nil && endTime != nil {
		timeSeries, err := s.getTimeSeriesFromPrometheus(ctx, labels, *startTime, *endTime)
		if err != nil {
			s.logger.Warn("从 Prometheus 查询时间序列数据失败", zap.Error(err))
		} else {
			metrics.TimeSeries = timeSeries
		}
	}

	return metrics, nil
}

// getLatestMetricsFromPrometheus 从 Prometheus 获取最新监控数据
func (s *MetricsService) getLatestMetricsFromPrometheus(ctx context.Context, labels map[string]string) (*LatestMetrics, error) {
	latest := &LatestMetrics{}

	// 查询 CPU 使用率
	if cpuValue, err := s.prometheusClient.GetMetricValue(ctx, "mxsec_host_cpu_usage", labels); err == nil && cpuValue != nil {
		latest.CPUUsage = cpuValue
	}

	// 查询内存使用率
	if memValue, err := s.prometheusClient.GetMetricValue(ctx, "mxsec_host_mem_usage", labels); err == nil && memValue != nil {
		latest.MemUsage = memValue
	}

	// 查询磁盘使用率
	if diskValue, err := s.prometheusClient.GetMetricValue(ctx, "mxsec_host_disk_usage", labels); err == nil && diskValue != nil {
		latest.DiskUsage = diskValue
	}

	// 设置采集时间（使用当前时间）
	now := time.Now()
	latest.CollectedAt = &now

	return latest, nil
}

// getTimeSeriesFromPrometheus 从 Prometheus 获取时间序列数据
func (s *MetricsService) getTimeSeriesFromPrometheus(ctx context.Context, labels map[string]string, start, end time.Time) (*TimeSeriesMetrics, error) {
	timeSeries := &TimeSeriesMetrics{}

	// 计算步长（根据时间范围自动调整）
	duration := end.Sub(start)
	var step string
	if duration <= 1*time.Hour {
		step = "1m" // 1 分钟
	} else if duration <= 24*time.Hour {
		step = "5m" // 5 分钟
	} else if duration <= 7*24*time.Hour {
		step = "15m" // 15 分钟
	} else {
		step = "1h" // 1 小时
	}

	// 查询 CPU 使用率时间序列
	if cpuPoints, err := s.prometheusClient.GetMetricRange(ctx, "mxsec_host_cpu_usage", labels, start, end, step); err == nil {
		timeSeries.CPUUsage = convertToTimeSeriesPoints(cpuPoints)
	}

	// 查询内存使用率时间序列
	if memPoints, err := s.prometheusClient.GetMetricRange(ctx, "mxsec_host_mem_usage", labels, start, end, step); err == nil {
		timeSeries.MemUsage = convertToTimeSeriesPoints(memPoints)
	}

	// 查询磁盘使用率时间序列
	if diskPoints, err := s.prometheusClient.GetMetricRange(ctx, "mxsec_host_disk_usage", labels, start, end, step); err == nil {
		timeSeries.DiskUsage = convertToTimeSeriesPoints(diskPoints)
	}

	return timeSeries, nil
}

// getTimeSeriesFromMySQL 从 MySQL 获取时间序列数据
func (s *MetricsService) getTimeSeriesFromMySQL(ctx context.Context, hostID string, start, end time.Time) (*TimeSeriesMetrics, error) {
	timeSeries := &TimeSeriesMetrics{}

	// 查询 CPU 使用率时间序列
	var cpuMetrics []model.HostMetric
	if err := s.db.Where("host_id = ? AND collected_at >= ? AND collected_at <= ?", hostID, start, end).
		Select("collected_at, cpu_usage").
		Order("collected_at ASC").
		Find(&cpuMetrics).Error; err == nil {
		timeSeries.CPUUsage = make([]TimeSeriesPoint, 0, len(cpuMetrics))
		for _, m := range cpuMetrics {
			if m.CPUUsage != nil {
				timeSeries.CPUUsage = append(timeSeries.CPUUsage, TimeSeriesPoint{
					Timestamp: time.Time(m.CollectedAt),
					Value:     *m.CPUUsage,
				})
			}
		}
	}

	// 查询内存使用率时间序列
	var memMetrics []model.HostMetric
	if err := s.db.Where("host_id = ? AND collected_at >= ? AND collected_at <= ?", hostID, start, end).
		Select("collected_at, mem_usage").
		Order("collected_at ASC").
		Find(&memMetrics).Error; err == nil {
		timeSeries.MemUsage = make([]TimeSeriesPoint, 0, len(memMetrics))
		for _, m := range memMetrics {
			if m.MemUsage != nil {
				timeSeries.MemUsage = append(timeSeries.MemUsage, TimeSeriesPoint{
					Timestamp: time.Time(m.CollectedAt),
					Value:     *m.MemUsage,
				})
			}
		}
	}

	// 查询磁盘使用率时间序列
	var diskMetrics []model.HostMetric
	if err := s.db.Where("host_id = ? AND collected_at >= ? AND collected_at <= ?", hostID, start, end).
		Select("collected_at, disk_usage").
		Order("collected_at ASC").
		Find(&diskMetrics).Error; err == nil {
		timeSeries.DiskUsage = make([]TimeSeriesPoint, 0, len(diskMetrics))
		for _, m := range diskMetrics {
			if m.DiskUsage != nil {
				timeSeries.DiskUsage = append(timeSeries.DiskUsage, TimeSeriesPoint{
					Timestamp: time.Time(m.CollectedAt),
					Value:     *m.DiskUsage,
				})
			}
		}
	}

	return timeSeries, nil
}

// convertToTimeSeriesPoints 转换 Prometheus 时间序列点为内部格式
func convertToTimeSeriesPoints(points []prometheus.TimeSeriesPoint) []TimeSeriesPoint {
	result := make([]TimeSeriesPoint, 0, len(points))
	for _, p := range points {
		result = append(result, TimeSeriesPoint{
			Timestamp: p.Timestamp,
			Value:     p.Value,
		})
	}
	return result
}
