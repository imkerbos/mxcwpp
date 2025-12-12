package model

import (
	"time"
)

// Policy 策略集模型
type Policy struct {
	ID          string      `gorm:"primaryKey;column:id;type:varchar(64);not null" json:"id"`
	Name        string      `gorm:"column:name;type:varchar(255);not null" json:"name"`
	Version     string      `gorm:"column:version;type:varchar(50)" json:"version"`
	Description string      `gorm:"column:description;type:text" json:"description"`
	OSFamily    StringArray `gorm:"column:os_family;type:json" json:"os_family"`
	OSVersion   string      `gorm:"column:os_version;type:varchar(50)" json:"os_version"`
	Enabled     bool        `gorm:"column:enabled;type:boolean;default:true" json:"enabled"`
	GroupID     string      `gorm:"column:group_id;type:varchar(64);index" json:"group_id"` // 所属策略组ID
	CreatedAt   time.Time   `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time   `gorm:"column:updated_at;type:timestamp;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联关系
	Rules []Rule `gorm:"foreignKey:PolicyID;references:ID" json:"rules,omitempty"`
}

// TableName 指定表名
func (Policy) TableName() string {
	return "policies"
}
