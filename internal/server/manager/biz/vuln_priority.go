package biz

import (
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 优先级评分权重（默认值）
const (
	defaultWeightCVSS     = 0.35
	defaultWeightExploit  = 0.30
	defaultWeightExposure = 0.20
	defaultWeightPatch    = 0.15
)

// PriorityCalculator 漏洞优先级计算器
type PriorityCalculator struct {
	db     *gorm.DB
	logger *zap.Logger
	// 权重可配置
	WeightCVSS     float64
	WeightExploit  float64
	WeightExposure float64
	WeightPatch    float64
}

// NewPriorityCalculator 创建优先级计算器
func NewPriorityCalculator(db *gorm.DB, logger *zap.Logger) *PriorityCalculator {
	return &PriorityCalculator{
		db:             db,
		logger:         logger,
		WeightCVSS:     defaultWeightCVSS,
		WeightExploit:  defaultWeightExploit,
		WeightExposure: defaultWeightExposure,
		WeightPatch:    defaultWeightPatch,
	}
}

// RecalculateAll 重新计算所有 unpatched 漏洞的优先级评分
func (p *PriorityCalculator) RecalculateAll() error {
	p.logger.Info("开始批量重算漏洞优先级")

	// 查询总主机数（用于暴露面计算）
	var totalHosts int64
	if err := p.db.Table("hosts").Where("status = ?", "online").Count(&totalHosts).Error; err != nil {
		return fmt.Errorf("查询主机总数失败: %w", err)
	}
	if totalHosts == 0 {
		totalHosts = 1 // 避免除零
	}

	// 查询所有 unpatched 漏洞的 ID
	var vulnIDs []uint
	if err := p.db.Table("vulnerabilities").
		Select("id").
		Where("status = ?", "unpatched").
		Pluck("id", &vulnIDs).Error; err != nil {
		return fmt.Errorf("查询未修复漏洞失败: %w", err)
	}

	p.logger.Info("需要计算优先级的漏洞数", zap.Int("count", len(vulnIDs)))

	// 分批处理
	batchSize := 100
	updated := 0
	for i := 0; i < len(vulnIDs); i += batchSize {
		end := min(i+batchSize, len(vulnIDs))
		batch := vulnIDs[i:end]

		if err := p.recalculateBatch(batch, totalHosts); err != nil {
			p.logger.Warn("批量计算优先级失败", zap.Error(err))
		} else {
			updated += len(batch)
		}
	}

	p.logger.Info("漏洞优先级计算完成", zap.Int("updated", updated))
	return nil
}

// vulnScoreRow 用于批量查询漏洞评分所需数据
type vulnScoreRow struct {
	ID            uint    `gorm:"column:id"`
	CvssScore     float64 `gorm:"column:cvss_score"`
	HasExploit    bool    `gorm:"column:has_exploit"`
	InKEV         bool    `gorm:"column:in_kev"`
	AffectedHosts int     `gorm:"column:affected_hosts"`
	FixedVersion  string  `gorm:"column:fixed_version"`
}

// recalculateBatch 批量计算一批漏洞的优先级
func (p *PriorityCalculator) recalculateBatch(vulnIDs []uint, totalHosts int64) error {
	var rows []vulnScoreRow
	if err := p.db.Table("vulnerabilities").
		Select("id, cvss_score, has_exploit, in_kev, affected_hosts, fixed_version").
		Where("id IN ?", vulnIDs).
		Scan(&rows).Error; err != nil {
		return err
	}

	// 批量查询是否有公网暴露主机
	internetFacingMap := p.queryInternetFacing(vulnIDs)

	for _, row := range rows {
		// 1. CVSS 归一化（0~1）
		cvssNorm := row.CvssScore / 10.0

		// 2. 利用状态评分
		var exploitScore float64
		if row.InKEV {
			exploitScore = 1.0
		} else if row.HasExploit {
			exploitScore = 0.7
		}

		// 3. 暴露面评分
		hostRatio := float64(row.AffectedHosts) / float64(totalHosts)
		if hostRatio > 1.0 {
			hostRatio = 1.0
		}
		internetFacing := 0.0
		if internetFacingMap[row.ID] {
			internetFacing = 1.0
		}
		exposureScore := hostRatio*0.5 + internetFacing*0.5

		// 4. 补丁可用性评分（有补丁 = 可行动 = 优先级更高）
		patchScore := 0.2
		if row.FixedVersion != "" {
			patchScore = 0.8
		}

		// 综合优先级分
		priorityScore := p.WeightCVSS*cvssNorm +
			p.WeightExploit*exploitScore +
			p.WeightExposure*exposureScore +
			p.WeightPatch*patchScore

		// 更新数据库
		p.db.Table("vulnerabilities").Where("id = ?", row.ID).
			Updates(map[string]any{
				"priority_score": priorityScore,
				"exposure_score": exposureScore,
			})
	}

	return nil
}

// RecalculateOne 计算单个漏洞的优先级评分
func (p *PriorityCalculator) RecalculateOne(vulnID uint) (float64, error) {
	var totalHosts int64
	if err := p.db.Table("hosts").Where("status = ?", "online").Count(&totalHosts).Error; err != nil {
		return 0, fmt.Errorf("查询主机总数失败: %w", err)
	}
	if totalHosts == 0 {
		totalHosts = 1
	}

	if err := p.recalculateBatch([]uint{vulnID}, totalHosts); err != nil {
		return 0, err
	}

	var score float64
	p.db.Table("vulnerabilities").Select("priority_score").Where("id = ?", vulnID).Scan(&score)
	return score, nil
}

// queryInternetFacing 查询漏洞关联的主机是否有公网暴露
func (p *PriorityCalculator) queryInternetFacing(vulnIDs []uint) map[uint]bool {
	result := make(map[uint]bool)

	// 查询受影响主机中是否有对外端口的
	type row struct {
		VulnID uint `gorm:"column:vuln_id"`
	}
	var rows []row

	p.db.Raw(`
		SELECT DISTINCT hv.vuln_id
		FROM host_vulnerabilities hv
		JOIN ports p ON p.host_id = hv.host_id
		WHERE hv.vuln_id IN ?
		  AND hv.status = 'unpatched'
		  AND p.listen_address NOT IN ('127.0.0.1', '::1', '0.0.0.0', '::')
	`, vulnIDs).Scan(&rows)

	for _, r := range rows {
		result[r.VulnID] = true
	}
	return result
}
