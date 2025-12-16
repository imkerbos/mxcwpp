package model

import (
	"database/sql/driver"
	"encoding/json"
)

// CheckConfig 检查配置（JSON 格式）
type CheckConfig struct {
	Condition string      `json:"condition"` // all/any/none
	Rules     []CheckRule `json:"rules"`
}

// CheckRule 单个检查规则
type CheckRule struct {
	Type   string   `json:"type"`
	Param  []string `json:"param"`
	Result string   `json:"result,omitempty"`
}

// FixConfig 修复配置（JSON 格式）
type FixConfig struct {
	Suggestion string `json:"suggestion"`
	Command    string `json:"command,omitempty"`
}

// Value 实现 driver.Valuer 接口
func (c CheckConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan 实现 sql.Scanner 接口
func (c *CheckConfig) Scan(value interface{}) error {
	if value == nil {
		*c = CheckConfig{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

// Value 实现 driver.Valuer 接口
func (f FixConfig) Value() (driver.Value, error) {
	return json.Marshal(f)
}

// Scan 实现 sql.Scanner 接口
func (f *FixConfig) Scan(value interface{}) error {
	if value == nil {
		*f = FixConfig{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, f)
}

// Rule 规则模型
type Rule struct {
	RuleID      string      `gorm:"primaryKey;column:rule_id;type:varchar(64);not null" json:"rule_id"`
	PolicyID    string      `gorm:"column:policy_id;type:varchar(64);not null;index" json:"policy_id"`
	Category    string      `gorm:"column:category;type:varchar(50)" json:"category"`
	Title       string      `gorm:"column:title;type:varchar(255)" json:"title"`
	Description string      `gorm:"column:description;type:text" json:"description"`
	Severity    string      `gorm:"column:severity;type:varchar(20)" json:"severity"`
	Enabled     bool        `gorm:"column:enabled;type:boolean;default:true" json:"enabled"`
	TargetType  string      `gorm:"column:target_type;type:varchar(20);default:'all'" json:"target_type"` // host/container/all
	CheckConfig CheckConfig `gorm:"column:check_config;type:json" json:"check_config"`
	FixConfig   FixConfig   `gorm:"column:fix_config;type:json" json:"fix_config"`
	CreatedAt   LocalTime   `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   LocalTime   `gorm:"column:updated_at;type:timestamp;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联关系
	Policy Policy `gorm:"foreignKey:PolicyID;references:ID" json:"policy,omitempty"`
}

// TableName 指定表名
func (Rule) TableName() string {
	return "rules"
}
