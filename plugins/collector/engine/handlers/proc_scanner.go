// Package handlers 提供各类资产采集器的实现
package handlers

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ProcScannerHandler 扫描 /proc/*/exe，输出主机上"正在运行"的二进制清单
// 目的：标记 inventory 中哪些软件实际运行（与 software/binary_probe 互补）
type ProcScannerHandler struct {
	Logger *zap.Logger
}

// Collect 遍历 /proc 拿到运行进程的真实二进制路径，去重后输出
func (h *ProcScannerHandler) Collect(ctx context.Context) ([]interface{}, error) {
	var results []interface{}

	// /proc 仅在 Linux 上存在，缺失直接返回
	if _, err := os.Stat("/proc"); err != nil {
		h.Logger.Debug("/proc not available, skip proc_scanner")
		return results, nil
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc: %w", err)
	}

	// 用 map 去重：key=真实二进制路径，value=占位
	seen := make(map[string]struct{})

	for _, e := range entries {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		if !e.IsDir() {
			continue
		}

		// 只关心数字目录（PID）
		name := e.Name()
		if _, err := strconv.Atoi(name); err != nil {
			continue
		}

		exeLink := "/proc/" + name + "/exe"
		realPath, err := os.Readlink(exeLink)
		if err != nil {
			// 权限不足 / 进程已退出 / 内核线程：静默跳过
			continue
		}

		// 跳过被删除的二进制（例如 "/usr/bin/foo (deleted)"）
		if strings.HasSuffix(realPath, " (deleted)") {
			continue
		}

		// 跳过空 exe（内核线程通常 readlink 失败，但保险处理）
		if realPath == "" {
			continue
		}

		// 处理可能含 /proc/{pid}/root/ 前缀（容器/chroot 内进程，主机视角下的路径）
		realPath = stripProcRootPrefix(realPath)

		// 再次校验：去掉前缀后不能是相对路径或空
		if realPath == "" || !filepath.IsAbs(realPath) {
			continue
		}

		// 去重
		if _, ok := seen[realPath]; ok {
			continue
		}
		seen[realPath] = struct{}{}

		// 拿 stat 信息（size + mtime），失败不阻塞输出
		var size int64
		var mtimeStr string
		if st, err := os.Stat(realPath); err == nil {
			size = st.Size()
			mtimeStr = st.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		} else {
			// 权限不足或文件不在主机命名空间（容器内独立 rootfs）：跳过
			continue
		}

		// 0 长度二进制视为内核线程，不上报
		if size == 0 {
			continue
		}

		basename := filepath.Base(realPath)

		// 输出格式与 python_packages 保持一致（map[string]interface{}）
		// Version 留空，由 binary_probe / go_buildinfo 后续补
		results = append(results, map[string]interface{}{
			"name":         basename,
			"version":      "",
			"collected_at": time.Now().Format(time.RFC3339),
			"package_type": "running-binary",
			"purl":         fmt.Sprintf("pkg:generic/%s?probe=procfs", url.PathEscape(basename)),
			"source_file":  realPath,
			"size":         size,
			"mtime":        mtimeStr,
		})
	}

	h.Logger.Debug("proc_scanner finished",
		zap.Int("unique_binaries", len(seen)),
		zap.Int("reported", len(results)))

	return results, nil
}

// stripProcRootPrefix 去掉 /proc/{pid}/root/ 前缀（容器/chroot 内进程的 readlink 可能带此前缀）
// 例如 "/proc/1234/root/usr/bin/nginx" -> "/usr/bin/nginx"
func stripProcRootPrefix(p string) string {
	if !strings.HasPrefix(p, "/proc/") {
		return p
	}
	rest := p[len("/proc/"):]
	// 形如 "1234/root/..."
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return p
	}
	pidPart := rest[:slash]
	if _, err := strconv.Atoi(pidPart); err != nil {
		return p
	}
	tail := rest[slash+1:]
	if !strings.HasPrefix(tail, "root/") && tail != "root" {
		return p
	}
	stripped := strings.TrimPrefix(tail, "root")
	if stripped == "" {
		return p
	}
	return stripped
}
