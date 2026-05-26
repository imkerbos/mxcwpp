package advisory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RedHatSource 拉取 Red Hat Security Data API 的 RHSA。
// 用于 RHEL 9 + Rocky 9（binary-compatible）+ CentOS Stream 9。
//
// API: https://access.redhat.com/hydra/rest/securitydata/cvrf.json?after=YYYY-MM-DD
// 返回 OVAL/CVRF 列表，每条 advisory 含 fixed pkg 列表（OS-specific 版本）。
type RedHatSource struct {
	client  *http.Client
	baseURL string // 注入便于测试 mock
}

// NewRedHatSource 构造默认配置的 RedHatSource。
func NewRedHatSource() *RedHatSource {
	return &RedHatSource{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: "https://access.redhat.com/hydra/rest/securitydata",
	}
}

// WithBaseURL 测试用：注入 mock server URL。
func (r *RedHatSource) WithBaseURL(url string) *RedHatSource {
	r.baseURL = url
	return r
}

// WithHTTPClient 测试用：注入定制 client。
func (r *RedHatSource) WithHTTPClient(c *http.Client) *RedHatSource {
	r.client = c
	return r
}

// Name 实现 Source。
func (r *RedHatSource) Name() string { return "rhsa" }

// Confidence 实现 Source：RHSA 是 OS 厂商权威，high。
func (r *RedHatSource) Confidence() Confidence { return ConfidenceHigh }

// rhCVRFListItem 是 CVRF 列表 API 的单条响应。
type rhCVRFListItem struct {
	RHSA         string   `json:"RHSA"`
	Severity     string   `json:"severity"`
	ReleasedOn   string   `json:"released_on"`
	CVEs         []string `json:"CVEs"`
	BugzillaURL  string   `json:"bugzilla_url"`
	ResourceURL  string   `json:"resource_url"`
	OSPaltforms  []string `json:"OS_platforms"`
}

// rhCVRFDetail 是 CVRF 详情 API 的响应。
type rhCVRFDetail struct {
	Document   rhCVRFDocument   `json:"cvrfdoc"`
	Vulns      []rhCVRFVuln     `json:"vulnerabilities"`
	ProductTree rhCVRFProductTree `json:"product_tree"`
}

type rhCVRFDocument struct {
	Title           string `json:"document_title"`
	Tracking        struct {
		ID              string `json:"identifier"`
		InitialReleaseDate string `json:"initial_release_date"`
		CurrentReleaseDate string `json:"current_release_date"`
	} `json:"document_tracking"`
}

type rhCVRFVuln struct {
	CVE              string                 `json:"cve"`
	CVSS3            rhCVRFCVSS3            `json:"cvss3"`
	Notes            []rhCVRFNote           `json:"notes"`
	ProductStatuses  []rhCVRFProductStatus  `json:"product_statuses"`
	Remediations     []rhCVRFRemediation    `json:"remediations"`
}

type rhCVRFCVSS3 struct {
	BaseScore     float64 `json:"base_score"`
	VectorV3      string  `json:"vector_v_3"`
	CVSS3ScoreSet struct {
		BaseScore string `json:"base_score"`
		Vector    string `json:"vector"`
	} `json:"cvss3_score_set"`
}

type rhCVRFNote struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type rhCVRFProductStatus struct {
	Type      string   `json:"type"`
	ProductID []string `json:"product_id"`
}

type rhCVRFRemediation struct {
	Type      string   `json:"type"`
	ProductID []string `json:"product_id"`
}

type rhCVRFProductTree struct {
	Branches []rhCVRFBranch `json:"branches"`
}

type rhCVRFBranch struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Branches []rhCVRFBranch `json:"branches"`
	Product  *rhCVRFProduct `json:"full_product_name"`
}

type rhCVRFProduct struct {
	ProductID string `json:"product_id"`
	Name      string `json:"text"`
}

