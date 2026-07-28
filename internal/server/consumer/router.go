// Package consumer 实现 Kafka Consumer 服务
// 从各 Topic 消费 MQMessage，路由到 MySQL / ClickHouse 写入器
package consumer

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/matrixplusio/mxcwpp/internal/common/jsonx"
	"github.com/matrixplusio/mxcwpp/internal/server/common/kafka"
	consumermetrics "github.com/matrixplusio/mxcwpp/internal/server/consumer/metrics"
	"github.com/matrixplusio/mxcwpp/internal/server/consumer/writer"
	"github.com/matrixplusio/mxcwpp/internal/server/engine/anomaly"
	"github.com/matrixplusio/mxcwpp/internal/server/engine/baseline"
	"github.com/matrixplusio/mxcwpp/internal/server/engine/celengine"
	"github.com/matrixplusio/mxcwpp/internal/server/engine/storyline"
	"github.com/matrixplusio/mxcwpp/internal/server/model"
)

// agentACTTL 是 Redis 中 agent:ac:{agentID} key 的 TTL（3 倍心跳间隔）
const agentACTTL = 180 * time.Second

// coldStartBehaviorAlertMinScore 学习期(冷启动全局基线 5σ)落 behavior_alert 的最低 risk_score。
// 低于此值的冷启动偏离视为噪声抑制(主机基线未就绪,全局基线在异构舰队误报多)。
const coldStartBehaviorAlertMinScore = 85.0

// activeBehaviorAlertMinScore 毕业主机(active)落 behavior_alert 的最低 risk_score。
// 原为 0(毕业后全量落库) → 单指标轻微越阈值(risk 很低)的 trivial 偏离全量刷库，是毕业主机
// 噪声洪水主因(BDE 评估实测 67 万/周稳态毕业主机产)。设风险下限只保留有意义偏离，
// 与 EWMA(基线自适应)/去重/节流叠加进一步收敛。低于此分仍进 anomaly/storyline 关联，只是不独立落 behavior_alert。
const activeBehaviorAlertMinScore = 60.0

// shouldPersistBehaviorAlert 决定 BDE 偏离是否落 behavior_alert。
// 学习期(冷启动)仅保留高信号；毕业后(非冷启动)保留 risk_score≥下限的有意义偏离。
func shouldPersistBehaviorAlert(coldStart bool, riskScore float64) bool {
	if coldStart {
		return riskScore >= coldStartBehaviorAlertMinScore
	}
	return riskScore >= activeBehaviorAlertMinScore
}

// Router 订阅所有业务 Topic，根据 DataType 路由到对应写入器
type Router struct {
	//nolint:unused // 嵌入 sarama.ConsumerGroupHandler 空实现，避免每次重写 Setup/Cleanup
	saramaConsumerGroupHandler
	group           sarama.ConsumerGroup
	mysql           *writer.MySQLWriter
	ch              *writer.ClickHouseWriter
	dlq             *DLQHandler
	redisClient     *redis.Client           // 可选，用于写 agent:ac: 映射
	bdeEngine       *baseline.Engine        // BDE 基线引擎（可选）
	bdeThrottler    *celengine.HitThrottler // BDE 告警节流：同 (host, metric) 高频复发窗内跳过写入
	anomalyDetector *anomaly.Detector       // ML 异常检测引擎（可选）
	storyEngine     *storyline.Engine       // 攻击故事线引擎（可选）
	topics          []string
	logger          *zap.Logger
	// suppressed 运维事件抑制窗内的 host 集(hosts.behavior_suppress_until>now)，每 30s 刷新一次。
	// 窗内低信号 BDE 偏离不落库，滤掉 agent 重连/插件重载引发的 WAL 重放突发假异常。
	suppressed atomic.Pointer[map[string]struct{}]
}

