package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// AffectedResource 受影响的 K8s 资源
type AffectedResource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// AffectedResources 受影响资源列表（JSON 数组）
type AffectedResources []AffectedResource

// Value 实现 driver.Valuer 接口
func (a AffectedResources) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	data, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

// Scan 实现 sql.Scanner 接口
func (a *AffectedResources) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("无法扫描类型 %T 到 AffectedResources", value)
	}
	return json.Unmarshal(bytes, a)
}

// KubeBaseline CIS 基线检查结果
type KubeBaseline struct {
	ID                uint              `gorm:"primaryKey;column:id;autoIncrement" json:"id"`
	ClusterID         uint              `gorm:"column:cluster_id;not null;index" json:"clusterId"`
	ClusterName       string            `gorm:"column:cluster_name;type:varchar(255)" json:"clusterName"`
	Category          string            `gorm:"column:category;type:varchar(50);not null;index" json:"category"`
	CheckID           string            `gorm:"column:check_id;type:varchar(50);not null" json:"checkId"`
	CheckName         string            `gorm:"column:check_name;type:varchar(255)" json:"checkName"`
	Title             string            `gorm:"column:title;type:varchar(255)" json:"title"`
	Description       string            `gorm:"column:description;type:text" json:"description"`
	Severity          string            `gorm:"column:severity;type:varchar(20);not null;index" json:"severity"`
	Result            string            `gorm:"column:result;type:varchar(20);not null;index" json:"result"`
	Remediation       string            `gorm:"column:remediation;type:text" json:"remediation"`
	Benchmark         string            `gorm:"column:benchmark;type:varchar(255)" json:"benchmark"`
	AffectedResources AffectedResources `gorm:"column:affected_resources;type:json" json:"affectedResources"`
	CheckedAt         LocalTime         `gorm:"column:checked_at;type:timestamp;not null;index" json:"checkedAt"`
	CreatedAt         LocalTime         `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP" json:"createdAt"`
}

func (KubeBaseline) TableName() string {
	return "kube_baselines"
}
