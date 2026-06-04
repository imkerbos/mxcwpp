package advisory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// NVD JSON API 2.0 enrich client。
//
// 用途:对本地已收录但 cvss_score=0 / severity="none" 的 vulnerability(主要来自
// RHSA / OSV source — 不提供 NVD 风格的 CVSS),按 cve_id 单查 NVD 补齐
// CVSS v3.1 score、qualitative severity、description 字段。
//
// 不走 Source 接口:NVD 无 OS pkg fix version,不该当 advisory 主源用,只做 enrich。
// Rate limit:NVD 无 API key 时 5 req / 30s(6s 间隔);有 key 50 req / 30s(0.6s 间隔)。
// 本实现默认无 key,按 6s 间隔单 CVE 查询;支持外部传 API key。
//
// API: https://services.nvd.nist.gov/rest/json/cves/2.0?cveId=CVE-2024-XXX

const (
	nvdBaseURL        = "https://services.nvd.nist.gov/rest/json/cves/2.0"
	nvdRateNoAPIKey   = 6 * time.Second // 30s / 5 req
	nvdRateWithKey    = 600 * time.Millisecond
	nvdRequestTimeout = 15 * time.Second
)

// NVDClient 单查 CVE 的 NVD JSON 2.0 API client。
type NVDClient struct {
	apiKey   string
	interval time.Duration
	http     *http.Client
	logger   *zap.Logger
}

// NewNVDClient 创建 NVD client。apiKey 为空时走无 key 限速(6s 间隔)。
func NewNVDClient(apiKey string, logger *zap.Logger) *NVDClient {
	if logger == nil {
		logger = zap.NewNop()
	}
	interval := nvdRateNoAPIKey
	if apiKey != "" {
		interval = nvdRateWithKey
	}
	return &NVDClient{
		apiKey:   apiKey,
		interval: interval,
		http:     &http.Client{Timeout: nvdRequestTimeout},
		logger:   logger,
	}
}

// NVDEnrichResult NVD 单 CVE 查询的精简结果(只取 enrich 用得着的字段)。
type NVDEnrichResult struct {
	CVEID       string
	CVSSScore   float64 // v3.1 base score,无则 0
	CVSSVector  string  // v3.1 vector string
	Severity    string  // critical / high / medium / low / none(v3.1 baseSeverity 小写)
	Description string  // 英文 description(NVD 不提供中文)
	Published   time.Time
	LastMod     time.Time
}

// Lookup 查询单个 CVE。NotFound 时返回 (nil, nil)。
func (c *NVDClient) Lookup(ctx context.Context, cveID string) (*NVDEnrichResult, error) {
	cveID = strings.TrimSpace(cveID)
	if !strings.HasPrefix(cveID, "CVE-") {
		return nil, fmt.Errorf("invalid cve_id %q", cveID)
	}
	url := fmt.Sprintf("%s?cveId=%s", nvdBaseURL, cveID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mxsec-platform/1.0 (nvd-enrich)")
	if c.apiKey != "" {
		req.Header.Set("apiKey", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nvd http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("nvd http %d: %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseNVD2Response(cveID, body)
}

// LookupBatch 按 cveID 列表逐个查询,在每次请求之间 sleep(rate limit)。
// 收集成功结果,失败仅记 warn 日志(不中断)。返回 cve_id → result。
//
// ctx 超时控制整体批次时长上限(调用方决定)。
func (c *NVDClient) LookupBatch(ctx context.Context, cveIDs []string) map[string]*NVDEnrichResult {
	out := make(map[string]*NVDEnrichResult, len(cveIDs))
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	first := true
	for _, id := range cveIDs {
		if !first {
			select {
			case <-ctx.Done():
				c.logger.Info("NVD enrich ctx cancelled", zap.Int("processed", len(out)))
				return out
			case <-ticker.C:
			}
		}
		first = false
		res, err := c.Lookup(ctx, id)
		if err != nil {
			c.logger.Warn("nvd lookup failed", zap.String("cve", id), zap.Error(err))
			continue
		}
		if res != nil {
			out[id] = res
		}
	}
	return out
}

// --- NVD JSON 2.0 schema(只取必需字段) ---

type nvd2Response struct {
	Vulnerabilities []struct {
		CVE struct {
			ID           string `json:"id"`
			Published    string `json:"published"`
			LastModified string `json:"lastModified"`
			Descriptions []struct {
				Lang  string `json:"lang"`
				Value string `json:"value"`
			} `json:"descriptions"`
			Metrics struct {
				// CVSS v3.1 优先,fallback v3.0,再 fallback v2
				CVSSMetricV31 []nvd2CVSSv3 `json:"cvssMetricV31"`
				CVSSMetricV30 []nvd2CVSSv3 `json:"cvssMetricV30"`
				CVSSMetricV2  []nvd2CVSSv2 `json:"cvssMetricV2"`
			} `json:"metrics"`
		} `json:"cve"`
	} `json:"vulnerabilities"`
}

type nvd2CVSSv3 struct {
	CVSSData struct {
		BaseScore    float64 `json:"baseScore"`
		BaseSeverity string  `json:"baseSeverity"`
		VectorString string  `json:"vectorString"`
	} `json:"cvssData"`
}

type nvd2CVSSv2 struct {
	CVSSData struct {
		BaseScore    float64 `json:"baseScore"`
		VectorString string  `json:"vectorString"`
	} `json:"cvssData"`
	BaseSeverity string `json:"baseSeverity"`
}

func parseNVD2Response(cveID string, body []byte) (*NVDEnrichResult, error) {
	var r nvd2Response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("nvd parse: %w", err)
	}
	if len(r.Vulnerabilities) == 0 {
		return nil, nil
	}
	v := r.Vulnerabilities[0].CVE
	out := &NVDEnrichResult{CVEID: v.ID}
	// 取英文 description
	for _, d := range v.Descriptions {
		if d.Lang == "en" {
			out.Description = d.Value
			break
		}
	}
	// CVSS:v3.1 > v3.0 > v2
	switch {
	case len(v.Metrics.CVSSMetricV31) > 0:
		m := v.Metrics.CVSSMetricV31[0].CVSSData
		out.CVSSScore = m.BaseScore
		out.CVSSVector = m.VectorString
		out.Severity = strings.ToLower(m.BaseSeverity)
	case len(v.Metrics.CVSSMetricV30) > 0:
		m := v.Metrics.CVSSMetricV30[0].CVSSData
		out.CVSSScore = m.BaseScore
		out.CVSSVector = m.VectorString
		out.Severity = strings.ToLower(m.BaseSeverity)
	case len(v.Metrics.CVSSMetricV2) > 0:
		m := v.Metrics.CVSSMetricV2[0]
		out.CVSSScore = m.CVSSData.BaseScore
		out.CVSSVector = m.CVSSData.VectorString
		out.Severity = strings.ToLower(m.BaseSeverity)
	}
	if t, err := time.Parse(time.RFC3339, v.Published); err == nil {
		out.Published = t
	}
	if t, err := time.Parse(time.RFC3339, v.LastModified); err == nil {
		out.LastMod = t
	}
	if out.CVEID == "" {
		out.CVEID = cveID
	}
	return out, nil
}
