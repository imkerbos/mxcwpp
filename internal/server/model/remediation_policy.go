package model

// RemediationPolicy 修复策略模板
type RemediationPolicy struct {
	ID          uint       `gorm:"primaryKey;column:id;autoIncrement" json:"id"`
	Name        string     `gorm:"column:name;type:varchar(100);not null" json:"name"`
	Description string     `gorm:"column:description;type:text" json:"description"`
	TargetType  string     `gorm:"column:target_type;type:varchar(20)" json:"targetType"`   // all / business_line / tag / host_ids
	TargetValue string     `gorm:"column:target_value;type:text" json:"targetValue"`        // JSON: 业务线ID / 标签名 / 主机ID列表
	SeverityMin string     `gorm:"column:severity_min;type:varchar(20)" json:"severityMin"` // 最低严重级别
	PriorityMin float64    `gorm:"column:priority_min;type:decimal(5,3);default:0" json:"priorityMin"`
	AutoConfirm bool       `gorm:"column:auto_confirm;default:false" json:"autoConfirm"`
	MaxParallel int        `gorm:"column:max_parallel;default:10" json:"maxParallel"`
	RolloutType string     `gorm:"column:rollout_type;type:varchar(20);default:'immediate'" json:"rolloutType"` // immediate / canary / rolling
	CanaryRatio int        `gorm:"column:canary_ratio;default:10" json:"canaryRatio"`                           // 金丝雀比例（%）
	Enabled     bool       `gorm:"column:enabled;default:true" json:"enabled"`
	LastRunAt   *LocalTime `gorm:"column:last_run_at;type:timestamp" json:"lastRunAt"`
	CreatedBy   string     `gorm:"column:created_by;type:varchar(64)" json:"createdBy"`
	CreatedAt   LocalTime  `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"createdAt"`
	UpdatedAt   LocalTime  `gorm:"column:updated_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"updatedAt"`
}

// TableName 指定表名
func (RemediationPolicy) TableName() string {
	return "remediation_policies"
}
