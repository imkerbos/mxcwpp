package handlers

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Python site-packages 扫描根路径（含 glob 通配）
var pythonScanGlobs = []string{
	"/usr/lib/python*/site-packages",
	"/usr/local/lib/python*/site-packages",
	"/opt/*/lib/python*/site-packages",
	"/home/*/.local/lib/python*/site-packages",
	"/usr/lib64/python*/site-packages",
}

// site-packages 内向下找 dist-info 的最大深度
const pythonMaxScanDepth = 3

// METADATA 文件大小上限 5MB
const pythonMaxFileSize = 5 * 1024 * 1024

// PythonPackagesHandler 扫描已安装的 Python 包（site-packages 内 dist-info / egg-info）
type PythonPackagesHandler struct {
	Logger *zap.Logger
}

// Collect 实现 engine.Handler 接口
func (h *PythonPackagesHandler) Collect(ctx context.Context) ([]interface{}, error) {
	var (
		results []interface{}
		seen    = make(map[string]struct{}) // name@version 去重
	)

	// 展开 glob，得到所有真实存在的 site-packages 目录
	var siteDirs []string
	for _, pattern := range pythonScanGlobs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			h.Logger.Debug("glob 展开失败", zap.String("pattern", pattern), zap.Error(err))
			continue
		}
		siteDirs = append(siteDirs, matches...)
	}

	for _, siteDir := range siteDirs {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		info, err := os.Stat(siteDir)
		if err != nil || !info.IsDir() {
			continue
		}

		pkgs, err := h.scanSiteDir(ctx, siteDir, seen)
		if err != nil {
			h.Logger.Debug("扫描 site-packages 失败", zap.String("dir", siteDir), zap.Error(err))
			continue
		}
		results = append(results, pkgs...)
	}

	h.Logger.Info("Python 已装包扫描完成",
		zap.Int("site_dirs", len(siteDirs)),
		zap.Int("total_packages", len(results)))
	return results, nil
}

// scanSiteDir 扫描单个 site-packages 目录
func (h *PythonPackagesHandler) scanSiteDir(ctx context.Context, siteDir string, seen map[string]struct{}) ([]interface{}, error) {
	var results []interface{}
	baseDepth := strings.Count(siteDir, string(filepath.Separator))

	err := filepath.WalkDir(siteDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过无权限项
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 控制深度
		depth := strings.Count(path, string(filepath.Separator)) - baseDepth
		if depth > pythonMaxScanDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.IsDir() {
			return nil
		}

		name := d.Name()
		if !strings.HasSuffix(name, ".dist-info") && !strings.HasSuffix(name, ".egg-info") {
			return nil
		}

		pkg, ok := h.parseDistInfo(path)
		if !ok {
			return filepath.SkipDir
		}

		key := strings.ToLower(pkg["name"].(string)) + "@" + pkg["version"].(string)
		if _, dup := seen[key]; dup {
			return filepath.SkipDir
		}
		seen[key] = struct{}{}

		results = append(results, pkg)
		// dist-info / egg-info 目录内不再递归
		return filepath.SkipDir
	})

	return results, err
}

// parseDistInfo 解析 dist-info / egg-info 目录内的 METADATA / PKG-INFO 文件
func (h *PythonPackagesHandler) parseDistInfo(distDir string) (map[string]interface{}, bool) {
	// dist-info 用 METADATA；egg-info 用 PKG-INFO
	candidates := []string{
		filepath.Join(distDir, "METADATA"),
		filepath.Join(distDir, "PKG-INFO"),
	}

	var metaPath string
	var metaInfo os.FileInfo
	for _, p := range candidates {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		metaPath = p
		metaInfo = info
		break
	}
	if metaPath == "" {
		return nil, false
	}

	if metaInfo.Size() <= 0 || metaInfo.Size() > pythonMaxFileSize {
		return nil, false
	}

	f, err := os.Open(metaPath)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	var name, version string
	scanner := bufio.NewScanner(f)
	// 单行上限 1MB，足以容纳超长头
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// RFC 822 头部以空行结束
		if line == "" {
			break
		}
		// 仅处理 "Key: Value" 形式（忽略续行）
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "Name":
			if name == "" {
				name = val
			}
		case "Version":
			if version == "" {
				version = val
			}
		}
		if name != "" && version != "" {
			break
		}
	}

	if name == "" || version == "" {
		return nil, false
	}

	lowerName := strings.ToLower(name)
	return map[string]interface{}{
		"name":         lowerName,
		"version":      version,
		"collected_at": time.Now().Format(time.RFC3339),
		"package_type": "pip",
		"ecosystem":    "PyPI",
		"purl":         fmt.Sprintf("pkg:pypi/%s@%s", url.PathEscape(lowerName), url.PathEscape(version)),
		"source_file":  distDir,
	}, true
}
