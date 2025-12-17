// Package engine 提供基线检查器实现
package engine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"go.uber.org/zap"
)

// FileKVChecker 检查配置文件键值对
type FileKVChecker struct {
	logger *zap.Logger
}

// NewFileKVChecker 创建文件键值检查器
func NewFileKVChecker(logger *zap.Logger) *FileKVChecker {
	return &FileKVChecker{logger: logger}
}

// Check 执行检查
func (c *FileKVChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 3 {
		return nil, fmt.Errorf("file_kv requires 3 parameters: [file_path, key, expected_value]")
	}

	filePath := rule.Param[0]
	key := rule.Param[1]
	expected := rule.Param[2]

	// 读取文件
	file, err := os.Open(filePath)
	if err != nil {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("文件不存在或无法读取: %v", err),
			Expected: fmt.Sprintf("文件存在且包含 %s=%s", key, expected),
		}, nil
	}
	defer file.Close()

	// 解析键值对
	scanner := bufio.NewScanner(file)
	var actualValue string
	found := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳过注释行和空行
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		// 支持多种格式：Key Value, Key=Value, Key: Value
		// 先检查 Key=Value 格式
		if strings.Contains(line, "=") {
			kvParts := strings.SplitN(line, "=", 2)
			if len(kvParts) == 2 {
				keyPart := strings.TrimSpace(kvParts[0])
				if strings.EqualFold(keyPart, key) {
					actualValue = strings.TrimSpace(kvParts[1])
					found = true
					break
				}
			}
		}

		// 如果不是 Key=Value 格式，尝试 Key Value 格式
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			if strings.EqualFold(parts[0], key) {
				actualValue = strings.Join(parts[1:], " ")
				found = true
				break
			}
		}
	}

	if !found {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("未找到键: %s", key),
			Expected: fmt.Sprintf("%s=%s", key, expected),
		}, nil
	}

	// 比较值（支持正则匹配）
	matched, err := regexp.MatchString(expected, actualValue)
	if err != nil {
		// 如果正则无效，使用精确匹配
		matched = strings.EqualFold(actualValue, expected)
	}

	if matched {
		return &CheckResult{
			Pass:     true,
			Actual:   fmt.Sprintf("%s=%s", key, actualValue),
			Expected: fmt.Sprintf("%s=%s", key, expected),
		}, nil
	}

	return &CheckResult{
		Pass:     false,
		Actual:   fmt.Sprintf("%s=%s", key, actualValue),
		Expected: fmt.Sprintf("%s=%s", key, expected),
	}, nil
}

// FileExistsChecker 检查文件是否存在
type FileExistsChecker struct {
	logger *zap.Logger
}

// NewFileExistsChecker 创建文件存在检查器
func NewFileExistsChecker(logger *zap.Logger) *FileExistsChecker {
	return &FileExistsChecker{logger: logger}
}

// Check 执行检查
func (c *FileExistsChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 1 {
		return nil, fmt.Errorf("file_exists requires 1 parameter: [file_path]")
	}

	filePath := rule.Param[0]
	_, err := os.Stat(filePath)

	if err == nil {
		return &CheckResult{
			Pass:     true,
			Actual:   fmt.Sprintf("文件存在: %s", filePath),
			Expected: fmt.Sprintf("文件存在: %s", filePath),
		}, nil
	}

	return &CheckResult{
		Pass:     false,
		Actual:   fmt.Sprintf("文件不存在: %s", filePath),
		Expected: fmt.Sprintf("文件存在: %s", filePath),
	}, nil
}

// FilePermissionChecker 检查文件权限
type FilePermissionChecker struct {
	logger *zap.Logger
}

// NewFilePermissionChecker 创建文件权限检查器
func NewFilePermissionChecker(logger *zap.Logger) *FilePermissionChecker {
	return &FilePermissionChecker{logger: logger}
}

