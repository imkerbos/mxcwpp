package model

import (
	"database/sql/driver"
	"encoding/json"
)

// OSRequirement OS 版本要求（详细）
type OSRequirement struct {
	OSFamily   string `json:"os_family"`             // rocky, centos, debian 等
	MinVersion string `json:"min_version,omitempty"` // 最小版本（含）
	MaxVersion string `json:"max_version,omitempty"` // 最大版本（含）
}

// OSRequirements OS 版本要求列表
type OSRequirements []OSRequirement

// Value 实现 driver.Valuer 接口
func (o OSRequirements) Value() (driver.Value, error) {
	if o == nil {
		return "[]", nil
	}
	return json.Marshal(o)
}

// Scan 实现 sql.Scanner 接口
func (o *OSRequirements) Scan(value interface{}) error {
	if value == nil {
		*o = OSRequirements{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, o)
}

// Policy 策略集模型
type Policy struct {
	ID             string         `gorm:"primaryKey;column:id;type:varchar(64);not null" json:"id"`
	Name           string         `gorm:"column:name;type:varchar(255);not null" json:"name"`
	Version        string         `gorm:"column:version;type:varchar(50)" json:"version"`
	Description    string         `gorm:"column:description;type:text" json:"description"`
	OSFamily       StringArray    `gorm:"column:os_family;type:json" json:"os_family"`                     // 简单 OS 列表（向后兼容）
	OSVersion      string         `gorm:"column:os_version;type:varchar(50)" json:"os_version"`            // 简单版本要求（向后兼容）
	OSRequirements OSRequirements `gorm:"column:os_requirements;type:json" json:"os_requirements"`         // 详细 OS 版本要求
	TargetType     string         `gorm:"column:target_type;type:varchar(20);default:'all'" json:"target_type"` // host/container/all
	Enabled        bool           `gorm:"column:enabled;type:boolean;default:true" json:"enabled"`
	GroupID        string         `gorm:"column:group_id;type:varchar(64);index" json:"group_id"` // 所属策略组ID
	CreatedAt      LocalTime      `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt      LocalTime      `gorm:"column:updated_at;type:timestamp;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联关系
	Rules []Rule `gorm:"foreignKey:PolicyID;references:ID" json:"rules,omitempty"`
}

// TableName 指定表名
func (Policy) TableName() string {
	return "policies"
}
