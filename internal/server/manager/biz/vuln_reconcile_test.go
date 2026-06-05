package biz

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// setupReconcileTestDB 建 sqlite 内存库 + 手动建表
// 手动 CREATE TABLE 而非 AutoMigrate：避免 GORM 在 sqlite 上的 MySQL 专有索引语法报错
func setupReconcileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	tables := []string{
		`CREATE TABLE hosts (
			host_id       TEXT PRIMARY KEY,
			hostname      TEXT,
			ipv4          TEXT DEFAULT '[]',
			status        TEXT DEFAULT 'offline',
			business_line TEXT
		)`,
		`CREATE TABLE vulnerabilities (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			cve_id            TEXT NOT NULL UNIQUE,
			osv_id            TEXT,
			purl              TEXT,
			severity          TEXT NOT NULL DEFAULT 'medium',
			cvss_score        REAL DEFAULT 0,
			component         TEXT,
			description       TEXT,
			affected_hosts    INTEGER DEFAULT 0,
			patched_hosts     INTEGER DEFAULT 0,
			status            TEXT NOT NULL DEFAULT 'unpatched',
			discovered_at     DATETIME,
			patched_at        DATETIME,
			current_version   TEXT,
			fixed_version     TEXT,
			reference_url     TEXT,
			source            TEXT,
			confidence        TEXT DEFAULT 'low',
			created_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
			deleted_at        DATETIME
		)`,
		`CREATE TABLE host_vulnerabilities (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			vuln_id         INTEGER NOT NULL,
			host_id         TEXT NOT NULL,
			hostname        TEXT,
			ip              TEXT,
			current_version TEXT,
			status          TEXT NOT NULL DEFAULT 'unpatched',
			patched_at      DATETIME,
			asset_type      TEXT DEFAULT 'unknown',
			subscope        TEXT DEFAULT 'unknown',
			fix_owner       TEXT DEFAULT 'unknown',
			host_binary_path TEXT,
			precheck_status  TEXT DEFAULT 'unchecked',
			precheck_message TEXT,
			precheck_packages TEXT,
			precheck_affected_processes TEXT,
			precheck_checked_at DATETIME,
			patched_reason   TEXT DEFAULT '',
			prev_status      TEXT DEFAULT '',
			vanished_at      DATETIME,
			resurfaced_at    DATETIME,
			created_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(vuln_id, host_id)
		)`,
		`CREATE TABLE software (
			id           TEXT PRIMARY KEY,
			host_id      TEXT NOT NULL,
			name         TEXT NOT NULL,
			version      TEXT,
			architecture TEXT,
			package_type TEXT NOT NULL DEFAULT 'rpm',
			vendor       TEXT,
			install_time TEXT,
			purl         TEXT,
			ecosystem    TEXT,
			source_file  TEXT,
			collected_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, ddl := range tables {
		require.NoError(t, db.Exec(ddl).Error, "failed DDL: %s", ddl)
	}
	return db
}

func TestReconcileHostVulns_PackageRemoved_MarksVanished(t *testing.T) {
	db := setupReconcileTestDB(t)
	logger := zap.NewNop()

	vuln := model.Vulnerability{
		CveID:        "CVE-2026-39830",
		Severity:     "critical",
		PURL:         "pkg:golang/golang.org/x/crypto",
		FixedVersion: "0.52.0",
	}
	require.NoError(t, db.Create(&vuln).Error)

	hv := model.HostVulnerability{
		VulnID:         vuln.ID,
		HostID:         "host-1",
		CurrentVersion: "v0.38.0",
		Status:         model.HostVulnStatusUnpatched,
	}
	require.NoError(t, db.Create(&hv).Error)

	// software 表故意不放该包，模拟包消失
	rec := NewVulnReconciler(db, logger)
	result, err := rec.ReconcileHosts([]string{"host-1"})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Vanished)
	assert.Equal(t, 0, result.Patched)

	var got model.HostVulnerability
	require.NoError(t, db.First(&got, hv.ID).Error)
	assert.Equal(t, model.HostVulnStatusVanished, got.Status)
	assert.Equal(t, model.PatchedReasonPackageRemoved, got.PatchedReason)
	assert.Equal(t, model.HostVulnStatusUnpatched, got.PrevStatus)
	require.NotNil(t, got.VanishedAt)
}

func TestReconcileHostVulns_VersionMeetsFix_MarksPatched(t *testing.T) {
	db := setupReconcileTestDB(t)
	logger := zap.NewNop()

	vuln := model.Vulnerability{
		CveID:        "CVE-2026-39830",
		Severity:     "critical",
		PURL:         "pkg:golang/golang.org/x/crypto",
		FixedVersion: "0.52.0",
	}
	require.NoError(t, db.Create(&vuln).Error)

	hv := model.HostVulnerability{
		VulnID:         vuln.ID,
		HostID:         "host-1",
		CurrentVersion: "v0.38.0",
		Status:         model.HostVulnStatusUnpatched,
	}
	require.NoError(t, db.Create(&hv).Error)

	sw := model.Software{
		ID:      "sw-1",
		HostID:  "host-1",
		Name:    "golang.org/x/crypto",
		Version: "v0.52.0",
		PURL:    "pkg:golang/golang.org/x/crypto",
	}
	require.NoError(t, db.Create(&sw).Error)

	rec := NewVulnReconciler(db, logger)
	result, err := rec.ReconcileHosts([]string{"host-1"})
	require.NoError(t, err)

	assert.Equal(t, 0, result.Vanished)
	assert.Equal(t, 1, result.Patched)

	var got model.HostVulnerability
	require.NoError(t, db.First(&got, hv.ID).Error)
	assert.Equal(t, model.HostVulnStatusPatched, got.Status)
	assert.Equal(t, model.PatchedReasonAutoVersionMatch, got.PatchedReason)
	require.NotNil(t, got.PatchedAt)
	assert.Equal(t, "v0.52.0", got.CurrentVersion)
}

func TestReconcileHostVulns_VersionStillLow_KeepsUnpatched(t *testing.T) {
	db := setupReconcileTestDB(t)
	logger := zap.NewNop()

	vuln := model.Vulnerability{
		CveID:        "CVE-2026-39830",
		Severity:     "critical",
		PURL:         "pkg:golang/golang.org/x/crypto",
		FixedVersion: "0.52.0",
	}
	require.NoError(t, db.Create(&vuln).Error)

	hv := model.HostVulnerability{
		VulnID:         vuln.ID,
		HostID:         "host-1",
		CurrentVersion: "v0.38.0",
		Status:         model.HostVulnStatusUnpatched,
	}
	require.NoError(t, db.Create(&hv).Error)

	sw := model.Software{
		ID:      "sw-1",
		HostID:  "host-1",
		Name:    "golang.org/x/crypto",
		Version: "v0.47.0",
		PURL:    "pkg:golang/golang.org/x/crypto",
	}
	require.NoError(t, db.Create(&sw).Error)

	rec := NewVulnReconciler(db, logger)
	result, err := rec.ReconcileHosts([]string{"host-1"})
	require.NoError(t, err)

	assert.Equal(t, 0, result.Vanished)
	assert.Equal(t, 0, result.Patched)

	var got model.HostVulnerability
	require.NoError(t, db.First(&got, hv.ID).Error)
	assert.Equal(t, model.HostVulnStatusUnpatched, got.Status)
	assert.Equal(t, "v0.47.0", got.CurrentVersion, "应更新 current_version 跟踪")
}

func TestReconcileHostVulns_FixedVersionEmpty_NoChange(t *testing.T) {
	db := setupReconcileTestDB(t)
	logger := zap.NewNop()

	vuln := model.Vulnerability{
		CveID:        "CVE-2026-39830",
		Severity:     "critical",
		PURL:         "pkg:golang/golang.org/x/crypto",
		FixedVersion: "",
	}
	require.NoError(t, db.Create(&vuln).Error)

	hv := model.HostVulnerability{
		VulnID:         vuln.ID,
		HostID:         "host-1",
		CurrentVersion: "v0.38.0",
		Status:         model.HostVulnStatusUnpatched,
	}
	require.NoError(t, db.Create(&hv).Error)

	sw := model.Software{
		ID:      "sw-1",
		HostID:  "host-1",
		Name:    "golang.org/x/crypto",
		Version: "v0.52.0",
		PURL:    "pkg:golang/golang.org/x/crypto",
	}
	require.NoError(t, db.Create(&sw).Error)

	rec := NewVulnReconciler(db, logger)
	result, err := rec.ReconcileHosts([]string{"host-1"})
	require.NoError(t, err)

	assert.Equal(t, 0, result.Vanished)
	assert.Equal(t, 0, result.Patched, "fixed_version 空时不应标 patched")
}

func TestReconcileHostVulns_MultipleHosts_BatchCorrect(t *testing.T) {
	db := setupReconcileTestDB(t)
	logger := zap.NewNop()

	vuln := model.Vulnerability{
		CveID:        "CVE-2026-39830",
		Severity:     "critical",
		PURL:         "pkg:golang/golang.org/x/crypto",
		FixedVersion: "0.52.0",
	}
	require.NoError(t, db.Create(&vuln).Error)

	for _, hostID := range []string{"host-1", "host-2", "host-3"} {
		require.NoError(t, db.Create(&model.HostVulnerability{
			VulnID: vuln.ID, HostID: hostID,
			CurrentVersion: "v0.38.0",
			Status:         model.HostVulnStatusUnpatched,
		}).Error)
	}
	require.NoError(t, db.Create(&model.Software{
		ID: "sw-2", HostID: "host-2",
		Name: "golang.org/x/crypto", Version: "v0.52.0",
		PURL: "pkg:golang/golang.org/x/crypto",
	}).Error)
	require.NoError(t, db.Create(&model.Software{
		ID: "sw-3", HostID: "host-3",
		Name: "golang.org/x/crypto", Version: "v0.47.0",
		PURL: "pkg:golang/golang.org/x/crypto",
	}).Error)

	rec := NewVulnReconciler(db, logger)
	result, err := rec.ReconcileHosts([]string{"host-1", "host-2", "host-3"})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Vanished)
	assert.Equal(t, 1, result.Patched)
}

// time import 占位（resurface 测试后续追加）
var _ = time.Now