// NewRouter 创建 Router (v2 拆分: Consumer 仅 writer 路径, 不做 CEL 检测).
//
// CEL/AlertGenerator/AutoResponder/ScanDetector/SequenceDetector 全部迁到 Engine 服务
// (cmd/server/engine + internal/server/engine/stage_cel). Consumer 只订阅 Kafka writer topic
// 持久化到 MySQL/ClickHouse.
func NewRouter(
	brokers []string,
	groupID string,
	topicPrefix string,
	mysql *writer.MySQLWriter,
	ch *writer.ClickHouseWriter,
	dlq *DLQHandler,
	redisClient *redis.Client, // 可为 nil，Redis 不可用时跳过 agent:ac: 写入
	initialOffset string, // 冷启动初始位点："newest"（默认）或 "oldest"
	logger *zap.Logger,
) (*Router, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_6_0_0
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	initial := parseInitialOffset(initialOffset)
	cfg.Consumer.Offsets.Initial = initial
	cfg.Consumer.Return.Errors = true
	// 仅冷启动（新消费组 / offset 过期）时 Initial 生效；提示位点语义避免运维误判积压去向。
	if initial == sarama.OffsetNewest {
		logger.Warn("Kafka consumer 初始位点=newest：冷启动/offset 过期时从最新位点消费，" +
			"可能跳过 producer 已写入但未消费的积压事件（如需消费积压设 kafka.consumer.initial_offset=oldest）")
	} else {
		logger.Warn("Kafka consumer 初始位点=oldest：冷启动从最早未消费开始，" +
			"注意 ClickHouse append 写入会因重放产生重复（MySQL upsert 幂等）")
	}

	group, err := sarama.NewConsumerGroup(brokers, groupID, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 Kafka ConsumerGroup 失败: %w", err)
	}

	prefix := topicPrefix
	topics := []string{
		prefix + kafka.TopicHeartbeat,
		prefix + kafka.TopicEvents,
		prefix + kafka.TopicBaseline,
		prefix + kafka.TopicAsset,
		prefix + kafka.TopicCommandAck,
		prefix + kafka.TopicScanner,
		prefix + kafka.TopicEBPF,
		prefix + kafka.TopicRemediation,
	}

	r := &Router{
		group:       group,
		mysql:       mysql,
		ch:          ch,
		dlq:         dlq,
		redisClient: redisClient,
		topics:      topics,
		logger:      logger,
		// BDE 告警节流：同 (host, metric) 1min 内命中超 50 次开启 10min 静默窗，
		// 削掉稳态偏离每 60s 复发的写入 churn（容量覆盖 host×13metric 组合）。
		bdeThrottler: celengine.NewHitThrottler(50, time.Minute, 20000),
	}
	go r.refreshSuppressLoop()
	return r, nil
}

// parseInitialOffset 将配置字符串解析为 sarama 冷启动初始位点。
// "oldest" → OffsetOldest（从最早未消费开始，需消费幂等）；其余（含空 / "newest"）→ OffsetNewest（默认）。
func parseInitialOffset(s string) int64 {
	if strings.EqualFold(strings.TrimSpace(s), "oldest") {
		return sarama.OffsetOldest
	}
	return sarama.OffsetNewest
}

// behaviorSuppressBypassScore 运维抑制窗内仍放行的最低 risk_score——真高危(反弹shell/挖矿级)
// 不因维护窗被压掉,只滤掉低信号突发噪声。
const behaviorSuppressBypassScore = 95.0

// refreshSuppressLoop 每 30s 刷新"运维抑制窗内 host 集"(hosts.behavior_suppress_until>now)。
func (r *Router) refreshSuppressLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		set := map[string]struct{}{}
		if r.mysql != nil {
			if db := r.mysql.DB(); db != nil {
				var ids []string
				db.Model(&model.Host{}).
					Where("behavior_suppress_until IS NOT NULL AND behavior_suppress_until > ?", time.Now()).
					Pluck("host_id", &ids)
				for _, id := range ids {
					set[id] = struct{}{}
				}
			}
		}
		r.suppressed.Store(&set)
		<-t.C
	}
}

// isBehaviorSuppressed 该 host 是否在运维抑制窗内。
func (r *Router) isBehaviorSuppressed(hostID string) bool {
	m := r.suppressed.Load()
	if m == nil {
		return false
	}
	_, ok := (*m)[hostID]
	return ok
}

// SetBDEEngine sets the optional BDE baseline engine for behavior anomaly detection.
func (r *Router) SetBDEEngine(eng *baseline.Engine) {
	r.bdeEngine = eng
}

// SetAnomalyDetector sets the optional ML anomaly detection engine.
func (r *Router) SetAnomalyDetector(det *anomaly.Detector) {
	r.anomalyDetector = det
}