// Fetch 实现 Source。
func (r *RedHatSource) Fetch(ctx context.Context, since time.Time) ([]*Advisory, error) {
	listURL := fmt.Sprintf("%s/cvrf.json", r.baseURL)
	if !since.IsZero() {
		listURL += "?after=" + since.Format("2006-01-02")
	}

	items, err := r.fetchList(ctx, listURL)
	if err != nil {
		return nil, fmt.Errorf("RHSA 列表拉取失败: %w", err)
	}

	advisories := make([]*Advisory, 0, len(items))
	for _, item := range items {
		detail, err := r.fetchDetail(ctx, item.RHSA)
		if err != nil {
			// 单条 advisory 失败不阻断全量，记录跳过
			continue
		}
		adv := r.parseDetail(item, detail)
		if adv == nil {
			continue
		}
		advisories = append(advisories, adv)
	}
	return advisories, nil
}

func (r *RedHatSource) fetchList(ctx context.Context, url string) ([]rhCVRFListItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("RHSA list HTTP %d: %s", resp.StatusCode, string(body))
	}
	var items []rhCVRFListItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("RHSA list decode: %w", err)
	}
	return items, nil
}

func (r *RedHatSource) fetchDetail(ctx context.Context, rhsaID string) (*rhCVRFDetail, error) {
	url := fmt.Sprintf("%s/cvrf/%s.json", r.baseURL, rhsaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RHSA detail HTTP %d", resp.StatusCode)
	}
	var detail rhCVRFDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("RHSA detail decode: %w", err)
	}
	return &detail, nil
}

// parseDetail 将 RHSA detail 解析成统一 Advisory。
// product_id 形如 "Red Hat Enterprise Linux 9:openssl-0:3.5.5-1.el9_4.src"
// 提取 OS 版本 + pkg 名 + arch + fixed version。
func (r *RedHatSource) parseDetail(item rhCVRFListItem, detail *rhCVRFDetail) *Advisory {
	if detail == nil {
		return nil
	}

	issuedAt, _ := time.Parse(time.RFC3339, detail.Document.Tracking.InitialReleaseDate)
	updatedAt, _ := time.Parse(time.RFC3339, detail.Document.Tracking.CurrentReleaseDate)

	// 提取 description (取第一个 General 类型 note)
	var description string
	for _, vuln := range detail.Vulns {
		for _, note := range vuln.Notes {
			if note.Type == "General" || note.Type == "Description" {
				description = note.Text
				break
			}
		}
		if description != "" {
			break
		}
	}

	// 提取 CVSS（取最高分）
	var cvssScore float64
	var cvssVector string
	for _, vuln := range detail.Vulns {
		if vuln.CVSS3.BaseScore > cvssScore {
			cvssScore = vuln.CVSS3.BaseScore
			cvssVector = vuln.CVSS3.VectorV3
		}
	}

	// 解析受影响包：从 remediations.product_id 提取
	pkgFixes := parseRHSAProducts(detail)

	// 主 OS：从 product_tree 提取（取第一个匹配的 RHEL 主版本）
	osMajor := extractRHELMajorVersion(detail.ProductTree)

	return &Advisory{
		AdvisoryID:   item.RHSA,
		CVEIDs:       dedupStrings(item.CVEs),
		Severity:     normalizeRHSeverity(item.Severity),
		CVSSScore:    cvssScore,
		CVSSVector:   cvssVector,
		Description:  description,
		ReferenceURL: item.ResourceURL,
		IssuedAt:     issuedAt,
		UpdatedAt:    updatedAt,
		AffectedPkgs: pkgFixes,
		OSFamily:     "rhel",
		OSMajorVer:   osMajor,
	}
}

