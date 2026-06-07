// Package outbound 实现告警/事件外发到 SIEM/SOC 系统。
//
// types.go 定义 Event/Connector 接口 (P2-3 PR100 起步; 本 PR 含同等定义保证 standalone build).
package outbound

import (
	"context"
	"time"
)

// Event 是外发的通用告警 event 结构 (与 PR100 connector.go 等价).
type Event struct {
	ID          string                 `json:"id"`
	TenantID    string                 `json:"tenant_id"`
	HostID      string                 `json:"host_id"`
	HostName    string                 `json:"host_name,omitempty"`
	Severity    string                 `json:"severity"`
	Category    string                 `json:"category"`
	RuleID      string                 `json:"rule_id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	MitreID     string                 `json:"mitre_id,omitempty"`
	Source      string                 `json:"source"`
	Timestamp   time.Time              `json:"timestamp"`
	Fields      map[string]any         `json:"fields,omitempty"`
}

// Connector 接口.
type Connector interface {
	Name() string
	Send(ctx context.Context, ev *Event) error
	Close() error
}
