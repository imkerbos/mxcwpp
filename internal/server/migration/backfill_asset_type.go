// Package migration —— BackfillAssetTypeAndFixOwner
//
// 全表回填 host_vulnerabilities.asset_type + fix_owner。
// 推导路径:
//
//	host_vuln.vuln_id → vulnerabilities.component + vuln_category
//	host_vuln.host_id + component → software.scope + source_handler
//	model.DeriveAssetType(scope, source_handler) + DeriveFixOwner(asset_type, vuln_category)
//
// 用单 SQL UPDATE + JOIN 而非 N+1 GORM Hook 走,prod 11k+ 行场景 1-2s 跑完。
package migration

import (
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BackfillAssetTypeAndFixOwner 一次性回填全表。幂等:仅在 asset_type=unknown 或空时填。
// 三段 UPDATE 覆盖主要 scope 路径,剩余无 software 关联的留 unknown(下一轮 SBOM 采集后再跑)。
func BackfillAssetTypeAndFixOwner(db *gorm.DB, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Step 1: scope=embedded → asset_type=app
	if err := db.Exec(`
UPDATE host_vulnerabilities hv
JOIN vulnerabilities v ON v.id = hv.vuln_id
JOIN software s ON s.host_id = hv.host_id AND s.name = v.component
SET hv.asset_type = 'app'
WHERE (hv.asset_type IS NULL OR hv.asset_type = '' OR hv.asset_type = 'unknown')
  AND s.scope = 'embedded'
`).Error; err != nil {
		return err
	}

	// Step 2: scope=container → asset_type=container
	if err := db.Exec(`
UPDATE host_vulnerabilities hv
JOIN vulnerabilities v ON v.id = hv.vuln_id
JOIN software s ON s.host_id = hv.host_id AND s.name = v.component
SET hv.asset_type = 'container'
WHERE (hv.asset_type IS NULL OR hv.asset_type = '' OR hv.asset_type = 'unknown')
  AND s.scope = 'container'
`).Error; err != nil {
		return err
	}

	// Step 3: scope=system + handler=rpm/dpkg/apk/pacman 或 handler 空 → asset_type=os
	// handler 空兜底:旧 collector 1.2.0- 不写 source_handler,默认按 OS 包处理
	if err := db.Exec(`
UPDATE host_vulnerabilities hv
JOIN vulnerabilities v ON v.id = hv.vuln_id
JOIN software s ON s.host_id = hv.host_id AND s.name = v.component
SET hv.asset_type = 'os'
WHERE (hv.asset_type IS NULL OR hv.asset_type = '' OR hv.asset_type = 'unknown')
  AND (s.scope = 'system' OR s.scope IS NULL OR s.scope = '')
  AND (s.source_handler IN ('rpm', 'dpkg', 'apk', 'pacman', 'portage')
       OR s.source_handler IS NULL OR s.source_handler = '')
`).Error; err != nil {
		return err
	}

	// Step 4: scope=system + handler 语言运行时/binary_probe/jar → asset_type=middleware
	if err := db.Exec(`
UPDATE host_vulnerabilities hv
JOIN vulnerabilities v ON v.id = hv.vuln_id
JOIN software s ON s.host_id = hv.host_id AND s.name = v.component
SET hv.asset_type = 'middleware'
WHERE (hv.asset_type IS NULL OR hv.asset_type = '' OR hv.asset_type = 'unknown')
  AND s.scope = 'system'
  AND s.source_handler IN ('jar_scanner', 'binary_probe', 'go_buildinfo', 'python', 'node', 'ruby', 'php')
`).Error; err != nil {
		return err
	}

	// Step 5: software 无关联 + vuln_category=language_dep → 大概率应用依赖
	if err := db.Exec(`
UPDATE host_vulnerabilities hv
JOIN vulnerabilities v ON v.id = hv.vuln_id
SET hv.asset_type = 'app'
WHERE (hv.asset_type IS NULL OR hv.asset_type = '' OR hv.asset_type = 'unknown')
  AND v.vuln_category = 'language_dep'
`).Error; err != nil {
		return err
	}

	// Step 6: 回填 fix_owner 按 asset_type + vuln_category 推导
	// SQL 直写,避免拉 11k 行进 Go 算
	if err := db.Exec(`
UPDATE host_vulnerabilities hv
JOIN vulnerabilities v ON v.id = hv.vuln_id
SET hv.fix_owner = CASE
  WHEN hv.asset_type = 'app' THEN 'dev'
  WHEN hv.asset_type IN ('container', 'image') THEN 'image_maintainer'
  WHEN hv.asset_type = 'os' AND v.vuln_category = 'db_service' THEN 'dba'
  WHEN hv.asset_type = 'os' AND v.vuln_category IN ('web_service', 'container_runtime', 'virtualization') THEN 'sre'
  WHEN hv.asset_type = 'os' THEN 'ops'
  WHEN hv.asset_type = 'middleware' AND v.vuln_category = 'db_service' THEN 'dba'
  WHEN hv.asset_type = 'middleware' AND v.vuln_category IN ('web_service', 'container_runtime') THEN 'sre'
  WHEN hv.asset_type = 'middleware' AND v.vuln_category = 'language_dep' THEN 'dev'
  WHEN hv.asset_type = 'middleware' THEN 'sre'
  ELSE 'unknown'
END
WHERE (hv.fix_owner IS NULL OR hv.fix_owner = '' OR hv.fix_owner = 'unknown')
`).Error; err != nil {
		return err
	}

	// Step 7: 把 app/container/image 类历史 failed precheck 状态批量标 not_applicable
	// 避免 cron 一直重试这些已知必失败的 SBOM 类漏洞
	if err := db.Exec(`
UPDATE host_vulnerabilities
SET precheck_status = 'not_applicable',
    precheck_message = '应用/容器漏洞不归 OS 包管理器,需 rebuild 业务程序或镜像'
WHERE asset_type IN ('app', 'container', 'image')
  AND precheck_status IN ('failed', 'unchecked')
`).Error; err != nil {
		return err
	}

	// 统计结果
	type stat struct {
		AssetType string
		N         int64
	}
	var stats []stat
	if err := db.Table("host_vulnerabilities").
		Select("asset_type, COUNT(*) AS n").
		Group("asset_type").Order("n DESC").Scan(&stats).Error; err == nil {
		for _, s := range stats {
			logger.Info("asset_type backfill stats", zap.String("type", s.AssetType), zap.Int64("n", s.N))
		}
	}
	return nil
}
