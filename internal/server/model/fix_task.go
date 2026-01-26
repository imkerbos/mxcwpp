package model

// FixTaskStatus 修复任务状态
type FixTaskStatus string

const (
	FixTaskStatusPending   FixTaskStatus = "pending"   // 待执行
	FixTaskStatusRunning   FixTaskStatus = "running"   // 执行中
	FixTaskStatusCompleted FixTaskStatus = "completed" // 已完成
	FixTaskStatusFailed    FixTaskStatus = "failed"    // 失败
)

// FixResultStatus 修复结果状态
type FixResultStatus string

const (
	FixResultStatusSuccess FixResultStatus = "success" // 成功
	FixResultStatusFailed  FixResultStatus = "failed"  // 失败
	FixResultStatusSkipped FixResultStatus = "skipped" // 跳过
)

// FixTask 修复任务模型
type FixTask struct {
	TaskID       string        `gorm:"primaryKey;column:task_id;type:varchar(64);not null" json:"task_id"`
	HostIDs      StringArray   `gorm:"column:host_ids;type:json;not null" json:"host_ids"`
	RuleIDs      StringArray   `gorm:"column:rule_ids;type:json;not null" json:"rule_ids"`
	Severities   StringArray   `gorm:"column:severities;type:json" json:"severities"`
	Status       FixTaskStatus `gorm:"column:status;type:varchar(20);default:'pending'" json:"status"`
	TotalCount   int           `gorm:"column:total_count;type:int;default:0" json:"total_count"`
	SuccessCount int           `gorm:"column:success_count;type:int;default:0" json:"success_count"`
	FailedCount  int           `gorm:"column:failed_count;type:int;default:0" json:"failed_count"`
	Progress     int           `gorm:"column:progress;type:int;default:0" json:"progress"` // 进度百分比 0-100
	CreatedBy    string        `gorm:"column:created_by;type:varchar(64)" json:"created_by"`
	CreatedAt    LocalTime     `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"created_at"`
	CompletedAt  *LocalTime    `gorm:"column:completed_at;type:timestamp" json:"completed_at"`
}

// TableName 指定表名
func (FixTask) TableName() string {
	return "fix_tasks"
}

// FixResult 修复结果模型
type FixResult struct {
	ResultID  string          `gorm:"primaryKey;column:result_id;type:varchar(64);not null" json:"result_id"`
	TaskID    string          `gorm:"column:task_id;type:varchar(64);not null;index" json:"task_id"`
	HostID    string          `gorm:"column:host_id;type:varchar(64);not null;index" json:"host_id"`
	RuleID    string          `gorm:"column:rule_id;type:varchar(64);not null" json:"rule_id"`
	Status    FixResultStatus `gorm:"column:status;type:varchar(20);not null" json:"status"`
	Command   string          `gorm:"column:command;type:text" json:"command"`
	Output    string          `gorm:"column:output;type:text" json:"output"`
	ErrorMsg  string          `gorm:"column:error_msg;type:text" json:"error_msg"`
	Message   string          `gorm:"column:message;type:varchar(500)" json:"message"`
	FixedAt   LocalTime       `gorm:"column:fixed_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"fixed_at"`
}

// TableName 指定表名
func (FixResult) TableName() string {
	return "fix_results"
}
