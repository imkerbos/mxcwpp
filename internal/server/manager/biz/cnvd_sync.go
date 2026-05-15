package biz

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

const (
	cnvdSyncDays = 14
	cnvdPageSize = 100
)

// cnvdVulnItem CNVD 漏洞条目
type cnvdVulnItem struct {
	CnvdID          string `json:"cnvdId"` // CNVD-YYYY-NNNNN
	Title           string `json:"title"`
	Severity        string `json:"severity"` // 超危/高危/中危/低危
	CveID           string `json:"cveId"`    // 对应的 CVE 编号（可能为空）
	PublishedDate   string `json:"publishedDate"`
	AffectedProduct string `json:"affectedProduct"`
	Description     string `json:"description"`
}

type cnvdAPIResponse struct {
	Total int            `json:"total"`
	Data  []cnvdVulnItem `json:"data"`
}

// SyncCNVD 从 CNVD 数据源同步漏洞信息
// CNVD（cnvd.org.cn）不提供公开 API，需要用户在系统配置中填入第三方数据源地址。
// 可选方案：Vulners API（需 API Key）、腾讯云漏洞知识库 API、自建爬虫等。
func (v *VulnScanner) SyncCNVD() error {
	// 从系统配置读取 API 地址
	var apiURL string
	if err := v.db.Table("system_configs").
		Select("value").
		Where("`key` = ?", "cnvd_api_url").
		Scan(&apiURL).Error; err != nil || apiURL == "" {
		v.logger.Info("CNVD 数据源未配置，跳过同步（CNVD 无公开 API，请在系统设置中配置第三方数据源地址）")
		return nil
	}

	v.logger.Info("开始 CNVD 数据源同步", zap.String("api", apiURL))

	client := &http.Client{Timeout: 30 * time.Second}
	since := time.Now().AddDate(0, 0, -cnvdSyncDays).Format("2006-01-02")

	totalSynced := 0
	page := 1

	for {
		reqURL := fmt.Sprintf("%s?since=%s&page=%d&pageSize=%d", apiURL, since, page, cnvdPageSize)
		resp, err := client.Get(reqURL)
		if err != nil {
			return fmt.Errorf("CNVD API 请求失败: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("读取 CNVD 响应失败: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("CNVD API 响应状态码: %d", resp.StatusCode)
		}

		var apiResp cnvdAPIResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return fmt.Errorf("解析 CNVD 响应失败: %w", err)
		}

		if len(apiResp.Data) == 0 {
			break
		}

		for _, item := range apiResp.Data {
			if item.CnvdID == "" {
				continue
			}

			if item.CveID != "" {
				result := v.db.Table("vulnerabilities").
					Where("cve_id = ? AND (cnvd_id IS NULL OR cnvd_id = '')", item.CveID).
					Update("cnvd_id", item.CnvdID)
				if result.RowsAffected > 0 {
					totalSynced++
				}
			}
		}

		if len(apiResp.Data) < cnvdPageSize {
			break
		}
		page++
	}

	v.logger.Info("CNVD 同步完成", zap.Int("synced", totalSynced))
	return nil
}
