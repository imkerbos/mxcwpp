package anomaly

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// MetricNames maps metric indices to names (matching BDE profiler output).
var MetricNames = [featureCount]string{
	"proc_exec_count", "proc_unique_exe", "proc_fork_rate",
	"file_write_count", "file_unique_path", "file_sensitive_hits",
	"net_connect_count", "net_unique_ip", "net_unique_port", "net_external_ratio",
	"dns_query_count", "dns_unique_domain", "dns_nx_ratio",
}

// Correlation patterns: multi-metric signatures that indicate specific attack types.
var correlationPatterns = []correlationPattern{
	{
		Name:        "c2_beacon",
		Description: "Possible C2 beaconing: high network + DNS activity with process execution",
		Indices:     []int{0, 6, 7, 10, 11}, // proc_exec, net_connect, net_unique_ip, dns_query, dns_unique_domain
		MinActive:   3,                      // at least 3 of 5 metrics elevated
		Severity:    "critical",
	},
	{
		Name:        "data_exfiltration",
		Description: "Possible data exfiltration: file access + external network",
		Indices:     []int{3, 4, 6, 9}, // file_write, file_unique_path, net_connect, net_external_ratio
		MinActive:   3,
		Severity:    "high",
	},
	{
		Name:        "privilege_escalation",
		Description: "Possible privilege escalation: sensitive file access + process forking",
		Indices:     []int{0, 2, 5}, // proc_exec, proc_fork_rate, file_sensitive_hits
		MinActive:   2,
		Severity:    "high",
	},
	{
		Name:        "reconnaissance",
		Description: "Possible reconnaissance: port scanning + DNS enumeration",
		Indices:     []int{6, 8, 10, 12}, // net_connect, net_unique_port, dns_query, dns_nx_ratio
		MinActive:   3,
		Severity:    "medium",
	},
}

type correlationPattern struct {
	Name        string
	Description string
	Indices     []int // metric indices to check
	MinActive   int   // minimum number of elevated metrics
	Severity    string
}

const (
	// retrainInterval is how often the forest is retrained from recent data.
	retrainInterval = 30 * time.Minute

	// sampleWindowSize is the max number of recent samples kept for training.
	sampleWindowSize = 2000

	// anomalyThreshold is the score above which a sample is flagged.
	anomalyThreshold = 0.65

	// correlationThreshold is z-score threshold for a metric to be "elevated".
	correlationThreshold = 2.0
)

// Detector is the server-side ML anomaly detection engine.
// It wraps an Isolation Forest with periodic retraining and
// multi-metric correlation detection.
type Detector struct {
	logger *zap.Logger
	db     *gorm.DB
	forest *IForest

	mu           sync.Mutex
	sampleBuffer [][]float64          // recent samples for training
	hostMeans    map[string][]float64 // per-host running mean for z-score
	hostCounts   map[string]int       // sample count per host
}

// NewDetector creates a new anomaly detection engine.
func NewDetector(db *gorm.DB, logger *zap.Logger) *Detector {
	return &Detector{
		logger:     logger,
		db:         db,
		forest:     NewIForest(),
		hostMeans:  make(map[string][]float64),
		hostCounts: make(map[string]int),
	}
}

// StartRetrain begins periodic retraining in the background.
// Call after Consumer startup.
func (d *Detector) StartRetrain(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(retrainInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				d.retrain()
			}
		}
	}()
}

// Ingest processes a BDE snapshot from a host.
// Returns anomaly alerts if any are generated.
func (d *Detector) Ingest(hostID, hostname string, metrics []float64) {
	if len(metrics) != featureCount {
		return
	}

	d.mu.Lock()
	// Add to sample buffer.
	d.sampleBuffer = append(d.sampleBuffer, metrics)
	if len(d.sampleBuffer) > sampleWindowSize {
		d.sampleBuffer = d.sampleBuffer[len(d.sampleBuffer)-sampleWindowSize:]
	}

	// Update per-host running mean (for correlation z-score).
	d.updateHostMean(hostID, metrics)
	hostMean := d.hostMeans[hostID]
	hostCount := d.hostCounts[hostID]
	d.mu.Unlock()

	// Skip detection during warm-up phase (need sufficient history).
	if hostCount < 50 {
		return
	}

	// 1. Isolation Forest scoring.
	if d.forest.Trained() {
		score := d.forest.Score(metrics)
		if score >= anomalyThreshold {
			d.emitForestAlert(hostID, hostname, metrics, score)
		}
	}

	// 2. Multi-metric correlation detection.
	if hostMean != nil {
		d.checkCorrelations(hostID, hostname, metrics, hostMean)
	}
}

