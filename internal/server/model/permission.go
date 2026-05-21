// Package model 提供数据库模型定义
package model

// PermissionCode 权限码
type PermissionCode string

// 权限码常量 — 每个值对应一个可配置的功能模块访问权限
const (
	PermDashboard    PermissionCode = "dashboard"     // 安全概览
	PermAssets       PermissionCode = "assets"        // 资产中心
	PermAlerts       PermissionCode = "alerts"        // 告警中心
	PermBaseline     PermissionCode = "baseline"      // 基线安全
	PermFIM          PermissionCode = "fim"           // 文件完整性
	PermVirus        PermissionCode = "virus"         // 病毒查杀
	PermVuln         PermissionCode = "vuln"          // 漏洞管理
	PermKube         PermissionCode = "kube"          // 容器集群
	PermDetection    PermissionCode = "detection"     // 威胁检测
	PermMonitoring   PermissionCode = "monitoring"    // 系统监控
	PermOperations   PermissionCode = "operations"    // 运维中心
	PermAuditLog     PermissionCode = "audit_log"     // 审计日志
	PermUserManage   PermissionCode = "user_manage"   // 用户管理
	PermSystemConfig PermissionCode = "system_config" // 系统设置
)

// AllPermissionCodes 返回所有权限码（admin 角色默认拥有全部）
var AllPermissionCodes = []PermissionCode{
	PermDashboard, PermAssets, PermAlerts, PermBaseline,
	PermFIM, PermVirus, PermVuln, PermKube,
	PermDetection, PermMonitoring, PermOperations, PermAuditLog,
	PermUserManage, PermSystemConfig,
}

// Permission 权限定义
type Permission struct {
	ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Code        PermissionCode `gorm:"column:code;type:varchar(50);uniqueIndex;not null" json:"code"`
	Name        string         `gorm:"column:name;type:varchar(100);not null" json:"name"`
	Module      string         `gorm:"column:module;type:varchar(50)" json:"module"`
	Description string         `gorm:"column:description;type:varchar(500)" json:"description"`
}

func (Permission) TableName() string { return "permissions" }

// RolePermission 角色-权限关联
type RolePermission struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	RoleCode string `gorm:"column:role_code;type:varchar(20);not null;index:idx_role_perm,unique" json:"role_code"`
	PermCode string `gorm:"column:perm_code;type:varchar(50);not null;index:idx_role_perm,unique" json:"perm_code"`
}

func (RolePermission) TableName() string { return "role_permissions" }
