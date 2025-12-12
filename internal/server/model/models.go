// Package model 提供数据库模型定义
// 本文件导出所有模型，方便统一导入
package model

// 导出所有模型类型
var (
	// 所有模型列表，用于数据库迁移
	AllModels = []interface{}{
		&Host{},
		&PolicyGroup{},
		&Policy{},
		&Rule{},
		&ScanResult{},
		&ScanTask{},
		&User{},
		&Process{},
		&Port{},
		&AssetUser{},
		&Software{},
		&Container{},
		&App{},
		&NetInterface{},
		&Volume{},
		&Kmod{},
		&Service{},
		&Cron{},
		&HostMetric{},
		&HostMetricHourly{},
		&BusinessLine{},
		&SystemConfig{},
		&Notification{},
		&Alert{},
	}
)