// Check 执行检查
func (c *FilePermissionChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 2 {
		return nil, fmt.Errorf("file_permission requires 2 parameters: [file_path, min_permission]")
	}

	filePath := rule.Param[0]
	minPermStr := rule.Param[1]

	// 解析最小权限（8进制）
	minPerm, err := strconv.ParseUint(minPermStr, 8, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid permission format: %s (should be octal, e.g., 644)", minPermStr)
	}

	// 获取文件信息
	info, err := os.Stat(filePath)
	if err != nil {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("文件不存在或无法访问: %v", err),
			Expected: fmt.Sprintf("文件权限 <= %s", minPermStr),
		}, nil
	}

	// 获取文件权限（8进制）
	filePerm := uint32(info.Mode().Perm())

	// 检查权限（实际权限应该 <= 最小权限，即更严格）
	// 例如：如果最小权限是 644，那么实际权限应该是 644 或更严格（如 600）
	if filePerm <= uint32(minPerm) {
		return &CheckResult{
			Pass:     true,
			Actual:   fmt.Sprintf("权限: %o", filePerm),
			Expected: fmt.Sprintf("权限 <= %s", minPermStr),
		}, nil
	}

	return &CheckResult{
		Pass:     false,
		Actual:   fmt.Sprintf("权限: %o", filePerm),
		Expected: fmt.Sprintf("权限 <= %s", minPermStr),
	}, nil
}

// CommandExecChecker 执行命令检查
type CommandExecChecker struct {
	logger *zap.Logger
}

// NewCommandExecChecker 创建命令执行检查器
func NewCommandExecChecker(logger *zap.Logger) *CommandExecChecker {
	return &CommandExecChecker{logger: logger}
}

// Check 执行检查
func (c *CommandExecChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 2 {
		return nil, fmt.Errorf("command_exec requires 2 parameters: [command, expected_output]")
	}

	command := rule.Param[0]
	expected := rule.Param[1]

	// 执行命令
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("命令执行失败: %v", err),
			Expected: expected,
		}, nil
	}

	actual := strings.TrimSpace(string(output))

	// 匹配输出（支持正则）
	matched, err := regexp.MatchString(expected, actual)
	if err != nil {
		// 如果正则无效，使用精确匹配
		matched = strings.EqualFold(actual, expected)
	}

	if matched {
		return &CheckResult{
			Pass:     true,
			Actual:   actual,
			Expected: expected,
		}, nil
	}

	return &CheckResult{
		Pass:     false,
		Actual:   actual,
		Expected: expected,
	}, nil
}

// FileLineMatchChecker 检查文件行匹配
type FileLineMatchChecker struct {
	logger *zap.Logger
}

// NewFileLineMatchChecker 创建文件行匹配检查器
func NewFileLineMatchChecker(logger *zap.Logger) *FileLineMatchChecker {
	return &FileLineMatchChecker{logger: logger}
}

// Check 执行检查
func (c *FileLineMatchChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 2 {
		return nil, fmt.Errorf("file_line_match requires at least 2 parameters: [file_path, pattern]")
	}

	filePath := rule.Param[0]
	pattern := rule.Param[1]
	expectedMatch := true // 默认期望匹配
	if len(rule.Param) >= 3 {
		// 第三个参数可以是 "match" 或 "not_match"
		if rule.Param[2] == "not_match" {
			expectedMatch = false
		}
	}

	// 读取文件
	file, err := os.Open(filePath)
	if err != nil {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("文件不存在或无法读取: %v", err),
			Expected: fmt.Sprintf("文件存在且包含匹配行: %s", pattern),
		}, nil
	}
	defer file.Close()

	// 编译正则表达式
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %s, error: %w", pattern, err)
	}

	// 扫描文件行
	scanner := bufio.NewScanner(file)
	lineNum := 0
	var matchedLines []string

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// 检查是否匹配
		if regex.MatchString(line) {
			matchedLines = append(matchedLines, fmt.Sprintf("line %d: %s", lineNum, line))
		}
	}

	if err := scanner.Err(); err != nil {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("读取文件时出错: %v", err),
			Expected: fmt.Sprintf("文件可正常读取且包含匹配行: %s", pattern),
		}, nil
	}

	// 判断结果
	hasMatch := len(matchedLines) > 0
	pass := (hasMatch && expectedMatch) || (!hasMatch && !expectedMatch)

	actual := ""
	if hasMatch {
		actual = fmt.Sprintf("找到 %d 个匹配行: %s", len(matchedLines), strings.Join(matchedLines[:min(3, len(matchedLines))], "; "))
		if len(matchedLines) > 3 {
			actual += fmt.Sprintf(" ... (共 %d 行)", len(matchedLines))
		}
	} else {
		actual = "未找到匹配行"
	}

	expected := ""
	if expectedMatch {
		expected = fmt.Sprintf("文件应包含匹配行: %s", pattern)
	} else {
		expected = fmt.Sprintf("文件不应包含匹配行: %s", pattern)
	}

	return &CheckResult{
		Pass:     pass,
		Actual:   actual,
		Expected: expected,
	}, nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SysctlChecker 检查内核参数
