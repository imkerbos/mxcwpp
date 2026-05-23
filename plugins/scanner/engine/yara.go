package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	yaraRulesDir = "/var/mxsec/yara-rules"
	yaraBinary   = "yr" // YARA-X CLI
)

// YARAScanner 基于 YARA-X CLI 的扫描器
type YARAScanner struct {
	logger   *zap.Logger
	rulesDir string
}

// NewYARAScanner 创建 YARA-X 扫描器
func NewYARAScanner(logger *zap.Logger) *YARAScanner {
	rulesDir := yaraRulesDir
	// 如果插件目录下有 yara-rules，优先使用
	if pluginDir := os.Getenv("PLUGIN_DIR"); pluginDir != "" {
		localRules := filepath.Join(pluginDir, "yara-rules")
		if info, err := os.Stat(localRules); err == nil && info.IsDir() {
			rulesDir = localRules
		}
	}
	return &YARAScanner{
		logger:   logger,
		rulesDir: rulesDir,
	}
}

// Available 检查 yr (YARA-X) 是否可用（验证二进制可执行，非仅文件存在）
func (s *YARAScanner) Available() bool {
	bin := s.findBinary()
	if bin == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		s.logger.Warn("yr 二进制存在但无法执行",
			zap.String("path", bin),
			zap.String("output", string(out)),
			zap.Error(err))
		return false
	}
	return true
}

// findBinary 查找 yr 路径：优先插件目录 → 系统 PATH
func (s *YARAScanner) findBinary() string {
	// 1. 插件工作目录下的 bin/yr
	if pluginDir := os.Getenv("PLUGIN_DIR"); pluginDir != "" {
		local := filepath.Join(pluginDir, "bin", "yr")
		if _, err := os.Stat(local); err == nil {
			return local
		}
	}
	// 2. 系统 PATH
	if p, err := exec.LookPath(yaraBinary); err == nil {
		return p
	}
	return ""
}

// yaraOutput is the top-level YARA-X v1.15+ JSON output structure.
type yaraOutput struct {
	Version string          `json:"version"`
	Matches []yaraMatchItem `json:"matches"`
}

// yaraMatchItem represents a single match in YARA-X v1.15+ JSON output.
type yaraMatchItem struct {
	Rule      string            `json:"rule"`
	File      string            `json:"file"`
	Namespace string            `json:"namespace,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Scan 使用 YARA-X 扫描指定路径
func (s *YARAScanner) Scan(ctx context.Context, paths []string) ([]ScanResult, error) {
	if !s.Available() {
		s.logger.Warn("yr (YARA-X) 不可用，跳过 YARA 扫描")
		return nil, nil
	}

	var results []ScanResult

	for _, scanPath := range paths {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		pathResults, err := s.scanPath(ctx, scanPath)
		if err != nil {
			s.logger.Error("YARA 扫描路径失败",
				zap.String("path", scanPath),
				zap.Error(err))
			continue
		}
		results = append(results, pathResults...)
	}

	return results, nil
}

// scanPath 使用 YARA-X 扫描单个路径
func (s *YARAScanner) scanPath(ctx context.Context, scanPath string) ([]ScanResult, error) {
	// yr scan -r --output-format=json RULES_DIR TARGET
	args := []string{
		"scan",
		"-r",
		"--output-format=json",
		s.rulesDir,
		scanPath,
	}

	cmd := exec.CommandContext(ctx, s.findBinary(), args...)
	output, err := cmd.Output()
	if err != nil {
		// yr 返回非零退出码可能表示匹配或错误
		if exitErr, ok := err.(*exec.ExitError); ok {
			// 如果有 stdout 输出，说明有匹配结果
			if len(output) > 0 {
				s.logger.Debug("YARA 扫描发现匹配", zap.Int("exit_code", exitErr.ExitCode()))
			} else {
				return nil, fmt.Errorf("yr 执行错误 (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
			}
		} else {
			return nil, fmt.Errorf("yr 执行失败: %w", err)
		}
	}

	return s.parseOutput(output)
}

// parseOutput 解析 YARA-X JSON 输出（v1.15+ 格式）
func (s *YARAScanner) parseOutput(output []byte) ([]ScanResult, error) {
	if len(output) == 0 {
		return nil, nil
	}

	var out yaraOutput
	if err := json.Unmarshal(output, &out); err != nil {
		s.logger.Warn("解析 YARA-X JSON 输出失败", zap.Error(err))
		return nil, nil
	}

	var results []ScanResult
	for _, m := range out.Matches {
		threatType := s.extractThreatTypeV2(m.Tags, m.Metadata)
		severity := s.extractSeverityV2(m.Metadata, threatType)

		result := ScanResult{
			FilePath:   m.File,
			ThreatName: m.Rule,
			ThreatType: threatType,
			Severity:   severity,
			Engine:     "yara",
			RuleName:   m.Rule,
			DetectedAt: time.Now(),
		}

		result.FileHash, result.FileSize = getFileInfo(m.File)
		results = append(results, result)
	}

	return results, nil
}

// extractThreatTypeV2 从 YARA-X v1.15+ 规则标签和元数据提取威胁类型
func (s *YARAScanner) extractThreatTypeV2(tags []string, metadata map[string]string) string {
	// 先检查标签
	for _, tag := range tags {
		lower := strings.ToLower(tag)
		switch lower {
		case "ransomware":
			return "ransomware"
		case "rootkit":
			return "rootkit"
		case "backdoor":
			return "backdoor"
		case "trojan":
			return "trojan"
		case "miner", "coinminer":
			return "miner"
		case "worm":
			return "worm"
		case "virus":
			return "virus"
		}
	}

	// 检查元数据中的 threat_type
	if v, ok := metadata["threat_type"]; ok {
		return v
	}

	return "other"
}

// extractSeverityV2 从元数据提取严重级别
func (s *YARAScanner) extractSeverityV2(metadata map[string]string, threatType string) string {
	if v, ok := metadata["severity"]; ok {
		return v
	}
	return getSeverity(threatType)
}
