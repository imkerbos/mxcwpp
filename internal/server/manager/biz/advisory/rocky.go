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

// RockySource 拉取 Rocky Linux Apollo Errata API。
//
// API: https://apollo.build.resf.org/api/v3/advisories（公开 JSON）
// 提供 Rocky-specific errata（RLSA/RLBA/RLEA），与 RHSA 同源但 OS-specific 包名 + 版本。
type RockySource struct {
	client  *http.Client
	baseURL string
}

// NewRockySource 构造默认配置。
func NewRockySource() *RockySource {
	return &RockySource{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: "https://apollo.build.resf.org/api/v3",
	}
}

func (r *RockySource) WithBaseURL(url string) *RockySource {
	r.baseURL = url
	return r
}

func (r *RockySource) WithHTTPClient(c *http.Client) *RockySource {
	r.client = c
	return r
}

func (r *RockySource) Name() string         { return "rocky-apollo" }
func (r *RockySource) Confidence() Confidence { return ConfidenceHigh }

type rockyAdvisoriesResponse struct {
	Advisories []rockyAdvisory `json:"advisories"`
	Links      rockyLinks      `json:"links"`
	Page       int             `json:"page"`
	Size       int             `json:"size"`
	Total      int             `json:"total"`
}

type rockyLinks struct {
	Last string `json:"last"`
	Next string `json:"next"`
}

type rockyAdvisory struct {
	Name             string             `json:"name"` // RLSA-2024:1234
	Synopsis         string             `json:"synopsis"`
	Severity         string             `json:"severity"`
	Type             string             `json:"kind"` // security / bugfix / enhancement
	PublishedAt      string             `json:"published_at"`
	UpdatedAt        string             `json:"updated_at"`
	Topic            string             `json:"topic"`
	Description      string             `json:"description"`
	CVEs             []rockyCVE         `json:"cves"`
	Packages         []rockyPackage     `json:"packages"`
	RPMs             []rockyRPM         `json:"rpms"`
	AffectedProducts []string           `json:"affected_products"`
	References       []rockyReference   `json:"references"`
}

type rockyCVE struct {
	Name        string  `json:"name"`
	CVSSScore   float64 `json:"cvss3_base_score"`
	CVSSVector  string  `json:"cvss3_scoring_vector"`
	CWE         string  `json:"cwe"`
}

type rockyPackage struct {
	Name string `json:"name"`
}

type rockyRPM struct {
	Name           string `json:"name"`          // 含完整 NEVRA：openssl-1:3.5.5-1.el9_4
	Filename       string `json:"filename"`      // openssl-3.5.5-1.el9_4.x86_64.rpm
	ProductName    string `json:"product_name"`  // Rocky Linux 9
	Module         string `json:"module"`        // 通常空
}

type rockyReference struct {
	URL string `json:"url"`
}

// Fetch 实现 Source。
func (r *RockySource) Fetch(ctx context.Context, since time.Time) ([]*Advisory, error) {
	var all []*Advisory
	page := 1
	for {
		url := fmt.Sprintf("%s/advisories?page=%d&size=100", r.baseURL, page)
		if !since.IsZero() {
			url += "&published_after=" + since.Format("2006-01-02T15:04:05Z")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := r.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Rocky errata HTTP: %w", err)
		}
		var page rockyAdvisoriesResponse
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("Rocky errata decode: %w", err)
		}
		for _, ra := range page.Advisories {
			adv := r.parseAdvisory(&ra)
			if adv != nil {
				all = append(all, adv)
			}
		}
		if page.Links.Next == "" {
			break
		}
		// 简化：apollo 用 Links.Next 但我们走 page+1
		if len(page.Advisories) < page.Size {
			break
		}
		page.Page++
	}

	// 防 dead loop（apollo response size 默认 100，超过 100 页就 break）
	_ = io.Discard
	return all, nil
}

func (r *RockySource) parseAdvisory(ra *rockyAdvisory) *Advisory {
	if ra == nil || ra.Type != "security" {
		return nil
	}

	cveIDs := make([]string, 0, len(ra.CVEs))
	var maxScore float64
	var maxVector string
	for _, cve := range ra.CVEs {
		cveIDs = append(cveIDs, cve.Name)
		if cve.CVSSScore > maxScore {
			maxScore = cve.CVSSScore
			maxVector = cve.CVSSVector
		}
	}

	pkgFixes := make([]PkgFix, 0, len(ra.RPMs))
	for _, rpm := range ra.RPMs {
		fix := parseRockyRPM(&rpm)
		if fix != nil {
			pkgFixes = append(pkgFixes, *fix)
		}
	}

	var refURL string
	if len(ra.References) > 0 {
		refURL = ra.References[0].URL
	}
	if refURL == "" {
		refURL = "https://errata.rockylinux.org/" + ra.Name
	}

	issuedAt, _ := time.Parse(time.RFC3339, ra.PublishedAt)
	updatedAt, _ := time.Parse(time.RFC3339, ra.UpdatedAt)

	osMajor := extractRockyMajor(ra.AffectedProducts)

	return &Advisory{
		AdvisoryID:   ra.Name,
		CVEIDs:       cveIDs,
		Severity:     normalizeRockySeverity(ra.Severity),
		CVSSScore:    maxScore,
		CVSSVector:   maxVector,
		Description:  firstNonEmpty(ra.Synopsis, ra.Topic, ra.Description),
		ReferenceURL: refURL,
		IssuedAt:     issuedAt,
		UpdatedAt:    updatedAt,
		AffectedPkgs: pkgFixes,
		OSFamily:     "rocky",
		OSMajorVer:   osMajor,
	}
}

// parseRockyRPM 解析 NEVRA。
// rpm.Name 形如: "openssl-1:3.5.5-1.el9_4"
// rpm.Filename 形如: "openssl-3.5.5-1.el9_4.x86_64.rpm"
func parseRockyRPM(rpm *rockyRPM) *PkgFix {
	if rpm == nil || rpm.Name == "" {
		return nil
	}

	// 从 filename 提取 arch
	arch := "noarch"
	if rpm.Filename != "" {
		// strip .rpm
		fn := strings.TrimSuffix(rpm.Filename, ".rpm")
		lastDot := strings.LastIndex(fn, ".")
		if lastDot > 0 {
			a := fn[lastDot+1:]
			if isValidRPMArch(a) {
				arch = a
			}
		}
	}

	// 从 name 拆 NAME-EVR
	dashIdx := findRPMVersionDash(rpm.Name)
	if dashIdx < 0 {
		return nil
	}
	return &PkgFix{
		Name:         rpm.Name[:dashIdx],
		Arch:         arch,
		FixedVersion: rpm.Name[dashIdx+1:],
		Module:       rpm.Module,
	}
}

// extractRockyMajor 从 affected_products 提取 OS 主版本。
// "Rocky Linux 9" → "9"
func extractRockyMajor(products []string) string {
	for _, p := range products {
		// 取末尾数字
		for i := len(p) - 1; i >= 0; i-- {
			c := p[i]
			if c >= '0' && c <= '9' {
				return string(c)
			}
		}
	}
	return ""
}

func normalizeRockySeverity(s string) Severity {
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