type SysctlChecker struct {
	logger *zap.Logger
}

// NewSysctlChecker 创建内核参数检查器
func NewSysctlChecker(logger *zap.Logger) *SysctlChecker {
	return &SysctlChecker{logger: logger}
}

// Check 执行检查
func (c *SysctlChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 2 {
		return nil, fmt.Errorf("sysctl requires 2 parameters: [key, expected_value]")
	}

	key := rule.Param[0]
	expected := rule.Param[1]

	// 读取 sysctl 值
	cmd := exec.CommandContext(ctx, "sysctl", "-n", key)
	output, err := cmd.Output()
	if err != nil {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("无法读取 sysctl 参数 %s: %v", key, err),
			Expected: fmt.Sprintf("%s=%s", key, expected),
		}, nil
	}

	actual := strings.TrimSpace(string(output))

	// 比较值（支持正则匹配）
	matched, err := regexp.MatchString(expected, actual)
	if err != nil {
		// 如果正则无效，使用精确匹配
		matched = strings.EqualFold(actual, expected)
	}

	if matched {
		return &CheckResult{
			Pass:     true,
			Actual:   fmt.Sprintf("%s=%s", key, actual),
			Expected: fmt.Sprintf("%s=%s", key, expected),
		}, nil
	}

	return &CheckResult{
		Pass:     false,
		Actual:   fmt.Sprintf("%s=%s", key, actual),
		Expected: fmt.Sprintf("%s=%s", key, expected),
	}, nil
}

// ServiceStatusChecker 检查服务状态
type ServiceStatusChecker struct {
	logger *zap.Logger
}

// NewServiceStatusChecker 创建服务状态检查器
func NewServiceStatusChecker(logger *zap.Logger) *ServiceStatusChecker {
	return &ServiceStatusChecker{logger: logger}
}

// Check 执行检查
func (c *ServiceStatusChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 2 {
		return nil, fmt.Errorf("service_status requires 2 parameters: [service_name, expected_status]")
	}

	serviceName := rule.Param[0]
	expectedStatus := strings.ToLower(rule.Param[1]) // active, inactive, enabled, disabled 等

	// 尝试使用 systemctl（systemd）
	actualStatus, err := c.checkSystemdService(ctx, serviceName)
	if err != nil {
		// 如果 systemctl 失败，尝试使用 service 命令（SysV）
		actualStatus, err = c.checkSysVService(ctx, serviceName)
		if err != nil {
			return &CheckResult{
				Pass:     false,
				Actual:   fmt.Sprintf("无法检查服务状态: %v", err),
				Expected: fmt.Sprintf("服务 %s 状态应为: %s", serviceName, expectedStatus),
			}, nil
		}
	}

	// 规范化状态值
	normalizedActual := c.normalizeStatus(actualStatus)
	normalizedExpected := c.normalizeStatus(expectedStatus)

	pass := strings.EqualFold(normalizedActual, normalizedExpected)

	return &CheckResult{
		Pass:     pass,
		Actual:   fmt.Sprintf("服务 %s 状态: %s", serviceName, normalizedActual),
		Expected: fmt.Sprintf("服务 %s 状态应为: %s", serviceName, normalizedExpected),
	}, nil
}

// checkSystemdService 检查 systemd 服务状态
func (c *ServiceStatusChecker) checkSystemdService(ctx context.Context, serviceName string) (string, error) {
	// 检查服务是否活跃（is-active）
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", serviceName)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	activeStatus := strings.TrimSpace(string(output))

	// 检查服务是否启用（is-enabled）
	cmd = exec.CommandContext(ctx, "systemctl", "is-enabled", serviceName)
	output, err = cmd.Output()
	if err != nil {
		// 如果 is-enabled 失败，只返回 active 状态
		return activeStatus, nil
	}
	enabledStatus := strings.TrimSpace(string(output))

	// 组合状态：active+enabled, active+disabled, inactive+enabled, inactive+disabled
	return fmt.Sprintf("%s+%s", activeStatus, enabledStatus), nil
}