// Trained returns whether the forest has been trained.
func (d *Detector) Trained() bool {
	return d.forest.Trained()
}

// SampleCount returns the number of samples in the training buffer.
func (d *Detector) SampleCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sampleBuffer)
}

// HostCount returns the number of unique hosts tracked.
func (d *Detector) HostCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.hostMeans)
}

// --- Internal methods ---

func (d *Detector) retrain() {
	d.mu.Lock()
	data := make([][]float64, len(d.sampleBuffer))
	copy(data, d.sampleBuffer)
	d.mu.Unlock()

	if len(data) < 64 {
		d.logger.Debug("insufficient samples for IForest training",
			zap.Int("samples", len(data)))
		return
	}

	d.forest.Train(data)
	d.logger.Info("IForest retrained",
		zap.Int("samples", len(data)),
		zap.Bool("trained", d.forest.Trained()))
}

func (d *Detector) updateHostMean(hostID string, metrics []float64) {
	mean, ok := d.hostMeans[hostID]
	if !ok {
		mean = make([]float64, featureCount)
		d.hostMeans[hostID] = mean
		d.hostCounts[hostID] = 0
	}

	d.hostCounts[hostID]++
	n := float64(d.hostCounts[hostID])

	// Online mean update: mean = mean + (x - mean) / n
	for i, v := range metrics {
		mean[i] += (v - mean[i]) / n
	}
}

func (d *Detector) emitForestAlert(hostID, hostname string, metrics []float64, score float64) {
	// Find the metric with the largest deviation from mean.
	d.mu.Lock()
	mean := d.hostMeans[hostID]
	d.mu.Unlock()

	topMetric := ""
	topValue := 0.0
	if mean != nil {
		maxDev := 0.0
		for i, v := range metrics {
			dev := v - mean[i]
			if dev < 0 {
				dev = -dev
			}
			if dev > maxDev {
				maxDev = dev
				topMetric = MetricNames[i]
				topValue = v
			}
		}
	}

	severity := "medium"
	if score >= 0.80 {
		severity = "critical"
	} else if score >= 0.70 {
		severity = "high"
	}

	// 拼描述：让 UI drawer 至少有一行有意义内容，避免 v-if 空白
	description := fmt.Sprintf("Isolation Forest 异常评分 %.2f（>=0.6 触发告警）", score)
	if topMetric != "" {
		description = fmt.Sprintf("指标 %s 偏离主机历史均值，当前值 %.2f；Isolation Forest 异常评分 %.2f",
			topMetric, topValue, score)
	}

	alert := model.AnomalyAlert{
		HostID:       hostID,
		Hostname:     hostname,
		AlertType:    "isolation_forest",
		Severity:     severity,
		AnomalyScore: score,
		TopMetric:    topMetric,
		TopValue:     topValue,
		Description:  description,
		Status:       "open",
	}

	if err := d.db.Create(&alert).Error; err != nil {
		d.logger.Error("failed to save anomaly alert", zap.Error(err))
	}

	d.logger.Warn("IForest anomaly detected",
		zap.String("host_id", hostID),
		zap.Float64("score", score),
		zap.String("top_metric", topMetric),
		zap.String("severity", severity))
}

func (d *Detector) checkCorrelations(hostID, hostname string, metrics, mean []float64) {
	for _, pattern := range correlationPatterns {
		elevatedCount := 0
		for _, idx := range pattern.Indices {
			if mean[idx] == 0 {
				continue
			}
			// Simple ratio-based elevation check (current/mean > threshold).
			ratio := metrics[idx] / mean[idx]
			if ratio > correlationThreshold {
				elevatedCount++
			}
		}

		if elevatedCount >= pattern.MinActive {
			alert := model.AnomalyAlert{
				HostID:       hostID,
				Hostname:     hostname,
				AlertType:    "correlation",
				PatternName:  pattern.Name,
				Severity:     pattern.Severity,
				AnomalyScore: float64(elevatedCount) / float64(len(pattern.Indices)),
				Description:  pattern.Description,
				Status:       "open",
			}

			if err := d.db.Create(&alert).Error; err != nil {
				d.logger.Error("failed to save correlation alert", zap.Error(err))
			}

			d.logger.Warn("correlation pattern detected",
				zap.String("host_id", hostID),
				zap.String("pattern", pattern.Name),
				zap.Int("elevated_metrics", elevatedCount),
				zap.String("severity", pattern.Severity))
		}
	}
}
