package handlers

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// jar 扫描默认根目录（Tomcat/Spring/独立部署常见路径）
var jarDefaultScanDirs = []string{
	"/opt",
	"/usr/local",
	"/srv",
	"/var/lib",
	"/var/www",
	"/home",
}

// jar 文件大小上限，超过则跳过（fat jar 解压慢且 RAM 占用大）
const jarMaxFileSize = 100 * 1024 * 1024 // 100MB

// jar 扫描最大目录深度
const jarMaxScanDepth = 6

// jar 文件后缀
var jarExtensions = map[string]struct{}{
	".jar": {},
	".war": {},
	".ear": {},
}

// 跳过的目录名（避免扫描系统/缓存/源码目录浪费）
var jarSkipDirs = map[string]struct{}{
	".git":         {},
	".svn":         {},
	".hg":          {},
	".cache":       {},
	"node_modules": {},
	"__pycache__":  {},
	"proc":         {},
	"sys":          {},
	"dev":          {},
}

// JarScannerHandler 扫描磁盘上的 jar/war/ear 并解析其内部 BOM
// 不解压 jar，直接通过 archive/zip 读取 META-INF/MANIFEST.MF 与 pom.properties
type JarScannerHandler struct {
	Logger   *zap.Logger
	ScanDirs []string // 可配置扫描目录
}

// Collect 实现 engine.Handler 接口
func (h *JarScannerHandler) Collect(ctx context.Context) ([]interface{}, error) {
	dirs := h.ScanDirs
	if len(dirs) == 0 {
		dirs = jarDefaultScanDirs
	}

	var (
		results []interface{}
		// group:artifact:version 去重（无 group 时用 name:version）
		seen = make(map[string]struct{})
	)

	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}

		found, err := h.scanDir(ctx, dir, seen)
		if err != nil {
			h.Logger.Warn("jar 扫描目录失败", zap.String("dir", dir), zap.Error(err))
			continue
		}
		results = append(results, found...)
	}

	h.Logger.Info("jar 扫描完成", zap.Int("total_packages", len(results)))
	return results, nil
}

// scanDir 遍历单个根目录，找出所有 jar/war/ear 并解析
func (h *JarScannerHandler) scanDir(ctx context.Context, root string, seen map[string]struct{}) ([]interface{}, error) {
	var results []interface{}
	baseDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 无权限/不存在等错误：跳过单条，不中断整体
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 深度控制
		depth := strings.Count(path, string(filepath.Separator)) - baseDepth
		if depth > jarMaxScanDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if _, skip := jarSkipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := jarExtensions[ext]; !ok {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() == 0 || info.Size() > jarMaxFileSize {
			return nil
		}

		pkgs, err := h.parseJar(path)
		if err != nil {
			h.Logger.Debug("jar 解析失败", zap.String("path", path), zap.Error(err))
			return nil
		}

		for _, pkg := range pkgs {
			key := jarDedupKey(pkg)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, pkg)
		}
		return nil
	})

	return results, err
}

// jarDedupKey 取 group:artifact:version 作为去重键，若缺 group 则用 name:version
func jarDedupKey(pkg map[string]interface{}) string {
	name, _ := pkg["name"].(string)
	version, _ := pkg["version"].(string)
	return name + "@" + version
}

// parseJar 用 archive/zip 读 jar 内部 BOM 信息
// pom.properties 优先（最权威），其次 MANIFEST.MF
func (h *JarScannerHandler) parseJar(path string) ([]map[string]interface{}, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("zip open: %w", err)
	}
	defer zr.Close()

	var (
		// pom.properties 可能有多个（fat jar 内嵌依赖），全部采集
		pomPkgs []map[string]interface{}
		// MANIFEST.MF 只有一份，作为最后的回退
		manifestPkg map[string]interface{}
	)

	for _, f := range zr.File {
		name := f.Name
		switch {
		case strings.HasPrefix(name, "META-INF/maven/") && strings.HasSuffix(name, "/pom.properties"):
			pkg, ok := readPomProperties(f, path)
			if ok {
				pomPkgs = append(pomPkgs, pkg)
			}
		case name == "META-INF/MANIFEST.MF" && manifestPkg == nil:
			pkg, ok := readManifest(f, path)
			if ok {
				manifestPkg = pkg
			}
		}
	}

	if len(pomPkgs) > 0 {
		return pomPkgs, nil
	}
	if manifestPkg != nil {
		return []map[string]interface{}{manifestPkg}, nil
	}
	return nil, nil
}