// checkSysVService 检查 SysV 服务状态
func (c *ServiceStatusChecker) checkSysVService(ctx context.Context, serviceName string) (string, error) {
	// 使用 service 命令检查状态
	cmd := exec.CommandContext(ctx, "service", serviceName, "status")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	outputStr := strings.ToLower(string(output))
	if strings.Contains(outputStr, "running") {
		return "active", nil
	}
	return "inactive", nil
}

// normalizeStatus 规范化状态值
func (c *ServiceStatusChecker) normalizeStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))

	// 处理组合状态（如 "active+enabled"）
	if strings.Contains(status, "+") {
		parts := strings.Split(status, "+")
		if len(parts) == 2 {
			active := parts[0]
			enabled := parts[1]
			if active == "active" && enabled == "enabled" {
				return "active+enabled"
			}
			if active == "active" && enabled == "disabled" {
				return "active+disabled"
			}
			if active == "inactive" && enabled == "enabled" {
				return "inactive+enabled"
			}
			if active == "inactive" && enabled == "disabled" {
				return "inactive+disabled"
			}
		}
	}

	// 处理单个状态值
	switch status {
	case "active", "running", "started":
		return "active"
	case "inactive", "stopped", "dead":
		return "inactive"
	case "enabled":
		return "enabled"
	case "disabled":
		return "disabled"
	default:
		return status
	}
}

// FileOwnerChecker 检查文件属主
type FileOwnerChecker struct {
	logger *zap.Logger
}

// NewFileOwnerChecker 创建文件属主检查器
func NewFileOwnerChecker(logger *zap.Logger) *FileOwnerChecker {
	return &FileOwnerChecker{logger: logger}
}

// Check 执行检查
func (c *FileOwnerChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 2 {
		return nil, fmt.Errorf("file_owner requires 2 parameters: [file_path, expected_owner]")
	}

	filePath := rule.Param[0]
	expectedOwner := rule.Param[1]

	// 获取文件信息
	info, err := os.Stat(filePath)
	if err != nil {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("文件不存在或无法访问: %v", err),
			Expected: fmt.Sprintf("文件所有者应为: %s", expectedOwner),
		}, nil
	}

	// 获取文件所有者（UID 和 GID）
	stat := info.Sys().(*syscall.Stat_t)
	actualUID := strconv.FormatUint(uint64(stat.Uid), 10)
	actualGID := strconv.FormatUint(uint64(stat.Gid), 10)
	actualOwner := fmt.Sprintf("%s:%s", actualUID, actualGID)

	// 解析用户名和组名（可选，用于更友好的显示）
	username := ""
	groupname := ""
	if u, err := user.LookupId(actualUID); err == nil {
		username = u.Username
	}
	if g, err := user.LookupGroupId(actualGID); err == nil {
		groupname = g.Name
	}

	// 解析期望值（支持 uid:gid 或 username:groupname 格式）
	expectedUID := ""
	expectedGID := ""
	expectedUsername := ""
	expectedGroupname := ""

	// 检查期望值格式
	if strings.Contains(expectedOwner, ":") {
		parts := strings.Split(expectedOwner, ":")
		if len(parts) == 2 {
			// 尝试解析为数字（uid:gid）
			if _, err := strconv.ParseUint(parts[0], 10, 32); err == nil {
				expectedUID = parts[0]
				if _, err := strconv.ParseUint(parts[1], 10, 32); err == nil {
					expectedGID = parts[1]
				} else {
					// 可能是 uid:groupname
					expectedGroupname = parts[1]
				}
			} else {
				// 可能是 username:groupname
				expectedUsername = parts[0]
				expectedGroupname = parts[1]
			}
		}
	} else {
		// 只有 UID 或用户名
		if _, err := strconv.ParseUint(expectedOwner, 10, 32); err == nil {
			expectedUID = expectedOwner
		} else {
			expectedUsername = expectedOwner
		}
	}

	// 比较所有者
	pass := false
	actualDisplay := actualOwner
	expectedDisplay := expectedOwner

	// 如果提供了用户名，尝试解析
	if expectedUsername != "" {
		if u, err := user.Lookup(expectedUsername); err == nil {
			expectedUID = u.Uid
		}
	}
	if expectedGroupname != "" {
		if g, err := user.LookupGroup(expectedGroupname); err == nil {
			expectedGID = g.Gid
		}
	}

	// 比较 UID 和 GID
	if expectedUID != "" && expectedGID != "" {
		pass = (actualUID == expectedUID && actualGID == expectedGID)
		actualDisplay = fmt.Sprintf("%s:%s", actualUID, actualGID)
		if username != "" && groupname != "" {
			actualDisplay += fmt.Sprintf(" (%s:%s)", username, groupname)
		}
		expectedDisplay = fmt.Sprintf("%s:%s", expectedUID, expectedGID)
	} else if expectedUID != "" {
		pass = (actualUID == expectedUID)
		actualDisplay = actualUID
		if username != "" {
			actualDisplay += fmt.Sprintf(" (%s)", username)
		}
		expectedDisplay = expectedUID
	} else {
		// 如果无法解析期望值，使用字符串比较
		pass = (actualOwner == expectedOwner)
	}

	return &CheckResult{
		Pass:     pass,
		Actual:   fmt.Sprintf("所有者: %s", actualDisplay),
		Expected: fmt.Sprintf("所有者应为: %s", expectedDisplay),
	}, nil
}

