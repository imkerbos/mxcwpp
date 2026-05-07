// Package engine 提供 FIM 插件的核心引擎
package engine

// FIMPolicy 从任务 JSON 解析的策略配置
type FIMPolicy struct {
	WatchPaths   []WatchPath `json:"watch_paths"`
	ExcludePaths []string    `json:"exclude_paths"`
}

// WatchPath 监控路径配置
type WatchPath struct {
	Path    string `json:"path"`
	Level   string `json:"level"`   // NORMAL, CONTENT, PERMS
	Comment string `json:"comment"`
}

// FIMEvent 单个文件变更事件
type FIMEvent struct {
	EventID      string       `json:"event_id"`
	FilePath     string       `json:"file_path"`
	ChangeType   string       `json:"change_type"` // added, removed, changed
	Severity     string       `json:"severity"`     // critical, high, medium, low
	Category     string       `json:"category"`     // binary, auth, ssh, config, other
	ChangeDetail ChangeDetail `json:"change_detail"`
}

// ChangeDetail 变更详情
type ChangeDetail struct {
	SizeBefore        string `json:"size_before,omitempty"`
	SizeAfter         string `json:"size_after,omitempty"`
	Attributes        string `json:"attributes,omitempty"`
	HashChanged       bool   `json:"hash_changed"`
	PermissionChanged bool   `json:"permission_changed"`
	OwnerChanged      bool   `json:"owner_changed"`
}

// AIDESummary AIDE 检查摘要
type AIDESummary struct {
	TotalEntries   int `json:"total_entries"`
	AddedEntries   int `json:"added_entries"`
	RemovedEntries int `json:"removed_entries"`
	ChangedEntries int `json:"changed_entries"`
}

// AIDEReport AIDE 解析后的完整报告
type AIDEReport struct {
	Summary AIDESummary `json:"summary"`
	Events  []FIMEvent  `json:"events"`
}

// ExecuteResult 引擎执行结果
type ExecuteResult struct {
	Summary AIDESummary `json:"summary"`
	Events  []FIMEvent  `json:"events"`
	Error   string      `json:"error,omitempty"`
}