// SetStorylineEngine sets the optional attack storyline engine.
func (r *Router) SetStorylineEngine(eng *storyline.Engine) {
	r.storyEngine = eng
}

// Run 阻塞式消费，直到 ctx 取消
//
// 所有 sarama 错误均退避重试，不返回错误退出进程。避免单次 broker 抖动 / rebalance / 网络
// 中断导致 Consumer 永久死亡（main 会 os.Exit(1)），需依赖容器编排器重启。
func (r *Router) Run(ctx context.Context) error {
	// 后台消费 sarama 错误
	go func() {
		for err := range r.group.Errors() {
			r.logger.Error("ConsumerGroup 错误", zap.Error(err))
		}
	}()

	const (
		minBackoff = 1 * time.Second
		maxBackoff = 30 * time.Second
	)
	backoff := minBackoff

	for {
		if ctx.Err() != nil {
			return nil
		}
		err := r.group.Consume(ctx, r.topics, r)
		if err == nil {
			backoff = minBackoff
			continue
		}
		// ctx 已取消，正常退出
		if ctx.Err() != nil {
			return nil
		}
		r.logger.Warn("ConsumerGroup Consume 出错，退避后重试",
			zap.Duration("backoff", backoff),
			zap.Error(err),
		)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		// 指数退避，最大 30s
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// Setup 实现 sarama.ConsumerGroupHandler.Setup，记录当前消费组成员数指标。
// sarama 在每次 rebalance 完成后调用一次，session.Claims() 返回该实例分到的 topic→partitions。
func (r *Router) Setup(session sarama.ConsumerGroupSession) error {
	partitions := 0
	for _, ps := range session.Claims() {
		partitions += len(ps)
	}
	// 成员数无法直接获取，至少表达"本实例已加入组并分到 partition"
	consumermetrics.SetGroupMembers(1)
	r.logger.Info("ConsumerGroup Session 建立",
		zap.String("member_id", session.MemberID()),
		zap.Int32("generation_id", session.GenerationID()),
		zap.Int("assigned_partitions", partitions),
	)
	return nil
}

// Cleanup 实现 sarama.ConsumerGroupHandler.Cleanup，rebalance 触发时清零成员指标。
//
// rebalance 会提交已标记的 offset 并把分区交给其他实例。此处先同步 flush ClickHouse 内存
// 批次，让已消费但仍在批次里的 ebpf/fim/metrics 在分区重分配前落盘，避免重平衡/部署丢在途
// 批次（graceful 退出已由 Close→done 覆盖；kill -9 硬崩溃残留窗口见 ClickHouseWriter.Flush 注释）。
func (r *Router) Cleanup(session sarama.ConsumerGroupSession) error {
	if r.ch != nil {
		r.ch.Flush()
	}
	consumermetrics.SetGroupMembers(0)
	r.logger.Info("ConsumerGroup Session 结束", zap.String("member_id", session.MemberID()))
	return nil
}

// Close 关闭 ConsumerGroup
func (r *Router) Close() error {
	return r.group.Close()
}

// ConsumeClaim 实现 sarama.ConsumerGroupHandler，处理每条消息
func (r *Router) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			r.handleMessage(session, msg)
		case <-session.Context().Done():
			return nil
		}
	}
}