// PackageInstalledChecker 检查软件包是否安装
type PackageInstalledChecker struct {
	logger *zap.Logger
}

// NewPackageInstalledChecker 创建软件包安装检查器
func NewPackageInstalledChecker(logger *zap.Logger) *PackageInstalledChecker {
	return &PackageInstalledChecker{logger: logger}
}

// Check 执行检查
func (c *PackageInstalledChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
	if len(rule.Param) < 1 {
		return nil, fmt.Errorf("package_installed requires at least 1 parameter: [package_name]")
	}

	packageName := rule.Param[0]
	versionConstraint := ""
	if len(rule.Param) >= 2 {
		versionConstraint = rule.Param[1]
	}

	// 检测包管理器类型
	var installed bool
	var installedVersion string

	// 尝试 RPM（CentOS/Rocky/Oracle）
	if _, err := exec.LookPath("rpm"); err == nil {
		var err error
		installed, installedVersion, err = c.checkRPMPackage(ctx, packageName)
		if err != nil {
			return &CheckResult{
				Pass:     false,
				Actual:   fmt.Sprintf("检查 RPM 包失败: %v", err),
				Expected: fmt.Sprintf("软件包 %s 应已安装", packageName),
			}, nil
		}
	} else if _, err := exec.LookPath("dpkg"); err == nil {
		// DEB 系统（Debian/Ubuntu）
		var err error
		installed, installedVersion, err = c.checkDEBPackage(ctx, packageName)
		if err != nil {
			return &CheckResult{
				Pass:     false,
				Actual:   fmt.Sprintf("检查 DEB 包失败: %v", err),
				Expected: fmt.Sprintf("软件包 %s 应已安装", packageName),
			}, nil
		}
	} else {
		return &CheckResult{
			Pass:     false,
			Actual:   "无法检测包管理器（未找到 rpm 或 dpkg）",
			Expected: fmt.Sprintf("软件包 %s 应已安装", packageName),
		}, nil
	}

	// 检查是否安装
	if !installed {
		return &CheckResult{
			Pass:     false,
			Actual:   fmt.Sprintf("软件包 %s 未安装", packageName),
			Expected: fmt.Sprintf("软件包 %s 应已安装", packageName),
		}, nil
	}

	// 如果提供了版本约束，检查版本
	if versionConstraint != "" {
		versionMatched, err := c.compareVersion(installedVersion, versionConstraint)
		if err != nil {
			return &CheckResult{
				Pass:     false,
				Actual:   fmt.Sprintf("版本比较失败: %v", err),
				Expected: fmt.Sprintf("软件包 %s 版本应满足: %s", packageName, versionConstraint),
			}, nil
		}

		if !versionMatched {
			return &CheckResult{
				Pass:     false,
				Actual:   fmt.Sprintf("软件包 %s 已安装，版本: %s", packageName, installedVersion),
				Expected: fmt.Sprintf("软件包 %s 版本应满足: %s", packageName, versionConstraint),
			}, nil
		}

		return &CheckResult{
			Pass:     true,
			Actual:   fmt.Sprintf("软件包 %s 已安装，版本: %s (满足约束: %s)", packageName, installedVersion, versionConstraint),
			Expected: fmt.Sprintf("软件包 %s 版本应满足: %s", packageName, versionConstraint),
		}, nil
	}

	return &CheckResult{
		Pass:     true,
		Actual:   fmt.Sprintf("软件包 %s 已安装，版本: %s", packageName, installedVersion),
		Expected: fmt.Sprintf("软件包 %s 应已安装", packageName),
	}, nil
}

