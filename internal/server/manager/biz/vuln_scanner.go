package biz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

const (
	osvBatchURL   = "https://api.osv.dev/v1/querybatch"
	osvBatchSize  = 1000 // OSV.dev 单次最多 1000 个查询
	osvTimeout    = 30 * time.Second
)

// VulnScanner 漏洞扫描器，基于 OSV.dev API
type VulnScanner struct {
	db         *gorm.DB
	httpClient *http.Client
	logger     *zap.Logger
}

// NewVulnScanner 创建漏洞扫描器
func NewVulnScanner(db *gorm.DB, logger *zap.Logger) *VulnScanner {
	return &VulnScanner{
		db: db,
		httpClient: &http.Client{
			Timeout: osvTimeout,
		},
		logger: logger,
	}
}

// osvQueryBatchRequest OSV.dev 批量查询请求
type osvQueryBatchRequest struct {
	Queries []osvQuery `json:"queries"`
}

type osvQuery struct {
	Package osvPackage `json:"package"`
}

type osvPackage struct {
	PURL string `json:"purl"`
}

// osvQueryBatchResponse OSV.dev 批量查询响应
type osvQueryBatchResponse struct {
	Results []osvQueryResult `json:"results"`
}

type osvQueryResult struct {
	Vulns []osvVuln `json:"vulns,omitempty"`
}