// parseRHSAProducts 从 CVRF detail 提取 (pkg_name, arch, fixed_version) 列表。
// product_id 编码：{product_name}:{pkg}-{epoch}:{version}-{release}.{arch}
// 例: "Red Hat Enterprise Linux 9:openssl-1:3.5.5-1.el9_4.src"
func parseRHSAProducts(detail *rhCVRFDetail) []PkgFix {
	seen := make(map[string]struct{})
	var fixes []PkgFix

	for _, vuln := range detail.Vulns {
		for _, rem := range vuln.Remediations {
			if rem.Type != "Vendor Fix" {
				continue
			}
			for _, pid := range rem.ProductID {
				fix := parseProductID(pid)
				if fix == nil {
					continue
				}
				key := fix.Name + "|" + fix.Arch + "|" + fix.FixedVersion
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				fixes = append(fixes, *fix)
			}
		}
	}
	return fixes
}

// parseProductID 解析单个 product_id 字符串。
// 输入: "Red Hat Enterprise Linux 9:openssl-1:3.5.5-1.el9_4.src"
// 输出: PkgFix{Name:"openssl", Arch:"src", FixedVersion:"1:3.5.5-1.el9_4"}
func parseProductID(pid string) *PkgFix {
	colon := strings.Index(pid, ":")
	if colon < 0 {
		return nil
	}
	pkgPart := pid[colon+1:]

	// arch 是最后的 . 后缀（src/x86_64/aarch64/noarch）
	lastDot := strings.LastIndex(pkgPart, ".")
	if lastDot < 0 {
		return nil
	}
	arch := pkgPart[lastDot+1:]
	if !isValidRPMArch(arch) {
		return nil
	}
	nameVer := pkgPart[:lastDot]

	// 拆 name vs version：从右往左找 - 后跟数字的位置
	// 如 "openssl-1:3.5.5-1.el9_4" → name="openssl" version="1:3.5.5-1.el9_4"
	dashIdx := findRPMVersionDash(nameVer)
	if dashIdx < 0 {
		return nil
	}
	return &PkgFix{
		Name:         nameVer[:dashIdx],
		Arch:         arch,
		FixedVersion: nameVer[dashIdx+1:],
	}
}

// findRPMVersionDash 找 NAME-VERSION 分隔符（第一个 dash 后紧跟数字或 epoch）。
func findRPMVersionDash(s string) int {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '-' {
			next := s[i+1]
			// 数字 或 epoch格式（如 "1:" 开头数字）
			if (next >= '0' && next <= '9') ||
				(i+2 < len(s) && next >= '0' && next <= '9') {
				return i
			}
		}
	}
	return -1
}

func isValidRPMArch(s string) bool {
	switch s {
	case "src", "x86_64", "aarch64", "noarch", "i686", "ppc64le", "s390x":
		return true
	}
	return false
}

// extractRHELMajorVersion 从 product_tree 提取 RHEL 主版本号（单一最常出现的）。
func extractRHELMajorVersion(tree rhCVRFProductTree) string {
	counts := map[string]int{}
	walkBranches(tree.Branches, func(name string) {
		// "Red Hat Enterprise Linux 9" / "Red Hat Enterprise Linux Server 8"
		idx := strings.LastIndex(name, " ")
		if idx < 0 {
			return
		}
		ver := name[idx+1:]
		if len(ver) > 0 && ver[0] >= '0' && ver[0] <= '9' {
			counts[ver]++
		}
	})
	var topVer string
	var topCount int
	for v, c := range counts {
		if c > topCount {
			topCount = c
			topVer = v
		}
	}
	return topVer
}

func walkBranches(branches []rhCVRFBranch, visit func(name string)) {
	for _, b := range branches {
		if b.Type == "Product Family" || b.Type == "Product Name" {
			visit(b.Name)
		}
		if len(b.Branches) > 0 {
			walkBranches(b.Branches, visit)
		}
	}
}

func normalizeRHSeverity(s string) Severity {
	switch strings.ToLower(s) {
	case "critical":
		return SeverityCritical
	case "important":
		return SeverityHigh
	case "moderate":
		return SeverityMedium
	case "low":
		return SeverityLow
	}
	return SeverityNone
}

func dedupStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