// readPomProperties 解析 META-INF/maven/{group}/{artifact}/pom.properties
func readPomProperties(f *zip.File, jarPath string) (map[string]interface{}, bool) {
	rc, err := f.Open()
	if err != nil {
		return nil, false
	}
	defer rc.Close()

	var groupID, artifactID, version string
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "groupId":
			groupID = val
		case "artifactId":
			artifactID = val
		case "version":
			version = val
		}
	}

	if artifactID == "" || version == "" {
		return nil, false
	}

	var name, purl string
	if groupID != "" {
		name = groupID + ":" + artifactID
		purl = fmt.Sprintf("pkg:maven/%s/%s@%s",
			url.PathEscape(groupID),
			url.PathEscape(artifactID),
			url.PathEscape(version))
	} else {
		name = artifactID
		purl = fmt.Sprintf("pkg:generic/%s@%s",
			url.PathEscape(artifactID),
			url.PathEscape(version))
	}

	return map[string]interface{}{
		"name":         name,
		"version":      version,
		"collected_at": time.Now().Format(time.RFC3339),
		"package_type": "jar",
		"ecosystem":    "Maven",
		"purl":         purl,
		"source_file":  jarPath,
	}, true
}

// readManifest 解析 META-INF/MANIFEST.MF
// 优先 Bundle-SymbolicName/Bundle-Version（OSGi），其次 Implementation-Title/Implementation-Version
func readManifest(f *zip.File, jarPath string) (map[string]interface{}, bool) {
	rc, err := f.Open()
	if err != nil {
		return nil, false
	}
	defer rc.Close()

	// MANIFEST.MF 单文件不会大，但仍上限 1MB 防御
	const maxManifestSize = 1 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(rc, maxManifestSize))
	if err != nil {
		return nil, false
	}

	attrs := parseManifestAttrs(data)

	var name, version string
	if v, ok := attrs["Bundle-SymbolicName"]; ok {
		// Bundle-SymbolicName 可能带 ;singleton:=true 之类后缀，截断
		if idx := strings.Index(v, ";"); idx > 0 {
			v = strings.TrimSpace(v[:idx])
		}
		name = v
		version = attrs["Bundle-Version"]
	}
	if name == "" || version == "" {
		if v, ok := attrs["Implementation-Title"]; ok && v != "" {
			name = v
			version = attrs["Implementation-Version"]
		}
	}

	if name == "" || version == "" {
		return nil, false
	}

	return map[string]interface{}{
		"name":         name,
		"version":      version,
		"collected_at": time.Now().Format(time.RFC3339),
		"package_type": "jar",
		"ecosystem":    "Maven",
		"purl":         fmt.Sprintf("pkg:generic/%s@%s", url.PathEscape(name), url.PathEscape(version)),
		"source_file":  jarPath,
	}, true
}

// parseManifestAttrs 解析 jar manifest（RFC 822 风格），处理续行（前导空格）
func parseManifestAttrs(data []byte) map[string]string {
	attrs := make(map[string]string)
	lines := strings.Split(string(data), "\n")

	var curKey, curVal string
	flush := func() {
		if curKey != "" {
			attrs[curKey] = strings.TrimSpace(curVal)
		}
		curKey, curVal = "", ""
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			// 主属性段结束（manifest 主段 + per-entry 段以空行分隔）
			// 后续 per-entry 段不携带 jar 整体 BOM，停止解析
			flush()
			break
		}
		// 续行：以单个空格开头，拼接到当前 value
		if strings.HasPrefix(line, " ") {
			curVal += line[1:]
			continue
		}
		// 新键
		flush()
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		curKey = strings.TrimSpace(line[:idx])
		curVal = strings.TrimSpace(line[idx+1:])
	}
	flush()

	return attrs
}
