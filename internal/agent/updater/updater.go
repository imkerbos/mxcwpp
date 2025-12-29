// Package updater 实现 Agent 自更新功能
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/mxcsec-platform/mxcsec-platform/api/proto/grpc"
)

// Manager 是更新管理器
type Manager struct {
	logger         *zap.Logger
	updateCh       <-chan *grpc.AgentUpdate
	currentVersion string
	workDir        string
	mu             sync.Mutex
	updating       bool
}

// NewManager 创建更新管理器
func NewManager(logger *zap.Logger, updateCh <-chan *grpc.AgentUpdate, currentVersion string, workDir string) *Manager {
	return &Manager{
		logger:         logger,
		updateCh:       updateCh,
		currentVersion: currentVersion,
		workDir:        workDir,
		updating:       false,
	}
}

// Startup 启动更新模块
func Startup(ctx context.Context, wg *sync.WaitGroup, logger *zap.Logger, updateCh <-chan *grpc.AgentUpdate, currentVersion string, workDir string) {
	mgr := NewManager(logger, updateCh, currentVersion, workDir)
	StartupWithManager(ctx, wg, mgr)
}

// StartupWithManager 使用已创建的管理器启动更新模块
func StartupWithManager(ctx context.Context, wg *sync.WaitGroup, mgr *Manager) {
	defer wg.Done()

	mgr.logger.Info("updater module started",
		zap.String("current_version", mgr.currentVersion),
		zap.String("work_dir", mgr.workDir),
	)

	for {
		select {
		case <-ctx.Done():
			mgr.logger.Info("updater module shutting down")
			return
		case update := <-mgr.updateCh:
			if update == nil {
				continue
			}
			mgr.handleUpdate(ctx, update)
		}
	}
}

// handleUpdate 处理更新命令
func (m *Manager) handleUpdate(ctx context.Context, update *grpc.AgentUpdate) {
	m.mu.Lock()
	if m.updating {
		m.mu.Unlock()
		m.logger.Warn("update already in progress, ignoring new update command")
		return
	}
	m.updating = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.updating = false
		m.mu.Unlock()
	}()

	m.logger.Info("processing agent update",
		zap.String("target_version", update.Version),
		zap.String("current_version", m.currentVersion),
		zap.String("download_url", update.DownloadUrl),
		zap.String("pkg_type", update.PkgType),
		zap.String("arch", update.Arch),
		zap.Bool("force", update.Force),
	)

	// 检查是否需要更新
	if !update.Force && update.Version == m.currentVersion {
		m.logger.Info("already running target version, skipping update",
			zap.String("version", update.Version),
		)
		return
	}

	// 验证架构匹配
	currentArch := runtime.GOARCH
	if currentArch == "amd64" && update.Arch != "amd64" {
		m.logger.Error("architecture mismatch",
			zap.String("current_arch", currentArch),
			zap.String("update_arch", update.Arch),
		)
		return
	}
	if currentArch == "arm64" && update.Arch != "arm64" {
		m.logger.Error("architecture mismatch",
			zap.String("current_arch", currentArch),
			zap.String("update_arch", update.Arch),
		)
		return
	}

	// 验证包类型
	if update.PkgType != "rpm" && update.PkgType != "deb" {
		m.logger.Error("unsupported package type",
			zap.String("pkg_type", update.PkgType),
		)
		return
	}

	// 执行更新流程
	if err := m.doUpdate(ctx, update); err != nil {
		m.logger.Error("update failed",
			zap.String("version", update.Version),
			zap.Error(err),
		)
		return
	}

	m.logger.Info("update completed successfully, restarting agent",
		zap.String("version", update.Version),
	)

	// 重启 Agent
	m.restartAgent()
}

// doUpdate 执行更新流程
func (m *Manager) doUpdate(ctx context.Context, update *grpc.AgentUpdate) error {
	// 1. 创建临时目录
	tmpDir := filepath.Join(m.workDir, "update_tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 2. 确定包文件名
	pkgFileName := fmt.Sprintf("mxsec-agent-%s.%s", update.Version, update.PkgType)
	pkgPath := filepath.Join(tmpDir, pkgFileName)

	// 3. 下载包文件
	m.logger.Info("downloading update package",
		zap.String("url", update.DownloadUrl),
		zap.String("dest", pkgPath),
	)

	if err := m.downloadFile(ctx, update.DownloadUrl, pkgPath); err != nil {
		return fmt.Errorf("failed to download package: %w", err)
	}

	// 4. 验证 SHA256
	m.logger.Info("verifying package checksum",
		zap.String("expected_sha256", update.Sha256),
	)

	actualSHA256, err := m.calculateSHA256(pkgPath)
	if err != nil {
		return fmt.Errorf("failed to calculate SHA256: %w", err)
	}

	if !strings.EqualFold(actualSHA256, update.Sha256) {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", update.Sha256, actualSHA256)
	}

	m.logger.Info("checksum verified successfully")

	// 5. 安装包
	m.logger.Info("installing update package",
		zap.String("pkg_type", update.PkgType),
		zap.String("pkg_path", pkgPath),
	)

	if err := m.installPackage(update.PkgType, pkgPath); err != nil {
		return fmt.Errorf("failed to install package: %w", err)
	}

	return nil
}

// downloadFile 下载文件
func (m *Manager) downloadFile(ctx context.Context, url string, destPath string) error {
	// 创建带超时的 HTTP 客户端
	client := &http.Client{
		Timeout: 10 * time.Minute, // 下载超时 10 分钟
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// 创建目标文件
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// 写入文件
	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	m.logger.Debug("file downloaded",
		zap.String("path", destPath),
		zap.Int64("bytes", written),
	)

	return nil
}

// calculateSHA256 计算文件的 SHA256 哈希值
func (m *Manager) calculateSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// installPackage 安装包
func (m *Manager) installPackage(pkgType string, pkgPath string) error {
	var cmd *exec.Cmd

	switch pkgType {
	case "rpm":
		// 使用 rpm -Uvh 升级安装（-U: upgrade, -v: verbose, -h: hash marks）
		cmd = exec.Command("rpm", "-Uvh", "--force", pkgPath)
	case "deb":
		// 使用 dpkg -i 安装 deb 包
		cmd = exec.Command("dpkg", "-i", pkgPath)
	default:
		return fmt.Errorf("unsupported package type: %s", pkgType)
	}

	// 执行安装命令
	output, err := cmd.CombinedOutput()
	if err != nil {
		m.logger.Error("package installation failed",
			zap.String("output", string(output)),
			zap.Error(err),
		)
		return fmt.Errorf("installation failed: %s, output: %s", err, string(output))
	}

	m.logger.Info("package installed successfully",
		zap.String("output", string(output)),
	)

	return nil
}

// restartAgent 重启 Agent 服务
func (m *Manager) restartAgent() {
	m.logger.Info("restarting mxsec-agent service...")

	// 使用 systemctl 重启服务
	// 注意：这会导致当前进程被终止，因此使用 go routine 并稍微延迟执行
	go func() {
		// 延迟 2 秒以确保日志被写入
		time.Sleep(2 * time.Second)

		cmd := exec.Command("systemctl", "restart", "mxsec-agent")
		if err := cmd.Start(); err != nil {
			m.logger.Error("failed to restart service",
				zap.Error(err),
			)
			// 如果 systemctl 失败，尝试直接退出让 systemd 自动重启
			m.logger.Info("attempting to exit for systemd auto-restart")
			os.Exit(0)
		}
	}()
}