// handleMessage 解码 MQMessage 并路由到对应写入器
func (r *Router) handleMessage(session sarama.ConsumerGroupSession, raw *sarama.ConsumerMessage) {
	// Prometheus: 测量端到端处理延迟与结果（success/error/dlq）
	start := time.Now()
	procStatus := "success" // 默认成功；解码失败或写入失败时改写
	var dataTypeLabel = "unknown"
	defer func() {
		consumermetrics.RecordProcessing(raw.Topic, dataTypeLabel, procStatus, time.Since(start))
	}()

	// P2-6: 池化 MQMessage 减 GC 压力
	msg := kafka.GetMQMessage()
	defer kafka.PutMQMessage(msg)
	if err := jsonx.Unmarshal(raw.Value, msg); err != nil {
		r.logger.Error("反序列化 MQMessage 失败",
			zap.String("topic", raw.Topic),
			zap.Error(err),
		)
		procStatus = "error"
		session.MarkMessage(raw, "")
		return
	}
	dataTypeLabel = strconv.Itoa(int(msg.DataType))

	var writeErr error
	switch {
	case msg.DataType == 1000:
		// 心跳：upsert hosts 表 + 写 Redis agent:ac: 映射 + 写 ClickHouse 指标
		writeErr = r.mysql.WriteHeartbeat(msg)
		r.writeAgentACMapping(msg)
		r.chWrite("host_metrics", r.ch.WriteHostMetrics(msg))
	case msg.DataType == 1001:
		r.chWrite("host_metrics", r.ch.WriteHostMetrics(msg)) // 插件心跳，Phase 4 实现

	// 资产数据（5050~5060）
	case msg.DataType >= 5050 && msg.DataType <= 5060:
		writeErr = r.mysql.WriteAsset(msg, msg.DataType)

	// FIM 事件
	case msg.DataType == 6001:
		writeErr = r.mysql.WriteFIMEvent(msg)
		if writeErr == nil {
			r.chWrite("fim_event", r.ch.WriteFIMEvent(msg))
			r.evaluateCEL(msg)
		}
	// FIM 任务完成
	case msg.DataType == 6002:
		writeErr = r.mysql.WriteFIMTaskComplete(msg)

	// 基线检查结果
	case msg.DataType == 8000:
		writeErr = r.mysql.WriteBaseline(msg)
	// 基线扫描任务完成
	case msg.DataType == 8001:
		writeErr = r.mysql.WriteTaskCompletion(msg)
	// 修复结果
	case msg.DataType == 8003:
		writeErr = r.mysql.WriteFixResult(msg)
	// 修复任务完成
	case msg.DataType == 8004:
		writeErr = r.mysql.WriteFixTaskComplete(msg)

	// Scanner 扫描结果
	case msg.DataType == 7001:
		writeErr = r.mysql.WriteScanResult(msg)
		if writeErr == nil {
			r.evaluateCEL(msg)
		}
	// Scanner 任务完成
	case msg.DataType == 7002:
		writeErr = r.mysql.WriteScanTaskComplete(msg)
	// Scanner 隔离/删除结果
	case msg.DataType == 7004:
		writeErr = r.mysql.WriteQuarantineResult(msg)

	// BDE 行为画像快照
	case msg.DataType == 3010:
		r.evaluateBDE(msg)

	// 内存威胁事件
	case msg.DataType == 3004:
		r.writeMemoryThreat(msg)

	// eBPF 事件（3000-3003，含 DNS 事件）
	case msg.DataType >= 3000 && msg.DataType <= 3003:
		r.chWrite("ebpf_event", r.ch.WriteEBPFEvent(msg))
		r.evaluateCEL(msg)
		r.ingestStoryline(msg)
		// 网络事件额外进行端口扫描检测
		if msg.DataType == 3002 {
			r.checkPortScan(msg)
		}

	// 漏洞修复结果
	case msg.DataType == 9200:
		writeErr = r.mysql.WriteRemediationResult(msg)

	// 漏洞修复阶段进度（11 state lifecycle 实时事件）
	case msg.DataType == 9201:
		writeErr = r.mysql.WriteRemediationProgress(msg)

	// 命令执行回包
	case msg.DataType == 9999:
		writeErr = r.mysql.WriteCommandAck(msg)

	default:
		// 未知/未路由 DataType 不再静默丢弃（原 Debug 日志 = 数据黑洞）：转 DLQ + 计数告警，
		// 便于发现新功能上线时的路由缺口 / producer 错误 DataType。
		r.logger.Warn("消费到未路由的未知 DataType，转入 DLQ",
			zap.Int32("data_type", msg.DataType),
			zap.String("agent_id", msg.AgentID),
		)
		consumermetrics.RecordUnknownDataType(dataTypeLabel)
		r.dlq.Send(raw.Topic, msg, fmt.Errorf("unknown data type: %d", msg.DataType), 1)
		procStatus = "dlq"
	}

	if writeErr != nil {
		r.logger.Error("写入失败，转入 DLQ",
			zap.String("topic", raw.Topic),
			zap.Int32("data_type", msg.DataType),
			zap.String("agent_id", msg.AgentID),
			zap.Error(writeErr),
		)
		r.dlq.Send(raw.Topic, msg, writeErr, 1)
		procStatus = "dlq"
	}

	// 不论成功失败，均标记 offset（失败消息已进 DLQ，不阻塞消费进度）
	session.MarkMessage(raw, "")
}

