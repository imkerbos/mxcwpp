// Package handlers 提供各类资产采集器的实现
package handlers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/plugins/collector/engine"
)

// SoftwareHandler 是软件包采集器
type SoftwareHandler struct {
	Logger *zap.Logger
}

// Collect 采集软件包信息
func (h *SoftwareHandler) Collect(ctx context.Context) ([]interface{}, error) {
	var packages []interface{}

	// 检测包管理器类型
	packageManager := h.detectPackageManager()
	if packageManager == "" {
		h.Logger.Warn("no supported package manager found")
		return packages, nil
	}

	h.Logger.Debug("detected package manager", zap.String("type", packageManager))

	// 根据包管理器类型采集
	switch packageManager {
	case "rpm":
		rpmPackages, err := h.collectRPMPackages(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to collect RPM packages: %w", err)
		}
		packages = append(packages, rpmPackages...)
	case "deb":
		debPackages, err := h.collectDEBPackages(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to collect DEB packages: %w", err)
		}
		packages = append(packages, debPackages...)
	}

	return packages, nil
}

// detectPackageManager 检测包管理器类型
func (h *SoftwareHandler) detectPackageManager() string {
	// 检测 RPM
	if _, err := exec.LookPath("rpm"); err == nil {
		return "rpm"
	}

	// 检测 DPKG
	if _, err := exec.LookPath("dpkg"); err == nil {
		return "deb"
	}

	return ""
}

// collectRPMPackages 采集 RPM 包信息
func (h *SoftwareHandler) collectRPMPackages(ctx context.Context) ([]interface{}, error) {
	var packages []interface{}

	// 执行 rpm -qa --queryformat
	cmd := exec.CommandContext(ctx, "rpm", "-qa", "--queryformat", "%{NAME}|%{VERSION}|%{ARCH}|%{VENDOR}|%{INSTALLTIME}\n")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute rpm: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		select {
		case <-ctx.Done():
			return packages, ctx.Err()
		default:
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}

		pkg := &engine.SoftwareAsset{
			Asset: engine.Asset{
				CollectedAt: time.Now(),
			},
			Name:         parts[0],
			Version:      parts[1],
			Architecture: parts[2],
			PackageType:  "rpm",
		}

		if len(parts) > 3 && parts[3] != "" {
			pkg.Vendor = parts[3]
		}

		if len(parts) > 4 && parts[4] != "" {
			pkg.InstallTime = parts[4]
		}

		packages = append(packages, pkg)
	}

	return packages, nil
}

// collectDEBPackages 采集 DEB 包信息
func (h *SoftwareHandler) collectDEBPackages(ctx context.Context) ([]interface{}, error) {
	var packages []interface{}

	// 执行 dpkg-query
	cmd := exec.CommandContext(ctx, "dpkg-query", "-W", "-f", "${Package}|${Version}|${Architecture}|${Status}\n")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute dpkg-query: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		select {
		case <-ctx.Done():
			return packages, ctx.Err()
		default:
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}

		// 只采集已安装的包（Status 包含 "installed"）
		status := ""
		if len(parts) > 3 {
			status = parts[3]
		}
		if !strings.Contains(status, "installed") {
			continue
		}

		pkg := &engine.SoftwareAsset{
			Asset: engine.Asset{
				CollectedAt: time.Now(),
			},
			Name:         parts[0],
			Version:      parts[1],
			Architecture: parts[2],
			PackageType:  "deb",
		}

		packages = append(packages, pkg)
	}

	return packages, nil
}
