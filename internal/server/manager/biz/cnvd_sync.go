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
	// CNVD 数据源 API（第三方聚合，实际部署时通过配置文件覆盖）
	cnvdDefaultAPI = "https://api.cnvd-data.example.com/v1/vulnerabilities"
	cnvdSyncDays   = 14
	cnvdPageSize   = 100
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
// 策略：优先通过 CVE ID 关联已有漏洞，补充 cnvd_id 字段
// 无 CVE 映射的 CNVD 漏洞单独入库
func (v *VulnScanner) SyncCNVD() error {
	v.logger.Info("开始 CNVD 数据源同步")

	// 从系统配置读取 API 地址（支持自定义数据源）
	apiURL := cnvdDefaultAPI
	var configURL string
	if err := v.db.Table("system_configs").
		Select("value").
		Where("`key` = ?", "cnvd_api_url").
		Scan(&configURL).Error; err == nil && configURL != "" {
		apiURL = configURL
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// 计算同步起始日期
	since := time.Now().AddDate(0, 0, -cnvdSyncDays).Format("2006-01-02")

	totalSynced := 0
	page := 1

	for {
		reqURL := fmt.Sprintf("%s?since=%s&page=%d&pageSize=%d", apiURL, since, page, cnvdPageSize)
		resp, err := client.Get(reqURL)
		if err != nil {
			v.logger.Warn("CNVD API 请求失败，跳过同步", zap.Error(err))
			return fmt.Errorf("CNVD API 请求失败: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("读取 CNVD 响应失败: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			v.logger.Warn("CNVD API 响应异常", zap.Int("statusCode", resp.StatusCode))
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
				// 有 CVE 映射 → 更新已有漏洞的 cnvd_id
				result := v.db.Table("vulnerabilities").
					Where("cve_id = ? AND (cnvd_id IS NULL OR cnvd_id = '')", item.CveID).
					Update("cnvd_id", item.CnvdID)
				if result.RowsAffected > 0 {
					totalSynced++
				}
			}
			// 无 CVE 映射的 CNVD 漏洞暂不入库，等后续确定完整数据源方案后再处理
		}

		if len(apiResp.Data) < cnvdPageSize {
			break
		}
		page++
	}

	v.logger.Info("CNVD 同步完成", zap.Int("synced", totalSynced))
	return nil
}