// chWrite 统一处理 ClickHouse 写结果：失败则计数 + 告警日志。
// 不进 DLQ（重放会重复 MySQL 幂等写，代价大）——双写漂移由 ch_write_errors 指标暴露，
// 后续 outbox/对账治理（架构评估 H2）。原各站点 `_ = r.ch.Write...` 静默吞错。
func (r *Router) chWrite(op string, err error) {
	if err != nil {
		consumermetrics.RecordCHWriteError(op)
		r.logger.Warn("ClickHouse 写入失败", zap.String("op", op), zap.Error(err))
	}
}

// evaluateCEL noop (v2 拆分: 检测全部走 Engine 服务).
//
// 旧架构 Consumer 内嵌 CEL 引擎评估事件 + 写 alerts 表. v2 重构后所有 CEL / Sequence /
// AutoResponse 迁到 cmd/server/engine, Consumer 仅 writer (Kafka → MySQL/CH). 此函数保留
// 为空 stub 兼容 Process 调用点, 不实际执行检测.
func (r *Router) evaluateCEL(_ *kafka.MQMessage) {
	// no-op: 检测能力已迁到 Engine 服务 (internal/server/engine/stage_cel.go)
}

// checkPortScan noop (v2 拆分: 端口扫描检测迁到 Engine 服务).
func (r *Router) checkPortScan(_ *kafka.MQMessage) {
	// no-op: ScanDetector 现归 cmd/server/engine 管理
}

// evaluateBDE parses a BDE behavior profile snapshot and feeds it to the baseline engine.
func (r *Router) evaluateBDE(msg *kafka.MQMessage) {
	if r.bdeEngine == nil {
		return
	}

	fields, err := writer.ParseRecordFields(msg.Body)
	if err != nil {
		return
	}

	var metrics [baseline.MetricCount]float64
	metrics[baseline.MetricProcExecCount] = parseFloat(fields["proc_exec_count"])
	metrics[baseline.MetricProcUniqueExe] = parseFloat(fields["proc_unique_exe"])
	metrics[baseline.MetricProcForkRate] = parseFloat(fields["proc_fork_rate"])
	metrics[baseline.MetricFileWriteCount] = parseFloat(fields["file_write_count"])
	metrics[baseline.MetricFileUniquePath] = parseFloat(fields["file_unique_path"])
	metrics[baseline.MetricFileSensitiveHits] = parseFloat(fields["file_sensitive_hits"])
	metrics[baseline.MetricNetConnectCount] = parseFloat(fields["net_connect_count"])
	metrics[baseline.MetricNetUniqueIP] = parseFloat(fields["net_unique_ip"])
	metrics[baseline.MetricNetUniquePort] = parseFloat(fields["net_unique_port"])
	metrics[baseline.MetricNetExternalRatio] = parseFloat(fields["net_external_ratio"])
	metrics[baseline.MetricDNSQueryCount] = parseFloat(fields["dns_query_count"])
	metrics[baseline.MetricDNSUniqueDomain] = parseFloat(fields["dns_unique_domain"])
	metrics[baseline.MetricDNSNXRatio] = parseFloat(fields["dns_nx_ratio"])

	// Feed ML anomaly detector (IForest + correlation).
	if r.anomalyDetector != nil {
		r.anomalyDetector.Ingest(msg.AgentID, msg.Hostname, metrics[:])
	}

	result := r.bdeEngine.Ingest(msg.AgentID, metrics)
	if result == nil {
		return
	}

	// 学习期降噪：主机基线未就绪时走全局基线冷启动(5σ),在异构舰队上误报多。
	// prod 实测全队列 learning 期刷 ~1万/天 behavior_alert 且无人处置(纯噪声)。
	// 冷启动告警仅保留高信号(risk_score≥阈值),其余抑制；per-host 基线就绪(active)后照常全量。
	if !shouldPersistBehaviorAlert(result.ColdStart, result.RiskScore) {
		return
	}

	// 运维事件抑制:host 在抑制窗内(agent 刚重连/插件刚推送)且非真高危时不落库，
	// 滤掉 WAL 重放/采集突发引发的假异常;真高危(≥95)仍放行。
	if result.RiskScore < behaviorSuppressBypassScore && r.isBehaviorSuppressed(msg.AgentID) {
		return
	}

	// 持久化每条偏离到 behavior_alerts 表（提供按 metric / z_score 维度的分析能力）。
	// 与 alerts 表（通用告警，title="bde_anomaly_*"）并存：
	//   - alerts 表 → CEL 规则引擎统一去重 + AutoResponder 联动
	//   - behavior_alerts 表 → UI ListBehaviorAlerts API 按 BDE 维度展示 + 趋势分析
	// 历史问题：behavior_alerts 表定义但无写入逻辑 → 0 行。
	if r.mysql != nil {
		if db := r.mysql.DB(); db != nil {
			now := time.Now()
			for _, dev := range result.Deviations {
				// 1c 节流：同 (host, metric) 高频复发在静默窗内跳过写入，削下游 churn。
				if r.bdeThrottler != nil && !r.bdeThrottler.Allow(msg.AgentID, dev.Metric, now) {
					continue
				}
				ba := model.BehaviorAlert{
					HostID:    msg.AgentID,
					Hostname:  msg.Hostname,
					RiskScore: result.RiskScore,
					Metric:    dev.Metric,
					Value:     dev.Value,
					Mean:      dev.Mean,
					Stddev:    dev.Stddev,
					ZScore:    dev.ZScore,
					Status:    "open",
					HitCount:  1,
				}
				// 1d upsert：同 (tenant, host, metric) 已存在则累加 hit_count + 刷新最新偏离值，
				// 不新增行。status 不覆盖（ignored/resolved 保持，避免复发把已处置的重新 open）。
				if err := db.Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "tenant_id"}, {Name: "host_id"}, {Name: "metric"}},
					DoUpdates: clause.Assignments(map[string]any{
						"risk_score": result.RiskScore,
						"value":      dev.Value,
						"mean":       dev.Mean,
						"stddev":     dev.Stddev,
						"z_score":    dev.ZScore,
						"hit_count":  gorm.Expr("hit_count + 1"),
						"updated_at": now,
					}),
				}).Create(&ba).Error; err != nil {
					r.logger.Warn("写 behavior_alerts 失败", zap.Error(err))
				}
			}
		}
	}

	// v2 拆分: BDE 异常 alerts 改由 Engine 服务的 StorylineStage / Anomaly stage 升级落 DB.
	// Consumer 只持久化 behavior_alerts 维度 (上面 db.Create), 不直接写 alerts 表.

	r.logger.Info("BDE 异常检出",
		zap.String("host_id", msg.AgentID),
		zap.Float64("risk_score", result.RiskScore),
		zap.Int("deviations", len(result.Deviations)),
	)
}

