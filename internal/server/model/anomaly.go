package model

// AnomalyAlert represents an ML-detected behavioral anomaly.
type AnomalyAlert struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	HostID       string    `gorm:"type:varchar(64);index" json:"host_id"`
	Hostname     string    `gorm:"type:varchar(255)" json:"hostname"`
	AlertType    string    `gorm:"type:varchar(50);index" json:"alert_type"`        // isolation_forest / correlation
	PatternName  string    `gorm:"type:varchar(100)" json:"pattern_name"`           // correlation pattern name (c2_beacon, etc)
	Severity     string    `gorm:"type:varchar(20);default:medium" json:"severity"` // critical/high/medium/low
	AnomalyScore float64   `gorm:"type:double;default:0" json:"anomaly_score"`      // 0.0-1.0
	TopMetric    string    `gorm:"type:varchar(100)" json:"top_metric"`             // most anomalous metric name
	TopValue     float64   `gorm:"type:double;default:0" json:"top_value"`          // observed value of top metric
	Description  string    `gorm:"type:text" json:"description"`
	Status       string    `gorm:"type:varchar(20);default:open" json:"status"` // open/confirmed/false_positive
	ResolvedBy   string    `gorm:"type:varchar(100)" json:"resolved_by,omitempty"`
	CreatedAt    LocalTime `json:"created_at"`
	UpdatedAt    LocalTime `json:"updated_at"`
}

func (AnomalyAlert) TableName() string { return "anomaly_alerts" }
