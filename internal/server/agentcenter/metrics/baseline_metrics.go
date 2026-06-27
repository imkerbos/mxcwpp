package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 基线扫描任务管道指标。
// 大批量并发扫描时，结果摄取与任务收敛此前不可观测（无指标、关键日志被日志级别吞掉），
// 故障难定位。以下 Counter 暴露摄取速率、主机完成数与任务结局，供 Prometheus 抓取。
var (
	baselineResultReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mxcwpp_baseline_result_received_total",
		Help: "Total baseline check results (DataType 8000) received and persisted by agentcenter",
	})

	baselineHostCompleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mxcwpp_baseline_host_completed_total",
		Help: "Total baseline task host-completion signals processed by agentcenter",
	})

	// outcome: completed / partial / failed / retried
	baselineTaskOutcome = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mxcwpp_baseline_task_outcome_total",
		Help: "Baseline scan task lifecycle outcomes on timeout handling",
	}, []string{"outcome"})
)

// 任务结局取值
const (
	BaselineOutcomeCompleted = "completed"
	BaselineOutcomePartial   = "partial"
	BaselineOutcomeFailed    = "failed"
	BaselineOutcomeRetried   = "retried"
)

// IncBaselineResultReceived 记录一条基线结果被成功持久化。
func IncBaselineResultReceived() { baselineResultReceived.Inc() }

// IncBaselineHostCompleted 记录一台主机的完成信号被处理。
func IncBaselineHostCompleted() { baselineHostCompleted.Inc() }

// IncBaselineTaskOutcome 记录一次任务结局（completed/partial/failed/retried）。
func IncBaselineTaskOutcome(outcome string) {
	baselineTaskOutcome.WithLabelValues(outcome).Inc()
}
