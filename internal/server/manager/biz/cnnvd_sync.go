package biz

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	// CVE→CNNVD 映射表（开源 CSV 数据源）
	cnnvdMappingURL = "https://raw.githubusercontent.com/cnnvd-data/cve2cnnvd/main/cve2cnnvd.csv"
)

// SyncCNNVD 通过 CVE→CNNVD 映射表补齐 CNNVD 编号
// 先用映射表方案快速补齐，后续接入完整 CNNVD 数据源
func (v *VulnScanner) SyncCNNVD() error {
	v.logger.Info("开始 CNNVD 编号补齐")

	// 从系统配置读取自定义映射表地址
	mappingURL := cnnvdMappingURL
	var configURL string
	if err := v.db.Table("system_configs").
		Select("value").
		Where("`key` = ?", "cnnvd_mapping_url").
		Scan(&configURL).Error; err == nil && configURL != "" {
		mappingURL = configURL
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(mappingURL)
	if err != nil {
		v.logger.Warn("CNNVD 映射表下载失败，跳过同步", zap.Error(err))
		return fmt.Errorf("CNNVD 映射表下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CNNVD 映射表响应状态码: %d", resp.StatusCode)
	}

	// 解析 CSV 映射表
	// 预期格式：CVE-ID,CNNVD-ID 或 cve_id,cnnvd_id
	reader := csv.NewReader(resp.Body)

	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("读取 CNNVD 映射表头失败: %w", err)
	}

	// 找到 CVE 和 CNNVD 列索引
	cveIdx, cnnvdIdx := -1, -1
	for i, col := range header {
		col = strings.ToLower(strings.TrimSpace(col))
		switch {
		case strings.Contains(col, "cve"):
			cveIdx = i
		case strings.Contains(col, "cnnvd"):
			cnnvdIdx = i
		}
	}
	if cveIdx == -1 || cnnvdIdx == -1 {
		return fmt.Errorf("CNNVD 映射表格式异常，找不到 CVE/CNNVD 列")
	}

	// 收集映射关系
	mapping := make(map[string]string) // CVE → CNNVD
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if cveIdx >= len(record) || cnnvdIdx >= len(record) {
			continue
		}

		cveID := strings.TrimSpace(record[cveIdx])
		cnnvdID := strings.TrimSpace(record[cnnvdIdx])
		if cveID != "" && cnnvdID != "" && strings.HasPrefix(cveID, "CVE-") {
			mapping[cveID] = cnnvdID
		}
	}

	v.logger.Info("CNNVD 映射表解析完成", zap.Int("mappings", len(mapping)))

	if len(mapping) == 0 {
		return nil
	}

	// 查询需要补齐 CNNVD 编号的漏洞 CVE ID
	var cveIDs []string
	v.db.Table("vulnerabilities").
		Select("cve_id").
		Where("cve_id != '' AND (cnnvd_id IS NULL OR cnnvd_id = '')").
		Pluck("cve_id", &cveIDs)

	totalUpdated := 0
	for _, cveID := range cveIDs {
		cnnvdID, ok := mapping[cveID]
		if !ok {
			continue
		}
		result := v.db.Table("vulnerabilities").
			Where("cve_id = ?", cveID).
			Update("cnnvd_id", cnnvdID)
		if result.RowsAffected > 0 {
			totalUpdated++
		}
	}

	v.logger.Info("CNNVD 编号补齐完成", zap.Int("updated", totalUpdated))
	return nil
}
