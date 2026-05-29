package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// node_modules 扫描根路径（含 glob 通配）
var nodeScanGlobs = []string{
	"/opt/*/node_modules",
	"/usr/local/*/node_modules",
	"/srv/*/node_modules",
	"/var/www/*/node_modules",
	"/home/*/node_modules",
}

// 从扫描根向下找 node_modules 的最大深度
const nodeMaxScanDepth = 4

// package.json 大小上限 5MB
const nodeMaxFileSize = 5 * 1024 * 1024

// NodePackagesHandler 扫描已安装的 Node 包（node_modules 顶层 + @scope）
type NodePackagesHandler struct {
	Logger *zap.Logger
}

// Collect 实现 engine.Handler 接口
func (h *NodePackagesHandler) Collect(ctx context.Context) ([]interface{}, error) {
	var (
		results []interface{}
		seen    = make(map[string]struct{}) // name@version 去重
	)

	// 1. 直接命中：glob 出来本身就是 node_modules 目录
	nodeModuleDirs := make(map[string]struct{})
	for _, pattern := range nodeScanGlobs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			h.Logger.Debug("glob 展开失败", zap.String("pattern", pattern), zap.Error(err))
			continue
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || !info.IsDir() {
				continue
			}
			nodeModuleDirs[m] = struct{}{}
		}
	}

	// 2. 找到的每个 node_modules 顶层包做扫描
	for nm := range nodeModuleDirs {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		pkgs, err := h.scanNodeModules(ctx, nm, seen)
		if err != nil {
			h.Logger.Debug("扫描 node_modules 失败", zap.String("dir", nm), zap.Error(err))
			continue
		}
		results = append(results, pkgs...)
	}

	// 3. 另外：在 glob 根的父目录里向下浅扫，发现嵌套的 node_modules（不递归子 node_modules）
	additional := h.discoverNestedNodeModules(ctx, nodeModuleDirs)
	for _, nm := range additional {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}
		pkgs, err := h.scanNodeModules(ctx, nm, seen)
		if err != nil {
			continue
		}
		results = append(results, pkgs...)
	}

	h.Logger.Info("Node 已装包扫描完成",
		zap.Int("node_modules_dirs", len(nodeModuleDirs)+len(additional)),
		zap.Int("total_packages", len(results)))
	return results, nil
}

// discoverNestedNodeModules 在扫描根附近浅扫，找出嵌套的 node_modules
// 跳过已知的，命中 node_modules 后不再向下走（避免进入依赖树）
func (h *NodePackagesHandler) discoverNestedNodeModules(ctx context.Context, known map[string]struct{}) []string {
	var roots []string
	// 把 glob 的父目录作为浅扫根
	for _, pattern := range nodeScanGlobs {
		parent := filepath.Dir(pattern) // 如 /opt/* 或 /home/*
		matches, err := filepath.Glob(parent)
		if err != nil {
			continue
		}
		roots = append(roots, matches...)
	}

	var found []string
	for _, root := range roots {
		select {
		case <-ctx.Done():
			return found
		default:
		}

		baseDepth := strings.Count(root, string(filepath.Separator))
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			depth := strings.Count(path, string(filepath.Separator)) - baseDepth
			if depth > nodeMaxScanDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if d.Name() != "node_modules" {
				return nil
			}
			if _, ok := known[path]; ok {
				return filepath.SkipDir
			}
			known[path] = struct{}{}
			found = append(found, path)
			// 命中 node_modules 后不再向下走（避免进入依赖树）
			return filepath.SkipDir
		})
	}
	return found
}

// scanNodeModules 扫一个 node_modules 顶层：仅读直接子项的 package.json，不递归子 node_modules
func (h *NodePackagesHandler) scanNodeModules(ctx context.Context, nodeModulesDir string, seen map[string]struct{}) ([]interface{}, error) {
	var results []interface{}

	entries, err := os.ReadDir(nodeModulesDir)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		name := e.Name()
		if !e.IsDir() {
			continue
		}
		// 跳过隐藏目录与缓存
		if strings.HasPrefix(name, ".") {
			continue
		}

		entryPath := filepath.Join(nodeModulesDir, name)

		if strings.HasPrefix(name, "@") {
			// scope 目录：再下一级才是真实包
			scopeEntries, err := os.ReadDir(entryPath)
			if err != nil {
				continue
			}
			for _, se := range scopeEntries {
				if !se.IsDir() {
					continue
				}
				pkgDir := filepath.Join(entryPath, se.Name())
				if pkg, ok := h.parsePackageJSON(pkgDir, seen); ok {
					results = append(results, pkg)
				}
			}
			continue
		}

		if pkg, ok := h.parsePackageJSON(entryPath, seen); ok {
			results = append(results, pkg)
		}
	}

	return results, nil
}

// parsePackageJSON 读取 <pkgDir>/package.json 并构造记录
func (h *NodePackagesHandler) parsePackageJSON(pkgDir string, seen map[string]struct{}) (map[string]interface{}, bool) {
	pjPath := filepath.Join(pkgDir, "package.json")
	info, err := os.Stat(pjPath)
	if err != nil || info.IsDir() {
		return nil, false
	}
	if info.Size() <= 0 || info.Size() > nodeMaxFileSize {
		return nil, false
	}

	raw, err := os.ReadFile(pjPath)
	if err != nil {
		return nil, false
	}

	var meta struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, false
	}
	if meta.Name == "" || meta.Version == "" {
		return nil, false
	}

	key := meta.Name + "@" + meta.Version
	if _, dup := seen[key]; dup {
		return nil, false
	}
	seen[key] = struct{}{}

	return map[string]interface{}{
		"name":         meta.Name,
		"version":      meta.Version,
		"package_type": "npm",
		"ecosystem":    "npm",
		"purl":         buildNPMPURL(meta.Name, meta.Version),
		"source_file":  pjPath,
	}, true
}

// buildNPMPURL 生成 npm 包 PURL，保留 @scope/name 语义
// 规范：pkg:npm/{namespace}/{name}@{version} 或 pkg:npm/{name}@{version}
func buildNPMPURL(name, version string) string {
	if strings.HasPrefix(name, "@") {
		// @scope/pkg → namespace=@scope, name=pkg
		slash := strings.Index(name, "/")
		if slash > 0 {
			scope := name[:slash]
			pkg := name[slash+1:]
			return fmt.Sprintf("pkg:npm/%s/%s@%s",
				url.PathEscape(scope),
				url.PathEscape(pkg),
				url.PathEscape(version))
		}
	}
	return fmt.Sprintf("pkg:npm/%s@%s", url.PathEscape(name), url.PathEscape(version))
}
