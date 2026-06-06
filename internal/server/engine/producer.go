package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
)

// AlertEnvelope 是 Engine 产出的告警消息体 (落 Kafka mxsec.engine.alert)。
//
// 字段对齐 docs/operating-modes.md §6:
//   - Mode: observe / protect
//   - WouldAction: observe 模式预期动作
//   - Action / ActionResult: protect 模式实际动作
type AlertEnvelope struct {
	AlertID        string          `json:"alert_id"`
	TenantID       string          `json:"tenant_id"`
	HostID         string          `json:"host_id,omitempty"`
	RuleID         string          `json:"rule_id"`
	Severity       string          `json:"severity"`
	Mode           string          `json:"mode"` // observe / protect
	DetectedAt     time.Time       `json:"detected_at"`
	ATTCKTactic    string          `json:"attck_tactic,omitempty"`
	ATTCKTechnique string          `json:"attck_technique,omitempty"`
	WouldAction    json.RawMessage `json:"would_action,omitempty"`
	Action         json.RawMessage `json:"action,omitempty"`
	ActionResult   json.RawMessage `json:"action_result,omitempty"`
	AttackChain    json.RawMessage `json:"attack_chain,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	TraceID        string          `json:"trace_id,omitempty"`
}

// AlertProducer 是 Engine 向 Kafka 推送告警的生产者。
type AlertProducer struct {
	producer sarama.SyncProducer
	topic    string
	logger   *zap.Logger
}

// NewAlertProducer 构造告警 producer。
func NewAlertProducer(brokers []string, topic string, logger *zap.Logger) (*AlertProducer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("engine: alert producer brokers must not be empty")
	}
	if topic == "" {
		topic = "mxsec.engine.alert"
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_5_0_0
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 3
	cfg.Producer.Return.Successes = true
	cfg.Producer.Compression = sarama.CompressionSnappy

	p, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("engine: new sync producer: %w", err)
	}

	return &AlertProducer{
		producer: p,
		topic:    topic,
		logger:   logger,
	}, nil
}

// Publish 同步推送告警。
//
// 失败重试 3 次 (sarama 内置); 全部失败返回 error。
// Partition Key = "{tenant_id}:{host_id}" 保证同主机告警有序。
func (p *AlertProducer) Publish(ctx context.Context, env AlertEnvelope) error {
	if env.DetectedAt.IsZero() {
		env.DetectedAt = time.Now().UTC()
	}
	if env.Mode == "" {
		env.Mode = "observe" // 默认监听模式
	}

	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("engine alert: marshal: %w", err)
	}

	key := env.TenantID + ":" + env.HostID

	msg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(body),
		Headers: []sarama.RecordHeader{
			{Key: []byte("mode"), Value: []byte(env.Mode)},
			{Key: []byte("tenant_id"), Value: []byte(env.TenantID)},
			{Key: []byte("rule_id"), Value: []byte(env.RuleID)},
		},
	}

	// ctx cancellation 不被 sarama 直接尊重,这里手动检查
	if ctx.Err() != nil {
		return ctx.Err()
	}

	partition, offset, err := p.producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("engine alert send: %w", err)
	}
	p.logger.Debug("engine alert published",
		zap.String("topic", p.topic),
		zap.Int32("partition", partition),
		zap.Int64("offset", offset),
		zap.String("alert_id", env.AlertID),
		zap.String("mode", env.Mode),
	)
	return nil
}

// Close 关闭 producer。
func (p *AlertProducer) Close() error {
	return p.producer.Close()
}