type osvVuln struct {
	ID       string     `json:"id"`
	Summary  string     `json:"summary"`
	Details  string     `json:"details"`
	Aliases  []string   `json:"aliases"`
	Severity []osvSeverity `json:"severity,omitempty"`
	Affected []osvAffected `json:"affected,omitempty"`
	References []osvReference `json:"references,omitempty"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvAffected struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	} `json:"package"`
	Ranges []struct {
		Type   string `json:"type"`
		Events []struct {
			Introduced string `json:"introduced,omitempty"`
			Fixed      string `json:"fixed,omitempty"`
		} `json:"events"`
	} `json:"ranges"`
}

type osvReference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// purlInfo 软件包 PURL 信息
type purlInfo struct {
	PURL     string `gorm:"column:purl"`
	Name     string `gorm:"column:name"`
	Version  string `gorm:"column:version"`
	HostID   string `gorm:"column:host_id"`
	Hostname string `gorm:"column:hostname"`
}

// GetLatestSyncStatus 查询最近一条 OSV 同步记录
func (v *VulnScanner) GetLatestSyncStatus() (*model.SecurityDBSyncRecord, error) {
	var record model.SecurityDBSyncRecord
	err := v.db.Where("db_type = ?", "osv").Order("id DESC").First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// GetSyncHistory 分页查询 OSV 同步历史记录
func (v *VulnScanner) GetSyncHistory(page, pageSize int) ([]model.SecurityDBSyncRecord, int64, error) {
	var total int64
	query := v.db.Model(&model.SecurityDBSyncRecord{}).Where("db_type = ?", "osv")
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []model.SecurityDBSyncRecord
	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("id DESC").Find(&records).Error
	return records, total, err
}

// ScanAll 全量漏洞扫描：查询所有软件包 PURL → OSV.dev API → 写入漏洞表
func (v *VulnScanner) ScanAll() error {
	startedAt := time.Now()

	// 插入 running 记录
	record := model.SecurityDBSyncRecord{
		DBType:    "osv",
		Status:    "running",
		StartedAt: startedAt,
	}
	v.db.Create(&record)

	v.logger.Info("开始全量漏洞扫描")

	err := v.doScanAll()
	duration := int(time.Since(startedAt).Seconds())

	updates := map[string]interface{}{"duration": duration}
	if err != nil {
		updates["status"] = "failed"
		updates["error_msg"] = err.Error()
		v.db.Model(&record).Updates(updates)
		return err
	}

	updates["status"] = "success"
	updates["version"] = time.Now().Format("20060102.150405")
	v.db.Model(&record).Updates(updates)
	return nil
}

// doScanAll 实际执行扫描逻辑
func (v *VulnScanner) doScanAll() error {
	// 1. 查询所有有 PURL 的软件包（JOIN hosts 带上 hostname / ip 用于填充 host_vulnerabilities）
	var packages []purlInfo
	if err := v.db.Table("software AS s").
		Select("s.purl AS purl, s.name AS name, s.version AS version, s.host_id AS host_id, COALESCE(h.hostname, '') AS hostname").
		Joins("LEFT JOIN hosts h ON h.host_id = s.host_id").
		Where("s.purl != '' AND s.purl IS NOT NULL").
		Scan(&packages).Error; err != nil {
		return fmt.Errorf("查询软件包 PURL 失败: %w", err)
	}

	if len(packages) == 0 {
		v.logger.Info("没有找到带 PURL 的软件包")
		return nil
	}

	v.logger.Info("查询到软件包", zap.Int("count", len(packages)))

	// 2. 按 PURL 去重，记录每个 PURL 对应的主机列表 + 主机名映射
	purlHosts := make(map[string][]string)   // purl → []hostID
	purlPkgInfo := make(map[string]purlInfo) // purl → 包信息
	hostnameMap := make(map[string]string)   // hostID → hostname
	for _, pkg := range packages {
		purlHosts[pkg.PURL] = append(purlHosts[pkg.PURL], pkg.HostID)
		if _, exists := purlPkgInfo[pkg.PURL]; !exists {
			purlPkgInfo[pkg.PURL] = pkg
		}
		if pkg.Hostname != "" {
			hostnameMap[pkg.HostID] = pkg.Hostname
		}
	}

	// 3. 构建去重后的 PURL 列表
	uniquePURLs := make([]string, 0, len(purlHosts))
	for purl := range purlHosts {
		uniquePURLs = append(uniquePURLs, purl)
	}

	v.logger.Info("去重后 PURL 数", zap.Int("count", len(uniquePURLs)))

	// 4. 分批调用 OSV.dev API
	totalVulns := 0
	for i := 0; i < len(uniquePURLs); i += osvBatchSize {
		end := i + osvBatchSize
		if end > len(uniquePURLs) {
			end = len(uniquePURLs)
		}
		batch := uniquePURLs[i:end]

		vulnCount, err := v.queryBatch(batch, purlHosts, purlPkgInfo, hostnameMap)
		if err != nil {
			v.logger.Error("OSV.dev 批量查询失败",
				zap.Int("batch_start", i),
				zap.Error(err))
			continue
		}
		totalVulns += vulnCount
	}

	v.logger.Info("全量漏洞扫描完成",
		zap.Int("total_purls", len(uniquePURLs)),
		zap.Int("total_vulns", totalVulns))

	return nil
}

// queryBatch 批量查询 OSV.dev 并写入数据库
func (v *VulnScanner) queryBatch(purls []string, purlHosts map[string][]string, purlPkgInfo map[string]purlInfo, hostnameMap map[string]string) (int, error) {
	// 构建请求
	req := osvQueryBatchRequest{
		Queries: make([]osvQuery, len(purls)),
	}
	for i, purl := range purls {
		req.Queries[i] = osvQuery{Package: osvPackage{PURL: purl}}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 调用 API
	resp, err := v.httpClient.Post(osvBatchURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("调用 OSV.dev API 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("OSV.dev API 返回 %d: %s", resp.StatusCode, string(respBody))
	}

	var result osvQueryBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("解析 OSV.dev 响应失败: %w", err)
	}

	// 处理结果
	vulnCount := 0
	for i, qr := range result.Results {
		if i >= len(purls) {
			break
		}
		purl := purls[i]

		for _, vuln := range qr.Vulns {
			cveID := v.extractCVE(vuln)
			if cveID == "" {
				continue
			}

			pkgInfo := purlPkgInfo[purl]
			severity := v.mapSeverity(vuln)
			cvssScore := v.extractCVSS(vuln)
			fixedVersion := v.extractFixedVersion(vuln)
			referenceURL := v.extractReferenceURL(vuln)

			// Upsert 漏洞记录
			vulnRecord := &model.Vulnerability{
				CveID:          cveID,
				OsvID:          vuln.ID,
				PURL:           purl,
				Severity:       severity,
				CvssScore:      cvssScore,
				Component:      pkgInfo.Name,
				Description:    vuln.Summary,
				Status:         "unpatched",
				DiscoveredAt:   model.LocalTime(time.Now()),
				CurrentVersion: pkgInfo.Version,
				FixedVersion:   fixedVersion,
				ReferenceUrl:   referenceURL,
			}

			if err := v.db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "cve_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"osv_id", "purl", "cvss_score", "description", "fixed_version", "reference_url"}),
			}).Create(vulnRecord).Error; err != nil {
				v.logger.Error("写入漏洞记录失败",
					zap.String("cve_id", cveID),
					zap.Error(err))
				continue
			}

			// 为每个受影响的主机创建关联（同 host_id 去重，避免同一主机同 PURL 重复计数）
			seen := make(map[string]struct{})
			for _, hostID := range purlHosts[purl] {
				if _, ok := seen[hostID]; ok {
					continue
				}
				seen[hostID] = struct{}{}

				hostVuln := &model.HostVulnerability{
					VulnID:         vulnRecord.ID,
					HostID:         hostID,
					Hostname:       hostnameMap[hostID],
					CurrentVersion: pkgInfo.Version,
					Status:         "unpatched",
				}
				v.db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "vuln_id"}, {Name: "host_id"}},
					DoUpdates: clause.AssignmentColumns([]string{"hostname", "current_version"}),
				}).Create(hostVuln)
			}

			// 更新受影响主机数
			var affectedCount int64
			v.db.Model(&model.HostVulnerability{}).
				Where("vuln_id = ? AND status = ?", vulnRecord.ID, "unpatched").
				Count(&affectedCount)
			v.db.Model(vulnRecord).Update("affected_hosts", affectedCount)

			// 异步发送漏洞告警通知（仅新发现的漏洞，取第一个主机作为通知目标）
			if len(purlHosts[purl]) > 0 {
				firstHost := purlHosts[purl][0]
				go func(vuln *model.Vulnerability, hostID, hostname string, affected int64) {
					// 查询主机 IP
					var host model.Host
					ip := ""
					if v.db.Select("ipv4").First(&host, "host_id = ?", hostID).Error == nil && len(host.IPv4) > 0 {
						ip = host.IPv4[0]
					}
					ns := NewNotificationService(v.db, v.logger)
					if err := ns.SendVulnerabilityAlertNotification(&VulnerabilityAlertData{
						HostID:         hostID,
						Hostname:       hostname,
						IP:             ip,
						CveID:          vuln.CveID,
						Severity:       vuln.Severity,
						CvssScore:      vuln.CvssScore,
						Component:      vuln.Component,
						CurrentVersion: vuln.CurrentVersion,
						FixedVersion:   vuln.FixedVersion,
						Description:    vuln.Description,
						AffectedHosts:  int(affected),
					}); err != nil {
						v.logger.Error("发送漏洞告警通知失败", zap.String("cve_id", vuln.CveID), zap.Error(err))
					}
				}(vulnRecord, firstHost, hostnameMap[firstHost], affectedCount)
			}

			vulnCount++
		}
	}

	return vulnCount, nil
}

// extractCVE 从 OSV 漏洞中提取 CVE ID
func (v *VulnScanner) extractCVE(vuln osvVuln) string {
	// 先检查 ID 是否就是 CVE
	if strings.HasPrefix(vuln.ID, "CVE-") {
		return vuln.ID
	}
	// 检查 aliases
	for _, alias := range vuln.Aliases {
		if strings.HasPrefix(alias, "CVE-") {
			return alias
		}
	}
	// 没有 CVE ID，使用 OSV ID
	return vuln.ID
}

// mapSeverity 映射严重级别
func (v *VulnScanner) mapSeverity(vuln osvVuln) string {
	cvss := v.extractCVSS(vuln)
	switch {
	case cvss >= 9.0:
		return "critical"
	case cvss >= 7.0:
		return "high"
	case cvss >= 4.0:
		return "medium"
	case cvss > 0:
		return "low"
	default:
		return "medium"
	}
}

// extractCVSS 从 CVSS v3.x 向量字符串计算基础分数
// 向量格式: CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H
func (v *VulnScanner) extractCVSS(vuln osvVuln) float64 {
	for _, sev := range vuln.Severity {
		if sev.Type == "CVSS_V3" {
			score := parseCVSSv3Vector(sev.Score)
			if score > 0 {
				return score
			}
		}
	}
	return 0
}

// parseCVSSv3Vector 解析 CVSS v3.x 向量字符串，返回 Base Score
func parseCVSSv3Vector(vector string) float64 {
	// 解析各指标
	metrics := make(map[string]string)
	parts := strings.Split(vector, "/")
	for _, part := range parts {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) == 2 {
			metrics[kv[0]] = kv[1]
		}
	}

	// Attack Vector
	av := map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.20}
	// Attack Complexity
	ac := map[string]float64{"L": 0.77, "H": 0.44}
	// User Interaction
	ui := map[string]float64{"N": 0.85, "R": 0.62}
	// Confidentiality, Integrity, Availability Impact
	cia := map[string]float64{"H": 0.56, "L": 0.22, "N": 0}
	// Privileges Required (Scope Unchanged / Changed)
	prU := map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	prC := map[string]float64{"N": 0.85, "L": 0.68, "H": 0.50}

	avVal, ok1 := av[metrics["AV"]]
	acVal, ok2 := ac[metrics["AC"]]
	uiVal, ok3 := ui[metrics["UI"]]
	cVal, ok4 := cia[metrics["C"]]
	iVal, ok5 := cia[metrics["I"]]
	aVal, ok6 := cia[metrics["A"]]
	scopeChanged := metrics["S"] == "C"

	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return 0
	}

	var prVal float64
	if scopeChanged {
		prVal = prC[metrics["PR"]]
	} else {
		prVal = prU[metrics["PR"]]
	}

	// ISS (Impact Sub Score)
	iss := 1 - (1-cVal)*(1-iVal)*(1-aVal)

	// Impact
	var impact float64
	if scopeChanged {
		impact = 7.52*(iss-0.029) - 3.25*pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}

	if impact <= 0 {
		return 0
	}

	// Exploitability
	exploitability := 8.22 * avVal * acVal * prVal * uiVal

	// Base Score
	var base float64
	if scopeChanged {
		base = 1.08 * (impact + exploitability)
	} else {
		base = impact + exploitability
	}

	if base > 10.0 {
		base = 10.0
	}

	// Round up to nearest 0.1
	return roundUp(base)
}

// pow 简单幂运算
func pow(base float64, exp int) float64 {
	result := 1.0
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}

// roundUp 向上取整到 0.1
func roundUp(val float64) float64 {
	// 乘以 10，取 ceil，再除以 10
	scaled := val * 10
	truncated := float64(int(scaled))
	if scaled > truncated {
		return (truncated + 1) / 10
	}
	return truncated / 10
}

// extractFixedVersion 提取修复版本
func (v *VulnScanner) extractFixedVersion(vuln osvVuln) string {
	for _, affected := range vuln.Affected {
		for _, r := range affected.Ranges {
			for _, event := range r.Events {
				if event.Fixed != "" {
					return event.Fixed
				}
			}
		}
	}
	return ""
}

// extractReferenceURL 提取参考链接
func (v *VulnScanner) extractReferenceURL(vuln osvVuln) string {
	for _, ref := range vuln.References {
		if ref.Type == "ADVISORY" || ref.Type == "WEB" {
			return ref.URL
		}
	}
	if len(vuln.References) > 0 {
		return vuln.References[0].URL
	}
	return ""
}
