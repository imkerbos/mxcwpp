package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// TaskType 任务类型
type TaskType string

const (
	TaskTypeBaselineScan TaskType = "baseline_scan"
)

// TargetType 目标类型
type TargetType string

const (
	TargetTypeAll      TargetType = "all"
	TargetTypeHostIDs  TargetType = "host_ids"
	TargetTypeOSFamily TargetType = "os_family"
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// TargetConfig 目标配置（JSON 格式）
type TargetConfig struct {
	HostIDs  []string `json:"host_ids,omitempty"`
	OSFamily []string `json:"os_family,omitempty"`
}

// Value 实现 driver.Valuer 接口
func (t TargetConfig) Value() (driver.Value, error) {
	return json.Marshal(t)
}

// Scan 实现 sql.Scanner 接口
func (t *TargetConfig) Scan(value interface{}) error {
	if value == nil {
		*t = TargetConfig{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, t)
}

// ScanTask 扫描任务模型
type ScanTask struct {
	TaskID       string       `gorm:"primaryKey;column:task_id;type:varchar(64);not null" json:"task_id"`
	Name         string       `gorm:"column:name;type:varchar(255)" json:"name"`
	Type         TaskType     `gorm:"column:type;type:varchar(50);not null" json:"type"`
	TargetType   TargetType   `gorm:"column:target_type;type:varchar(20);not null" json:"target_type"`
	TargetConfig TargetConfig `gorm:"column:target_config;type:json" json:"target_config"`
	PolicyID     string       `gorm:"column:policy_id;type:varchar(64);index" json:"policy_id"`
	RuleIDs      StringArray  `gorm:"column:rule_ids;type:json" json:"rule_ids"`
	Status       TaskStatus   `gorm:"column:status;type:varchar(20);default:'pending'" json:"status"`
	CreatedAt    time.Time    `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time    `gorm:"column:updated_at;type:timestamp;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP" json:"updated_at"`
	ExecutedAt   *time.Time   `gorm:"column:executed_at;type:timestamp" json:"executed_at"`
	CompletedAt  *time.Time   `gorm:"column:completed_at;type:timestamp" json:"completed_at"`
}

// TableName 指定表名
func (ScanTask) TableName() string {
	return "scan_tasks"
}