// checkRPMPackage 检查 RPM 包是否安装
func (c *PackageInstalledChecker) checkRPMPackage(ctx context.Context, packageName string) (bool, string, error) {
	// 使用 rpm -q 查询包
	cmd := exec.CommandContext(ctx, "rpm", "-q", packageName)
	output, err := cmd.Output()
	if err != nil {
		// 如果命令返回错误，通常表示包未安装
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return false, "", nil
		}
		return false, "", err
	}

	// 解析输出（格式：package-name-version-release）
	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return false, "", nil
	}

	// 提取版本信息（简化处理，假设格式为 name-version-release）
	parts := strings.Split(outputStr, "-")
	if len(parts) >= 2 {
		// 版本通常是倒数第二个部分
		version := parts[len(parts)-2]
		return true, version, nil
	}

	return true, outputStr, nil
}

// checkDEBPackage 检查 DEB 包是否安装
func (c *PackageInstalledChecker) checkDEBPackage(ctx context.Context, packageName string) (bool, string, error) {
	// 使用 dpkg -l 查询包
	cmd := exec.CommandContext(ctx, "dpkg", "-l", packageName)
	output, err := cmd.Output()
	if err != nil {
		return false, "", err
	}

	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")

	// 查找包信息行（格式：ii  package-name  version  architecture  description）
	for _, line := range lines {
		if strings.HasPrefix(line, "ii") {
			// 解析行
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				// 检查包名是否匹配
				if fields[1] == packageName {
					version := fields[2]
					return true, version, nil
				}
			}
		}
	}

	return false, "", nil
}

// compareVersion 比较版本（支持 >=、<=、==、>、<）
func (c *PackageInstalledChecker) compareVersion(actual, constraint string) (bool, error) {
	constraint = strings.TrimSpace(constraint)
	actual = strings.TrimSpace(actual)

	// 支持 >= 前缀
	if strings.HasPrefix(constraint, ">=") {
		version := strings.TrimSpace(constraint[2:])
		return c.compareVersionValues(actual, version) >= 0, nil
	}

	// 支持 > 前缀
	if strings.HasPrefix(constraint, ">") {
		version := strings.TrimSpace(constraint[1:])
		return c.compareVersionValues(actual, version) > 0, nil
	}

	// 支持 <= 前缀
	if strings.HasPrefix(constraint, "<=") {
		version := strings.TrimSpace(constraint[2:])
		return c.compareVersionValues(actual, version) <= 0, nil
	}

	// 支持 < 前缀
	if strings.HasPrefix(constraint, "<") {
		version := strings.TrimSpace(constraint[1:])
		return c.compareVersionValues(actual, version) < 0, nil
	}

	// 支持 == 前缀或精确匹配
	if strings.HasPrefix(constraint, "==") {
		version := strings.TrimSpace(constraint[2:])
		return c.compareVersionValues(actual, version) == 0, nil
	}

	// 精确匹配
	return c.compareVersionValues(actual, constraint) == 0, nil
}

// compareVersionValues 比较版本值（返回 -1、0、1）
func (c *PackageInstalledChecker) compareVersionValues(v1, v2 string) int {
	// 简化实现：按点号分割版本号，逐段比较
	v1Parts := strings.Split(v1, ".")
	v2Parts := strings.Split(v2, ".")

	maxLen := len(v1Parts)
	if len(v2Parts) > maxLen {
		maxLen = len(v2Parts)
	}

	for i := 0; i < maxLen; i++ {
		var v1Num, v2Num int
		if i < len(v1Parts) {
			// 提取数字部分（忽略非数字字符）
			v1Part := v1Parts[i]
			for j := 0; j < len(v1Part); j++ {
				if v1Part[j] >= '0' && v1Part[j] <= '9' {
					fmt.Sscanf(v1Part[j:], "%d", &v1Num)
					break
				}
			}
		}
		if i < len(v2Parts) {
			v2Part := v2Parts[i]
			for j := 0; j < len(v2Part); j++ {
				if v2Part[j] >= '0' && v2Part[j] <= '9' {
					fmt.Sscanf(v2Part[j:], "%d", &v2Num)
					break
				}
			}
		}

		if v1Num < v2Num {
			return -1
		}
		if v1Num > v2Num {
			return 1
		}
	}

	return 0
}
