package model

import (
	"database/sql/driver"
	"encoding/json"
)

// ChangeDetail FIM 变更详情
type ChangeDetail struct {
	SizeBefore        string `json:"size_before,omitempty"`
	SizeAfter         string `json:"size_after,omitempty"`
	HashChanged       bool   `json:"hash_changed"`
	PermissionChanged bool   `json:"permission_changed"`
	OwnerChanged      bool   `json:"owner_changed"`
	Attributes        string `json:"attributes,omitempty"`
}

// Value 实现 driver.Valuer 接口
func (c ChangeDetail) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan 实现 sql.Scanner 接口
func (c *ChangeDetail) Scan(value interface{}) error {
	if value == nil {
		*c = ChangeDetail{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

// FIMEvent FIM 变更事件模型
type FIMEvent struct {
	EventID      string       `gorm:"primaryKey;column:event_id;type:varchar(64);not null" json:"event_id"`
	HostID       string       `gorm:"column:host_id;type:varchar(64);not null;index:idx_fim_event_host_id" json:"host_id"`
	Hostname     string       `gorm:"column:hostname;type:varchar(255)" json:"hostname"`
	TaskID       string       `gorm:"column:task_id;type:varchar(64)" json:"task_id"`
	FilePath     string       `gorm:"column:file_path;type:varchar(1024);not null;index:idx_fim_event_file_path,length:255" json:"file_path"`
	ChangeType   string       `gorm:"column:change_type;type:varchar(20);not null" json:"change_type"` // added/removed/changed
	ChangeDetail ChangeDetail `gorm:"column:change_detail;type:json" json:"change_detail"`
	Severity     string       `gorm:"column:severity;type:varchar(20);default:'medium';index:idx_fim_event_severity" json:"severity"`
	Category     string       `gorm:"column:category;type:varchar(50)" json:"category"` // binary/config/auth/log/other
	DetectedAt   LocalTime    `gorm:"column:detected_at;type:timestamp;not null;index:idx_fim_event_detected_at" json:"detected_at"`
	CreatedAt    LocalTime    `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"created_at"`
}

// TableName 指定表名
func (FIMEvent) TableName() string {
	return "fim_events"
}
