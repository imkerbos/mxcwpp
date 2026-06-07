package outbound

// 阿里云日志服务 SLS connector (P2-18).
//
// 协议参考: https://help.aliyun.com/document_detail/29026.html
// API: POST https://<project>.<region>.log.aliyuncs.com/logstores/<logstore>/shards/lb
// Body: protobuf 格式 (简化版改用 JSON Webhook API)
//
// 当前简化实现走 SLS HTTP Webhook 入口 (无需 protobuf 依赖).
// 完整 protobuf 实现见: github.com/aliyun/aliyun-log-go-sdk

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// AliyunSLSConnector 阿里云 SLS 日志服务推送.
type AliyunSLSConnector struct {
	endpoint        string // <project>.<region>.log.aliyuncs.com
	project         string
	logstore        string
	accessKeyID     string
	accessKeySecret string
	client          *http.Client
	logger          *zap.Logger
}

// NewAliyunSLSConnector 构造.
func NewAliyunSLSConnector(project, logstore, region, accessKeyID, accessKeySecret string, logger *zap.Logger) *AliyunSLSConnector {
	if logger == nil {
		logger = zap.NewNop()
	}
	if region == "" {
		region = "cn-hangzhou"
	}
	return &AliyunSLSConnector{
		endpoint:        fmt.Sprintf("%s.%s.log.aliyuncs.com", project, region),
		project:         project,
		logstore:        logstore,
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		client:          &http.Client{Timeout: 10 * time.Second},
		logger:          logger,
	}
}

// Name 名字.
func (c *AliyunSLSConnector) Name() string { return "aliyun_sls" }

// Send 推送 Event 到 SLS logstore.
//
// 注: 简化用 JSON body + putlogs HTTP API (实际 protobuf 性能更好, 留 M2 替换).
func (c *AliyunSLSConnector) Send(ctx context.Context, ev *Event) error {
	url := fmt.Sprintf("https://%s/logstores/%s/shards/lb", c.endpoint, c.logstore)

	// 构造 SLS LogGroup JSON (简化版)
	body := map[string]interface{}{
		"__topic__": "mxsec",
		"__source__": ev.HostName,
		"logs": []map[string]interface{}{
			{
				"__time__": ev.Timestamp.Unix(),
				"alert_id": ev.ID,
				"tenant_id": ev.TenantID,
				"host_id": ev.HostID,
				"severity": ev.Severity,
				"category": ev.Category,
				"rule_id": ev.RuleID,
				"title": ev.Title,
				"description": ev.Description,
				"mitre_id": ev.MitreID,
				"source": ev.Source,
			},
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return err
	}
	// SLS HTTP API 鉴权头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-log-apiversion", "0.6.0")
	req.Header.Set("x-log-signaturemethod", "hmac-sha1")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	bodyMD5 := md5.Sum(bodyJSON)
	req.Header.Set("Content-MD5", strings.ToUpper(hex.EncodeToString(bodyMD5[:])))
	req.Header.Set("Content-Length", strconv.Itoa(len(bodyJSON)))
	// 签名
	signature := c.sign(req, bodyJSON)
	req.Header.Set("Authorization", "LOG "+c.accessKeyID+":"+signature)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("sls do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("sls status %d", resp.StatusCode)
	}
	return nil
}

// sign SLS HMAC-SHA1 签名.
//
// 简化实现: 实际生产用 aliyun-log-go-sdk 签名函数, 这里仅给最小可工作版本.
func (c *AliyunSLSConnector) sign(req *http.Request, body []byte) string {
	canonicalString := strings.Join([]string{
		req.Method,
		req.Header.Get("Content-MD5"),
		req.Header.Get("Content-Type"),
		req.Header.Get("Date"),
		"x-log-apiversion:" + req.Header.Get("x-log-apiversion"),
		"x-log-signaturemethod:" + req.Header.Get("x-log-signaturemethod"),
		req.URL.Path,
	}, "\n")
	mac := hmac.New(sha1.New, []byte(c.accessKeySecret))
	mac.Write([]byte(canonicalString))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// Close 释放 http client.
func (c *AliyunSLSConnector) Close() error {
	c.client.CloseIdleConnections()
	return nil
}