// ingestStoryline feeds events with story_id to the storyline engine.
func (r *Router) ingestStoryline(msg *kafka.MQMessage) {
	if r.storyEngine == nil {
		return
	}
	fields, err := writer.ParseRecordFields(msg.Body)
	if err != nil {
		return
	}
	storyID := fields["story_id"]
	if storyID == "" {
		return
	}
	r.storyEngine.Ingest(storyID, msg.AgentID, msg.Hostname, msg.DataType, fields)
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// writeMemoryThreat persists a memory threat event to MySQL and evaluates CEL rules.
func (r *Router) writeMemoryThreat(msg *kafka.MQMessage) {
	if err := r.mysql.WriteMemoryThreat(msg); err != nil {
		r.logger.Warn("写入内存威胁失败",
			zap.String("host_id", msg.AgentID),
			zap.Error(err),
		)
	}
	r.evaluateCEL(msg)
}

// writeAgentACMapping 将 agent:ac:{agentID}=acID 写入 Redis（TTL=180s）
// 供 Manager 查询 Agent 所在 AC 实例，用于精准任务路由
func (r *Router) writeAgentACMapping(msg *kafka.MQMessage) {
	if r.redisClient == nil || msg.ACID == "" {
		return
	}
	key := "agent:ac:" + msg.AgentID
	if err := r.redisClient.Set(context.Background(), key, msg.ACID, agentACTTL).Err(); err != nil {
		r.logger.Warn("写 agent:ac: Redis 映射失败",
			zap.String("agent_id", msg.AgentID),
			zap.String("ac_id", msg.ACID),
			zap.Error(err),
		)
	}
}
