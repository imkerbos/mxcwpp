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

	"github.com/imkerbos/mxsec-platform/api/proto/grpc"
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

	// 检查是否为版本降级
	if m.isDowngrade(m.currentVersion, update.Version) {
		m.logger.Warn("detected version downgrade (rollback)",
			zap.String("current_version", m.currentVersion),
			zap.String("target_version", update.Version),
			zap.Bool("force", update.Force),
		)
		// 如果不是强制更新，记录警告但继续（允许回退）
		if !update.Force {
			m.logger.Info("allowing downgrade without force flag")
		}
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
	// 预检查：验证包文件存在并可读
	if _, err := os.Stat(pkgPath); err != nil {
		return fmt.Errorf("package file not accessible: %w", err)
	}

	// 预检查：诊断系统环境
	m.diagnoseSystemEnv(pkgType)

	var cmd *exec.Cmd

	switch pkgType {
	case "rpm":
		// 参考 Elkeid 设计：直接使用 rpm -Uvh 升级
		// RPM 的 lifecycle 脚本会处理：
		// - preremove: 升级时 ($1==1) 不停止服务，只有卸载时 ($1==0) 才停止
		// - postinstall: 升级时 ($1==2) 只 reload daemon，不启动服务
		// 这样升级过程中 Agent 进程保持运行，避免 systemd Restart=always 竞态
		cmd = exec.Command("rpm", "-Uvh", pkgPath)

		m.logger.Info("executing RPM upgrade command",
			zap.String("command", fmt.Sprintf("rpm -Uvh %s", pkgPath)),
			zap.Int("uid", os.Getuid()),
			zap.Int("gid", os.Getgid()),
		)
	case "deb":
		// 使用 dpkg -i 安装 deb 包
		cmd = exec.Command("dpkg", "-i", pkgPath)

		m.logger.Info("executing DEB install command",
			zap.String("command", fmt.Sprintf("dpkg -i %s", pkgPath)),
			zap.Int("uid", os.Getuid()),
			zap.Int("gid", os.Getgid()),
		)
	default:
		return fmt.Errorf("unsupported package type: %s", pkgType)
	}

	// 执行安装命令
	output, err := cmd.CombinedOutput()

	// 记录详细的输出，无论成功失败
	m.logger.Info("package installation output",
		zap.String("output", string(output)),
		zap.Bool("success", err == nil),
	)

	if err != nil {
		// 检查是否是 "already installed" 错误（RPM exit status 3）
		outputStr := string(output)
		if strings.Contains(outputStr, "is already installed") {
			m.logger.Info("package is already installed, treating as success",
				zap.String("output", outputStr),
			)
			return nil
		}

		m.logger.Error("package installation failed",
			zap.String("command", cmd.String()),
			zap.String("output", outputStr),
			zap.Error(err),
		)
		return fmt.Errorf("installation failed: %s, output: %s", err, outputStr)
	}

	m.logger.Info("package installed successfully")
	return nil
}

// diagnoseSystemEnv 诊断系统环境
func (m *Manager) diagnoseSystemEnv(pkgType string) {
	// 检查当前用户权限
	uid := os.Getuid()
	gid := os.Getgid()

	m.logger.Info("system environment diagnostic",
		zap.Int("uid", uid),
		zap.Int("gid", gid),
		zap.Bool("is_root", uid == 0),
	)

	if uid != 0 {
		m.logger.Warn("agent is not running as root, package installation may fail",
			zap.Int("current_uid", uid),
			zap.String("hint", "ensure mxsec-agent.service has User=root in systemd config"),
		)
	}

	// 检查 RPM 数据库
	if pkgType == "rpm" {
		rpmDbPath := "/var/lib/rpm"
		if stat, err := os.Stat(rpmDbPath); err != nil {
			m.logger.Warn("rpm database directory not accessible",
				zap.String("path", rpmDbPath),
				zap.Error(err),
			)
		} else {
			m.logger.Debug("rpm database directory status",
				zap.String("path", rpmDbPath),
				zap.String("mode", stat.Mode().String()),
				zap.Bool("is_dir", stat.IsDir()),
			)
		}

		// 检查文件系统只读状态
		if _, err := os.CreateTemp(rpmDbPath, "test-write-*"); err != nil {
			m.logger.Error("rpm database directory is not writable",
				zap.String("path", rpmDbPath),
				zap.Error(err),
				zap.String("hint", "check filesystem mount options with 'mount | grep /var'"),
			)
		} else {
			m.logger.Debug("rpm database directory is writable")
		}
	}
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

// isDowngrade 检查是否为版本降级
// 简单的版本比较：假设版本格式为 major.minor.patch
func (m *Manager) isDowngrade(currentVer, targetVer string) bool {
	// 移除 'v' 前缀（如果有）
	currentVer = strings.TrimPrefix(currentVer, "v")
	targetVer = strings.TrimPrefix(targetVer, "v")

	// 分割版本号
	currentParts := strings.Split(currentVer, ".")
	targetParts := strings.Split(targetVer, ".")

	// 逐段比较
	maxLen := len(currentParts)
	if len(targetParts) > maxLen {
		maxLen = len(targetParts)
	}

	for i := 0; i < maxLen; i++ {
		var current, target int

		// 解析当前版本的段
		if i < len(currentParts) {
			fmt.Sscanf(currentParts[i], "%d", &current)
		}

		// 解析目标版本的段
		if i < len(targetParts) {
			fmt.Sscanf(targetParts[i], "%d", &target)
		}

		if target < current {
			return true // 目标版本更小，是降级
		} else if target > current {
			return false // 目标版本更大，是升级
		}
		// 相等则继续比较下一段
	}

	return false // 版本相同，不是降级
}
