package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// ReportType 报告类型
type ReportType string

const (
	ReportTypeBaseline      ReportType = "baseline"
	ReportTypeAntivirus     ReportType = "antivirus"
	ReportTypeVulnerability ReportType = "vulnerability"
	ReportTypeKube          ReportType = "kube"
	ReportTypeRuntime       ReportType = "runtime"
	ReportTypeRemediation   ReportType = "remediation"
)

// ReportJSON 用于存储报告数据的 JSON 类型
type ReportJSON map[string]any

// Value 实现 driver.Valuer 接口
func (r ReportJSON) Value() (driver.Value, error) {
	if r == nil {
		return "null", nil
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("序列化报告数据失败: %w", err)
	}
	return string(data), nil
}

// Scan 实现 sql.Scanner 接口
func (r *ReportJSON) Scan(value any) error {
	if value == nil {
		*r = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return fmt.Errorf("无法扫描报告数据: %T", value)
	}
	return json.Unmarshal(bytes, r)
}

// GeneratedReport 已生成的报告记录
type GeneratedReport struct {
	ID         uint       `gorm:"primaryKey;column:id;autoIncrement" json:"id"`
	ReportType ReportType `gorm:"column:report_type;type:varchar(20);not null;index:idx_report_type" json:"report_type"`
	Title      string     `gorm:"column:title;type:varchar(200);not null" json:"title"`
	ReportID   string     `gorm:"column:report_id;type:varchar(50);not null" json:"report_id"`
	Period     string     `gorm:"column:period;type:varchar(100)" json:"period"`
	ReportData ReportJSON `gorm:"column:report_data;type:longtext;not null" json:"report_data"`
	CreatedAt  LocalTime  `gorm:"column:created_at;type:timestamp;default:CURRENT_TIMESTAMP;index:idx_created_at" json:"created_at"`
}

// TableName 指定表名
func (GeneratedReport) TableName() string {
	return "generated_reports"
}
