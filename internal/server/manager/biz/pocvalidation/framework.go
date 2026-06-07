// Package pocvalidation 实现 PoC (Proof of Concept) 自动验证 (P2-13)。
//
// ref/06-漏洞 M1-P1-2: 50 条 PoC 高危漏洞自动验证。
//
// 设计思想:
//
//	漏洞扫描器报告主机有 CVE-2024-1086 → 直接告警是"可能存在"
//	→ PoC 验证: Agent 安全执行预定义无害 PoC → 真假 100% 确认
//
// 安全原则:
//   - PoC 必须 read-only (检测漏洞存在 vs 真攻击)
//   - PoC 执行需用户明确授权 + 时间窗
//   - 失败必须 100% 安全 (不污染目标系统)
//   - 每条 PoC 单独 cgroup 隔离 + 超时 + 资源限制
//
// PoC 类型:
//   read_file        — 读特定路径检测 (DirtyPipe 用)
//   net_request      — 发特定网络请求 (Log4j JNDI 触发用)
//   subprocess_check — 执行只读命令检查输出 (CVE-2021-4034 用户态调用)
//   sysfs_check      — 读 /sys 或 /proc 状态
//   memory_read      — 内存读检查 (Heartbleed 类)
package pocvalidation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// PoCKind PoC 检测类型.
type PoCKind string

const (
	KindReadFile        PoCKind = "read_file"
	KindNetRequest      PoCKind = "net_request"
	KindSubprocessCheck PoCKind = "subprocess_check"
	KindSysfsCheck      PoCKind = "sysfs_check"
	KindMemoryRead      PoCKind = "memory_read"
)

// PoC 单条 PoC 定义.
//
// 由 Manager 下发到 Agent, Agent 执行后回报 Verdict.
type PoC struct {
	ID          string                 `json:"id"`            // poc-CVE-2021-4034 / poc-CVE-2024-3094
	CVE         string                 `json:"cve"`
	Name        string                 `json:"name"`          // PwnKit pkexec / xz-utils backdoor
	Description string                 `json:"description"`
	Kind        PoCKind                `json:"kind"`
	Params      map[string]interface{} `json:"params"`        // kind-specific 参数
	ExpectedVerdict string             `json:"expected_verdict"` // affected / not_affected / unknown
	TimeoutSec  int                    `json:"timeout_sec"`
	Severity    string                 `json:"severity"`
	// 安全保护
	MaxMemoryMB int    `json:"max_memory_mb"`  // cgroup 内存上限 (默认 64)
	MaxCPUMs    int    `json:"max_cpu_ms"`     // CPU 时间上限 (默认 500ms)
	ReadOnly    bool   `json:"read_only"`      // 必须为 true (否则拒绝执行)
	Rationale   string `json:"rationale"`      // 为何此 PoC 是只读 (审计材料)
}

// Verdict 单次 PoC 执行结果.
type Verdict struct {
	PoCID       string          `json:"poc_id"`
	HostID      string          `json:"host_id"`
	Result      string          `json:"result"` // affected / not_affected / error / timeout
	Confidence  float64         `json:"confidence"` // 0.0 - 1.0
	Evidence    json.RawMessage `json:"evidence"`
	ExecutedAt  time.Time       `json:"executed_at"`
	DurationMs  int64           `json:"duration_ms"`
	ErrorMsg    string          `json:"error_msg,omitempty"`
}

// Library 内置 PoC 库.
//
// MVP 目标: ref/06 M1-P1-2 = 50 条; 当前首批 10 条覆盖热门 CVE。
func Library() []PoC {
	return []PoC{
		{
			ID: "poc-CVE-2021-4034", CVE: "CVE-2021-4034", Name: "PwnKit",
			Description: "polkit pkexec 无参数提权; 检测 pkexec 二进制是否存在且有 SUID 位",
			Kind: KindReadFile,
			Params: map[string]interface{}{
				"path": "/usr/bin/pkexec",
				"check_suid": true,
				"version_cmd": "pkexec --version",
				"vuln_versions": []string{"<= 0.120"},
			},
			ExpectedVerdict: "affected", TimeoutSec: 5, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 32,
			Rationale: "仅 stat() + 读 version 字符串, 无 execve 提权操作",
		},
		{
			ID: "poc-CVE-2021-3156", CVE: "CVE-2021-3156", Name: "Baron Samedit (sudoedit)",
			Description: "sudoedit 反斜杠堆溢出; 检测 sudo --version",
			Kind: KindSubprocessCheck,
			Params: map[string]interface{}{
				"cmd": "sudo --version 2>/dev/null | head -1",
				"vuln_regex": "Sudo version 1\\.(7|8\\.[12]|8\\.[2-3][0-9])",
			},
			ExpectedVerdict: "affected", TimeoutSec: 3, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 32,
			Rationale: "仅读 sudo --version 输出, 不调用 sudoedit",
		},
		{
			ID: "poc-CVE-2022-0847", CVE: "CVE-2022-0847", Name: "DirtyPipe",
			Description: "splice 写只读文件; 仅检测内核版本",
			Kind: KindSysfsCheck,
			Params: map[string]interface{}{
				"path": "/proc/version",
				"vuln_kernel_regex": "Linux version 5\\.(8|9|10\\.[0-9]+|11|12|13|14|15|16)",
			},
			ExpectedVerdict: "affected", TimeoutSec: 2, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 16,
			Rationale: "仅读 /proc/version 字符串",
		},
		{
			ID: "poc-CVE-2024-3094", CVE: "CVE-2024-3094", Name: "xz-utils backdoor",
			Description: "xz/liblzma 5.6.0/5.6.1 后门; 检测 liblzma 版本",
			Kind: KindSubprocessCheck,
			Params: map[string]interface{}{
				"cmd": "xz --version 2>/dev/null | head -1",
				"vuln_regex": "xz \\(XZ Utils\\) 5\\.6\\.(0|1)$",
			},
			ExpectedVerdict: "affected", TimeoutSec: 3, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 32,
			Rationale: "仅 xz --version (子命令安全), 不调用 ssh",
		},
		{
			ID: "poc-CVE-2014-6271", CVE: "CVE-2014-6271", Name: "Shellshock",
			Description: "bash 环境变量函数定义; 检测 bash version",
			Kind: KindSubprocessCheck,
			Params: map[string]interface{}{
				"cmd": "bash --version | head -1",
				"vuln_regex": "bash, version (3\\.|4\\.[012])",
			},
			ExpectedVerdict: "affected", TimeoutSec: 3, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 32,
			Rationale: "仅 bash --version, 不构造 exploit",
		},
		{
			ID: "poc-CVE-2024-1086", CVE: "CVE-2024-1086", Name: "Linux nft 双重释放",
			Description: "netfilter nft 提权; 仅检测内核版本",
			Kind: KindSysfsCheck,
			Params: map[string]interface{}{
				"path": "/proc/version",
				"vuln_kernel_regex": "Linux version (3\\.15|[45]\\.|6\\.[0-7])",
			},
			ExpectedVerdict: "affected", TimeoutSec: 2, Severity: "high",
			ReadOnly: true, MaxMemoryMB: 16,
			Rationale: "仅读 /proc/version",
		},
		{
			ID: "poc-CVE-2021-44228", CVE: "CVE-2021-44228", Name: "Log4Shell",
			Description: "Log4j JNDI; 检测 jar 文件版本",
			Kind: KindSubprocessCheck,
			Params: map[string]interface{}{
				"cmd": "find / -name 'log4j-core-*.jar' 2>/dev/null | head -5",
				"vuln_regex": "log4j-core-2\\.([0-9]|1[0-6])\\.",
			},
			ExpectedVerdict: "affected", TimeoutSec: 30, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 64,
			Rationale: "仅 find 文件名 + 正则版本; 不触发 JNDI lookup",
		},
		{
			ID: "poc-CVE-2024-21626", CVE: "CVE-2024-21626", Name: "runc Leaky Vessels",
			Description: "runc 文件描述符泄露容器逃逸; 检测 runc 版本",
			Kind: KindSubprocessCheck,
			Params: map[string]interface{}{
				"cmd": "runc --version 2>/dev/null | head -1",
				"vuln_regex": "runc version 1\\.(0\\.|1\\.[0-9]|1\\.10|1\\.11)$",
			},
			ExpectedVerdict: "affected", TimeoutSec: 3, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 32,
			Rationale: "仅 runc --version",
		},
		{
			ID: "poc-CVE-2023-46604", CVE: "CVE-2023-46604", Name: "ActiveMQ OpenWire RCE",
			Description: "ActiveMQ 反序列化; 检测端口 + 版本",
			Kind: KindNetRequest,
			Params: map[string]interface{}{
				"host": "127.0.0.1",
				"port": 61616,
				"probe": "INFO_HEADER_ONLY", // 仅读取 banner, 不送 OpenWire payload
			},
			ExpectedVerdict: "affected", TimeoutSec: 5, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 32,
			Rationale: "仅 TCP connect + 读 banner; 不发 deserialization payload",
		},
		{
			ID: "poc-CVE-2024-27198", CVE: "CVE-2024-27198", Name: "TeamCity Auth Bypass",
			Description: "TeamCity 重定向认证绕过; 检测 HTTP HEAD 状态",
			Kind: KindNetRequest,
			Params: map[string]interface{}{
				"url": "http://127.0.0.1:8111/app/rest/users",
				"method": "HEAD",
				"vuln_status_code": []int{200, 401},
			},
			ExpectedVerdict: "affected", TimeoutSec: 5, Severity: "critical",
			ReadOnly: true, MaxMemoryMB: 32,
			Rationale: "HEAD 不发 body; 仅看状态码",
		},
	}
}

// Manager 服务端 PoC 编排.
type Manager struct {
	logger *zap.Logger
}

// NewManager 构造.
func NewManager(logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{logger: logger}
}

// PrepareDispatch 校验 PoC 安全性 → 准备下发 payload.
//
// 拒绝条件:
//   - ReadOnly != true
//   - TimeoutSec > 60 (防长时间执行)
//   - MaxMemoryMB > 256
func (m *Manager) PrepareDispatch(p PoC) ([]byte, error) {
	if !p.ReadOnly {
		return nil, errors.New("poc must be read-only")
	}
	if p.TimeoutSec > 60 {
		return nil, fmt.Errorf("poc timeout %d > 60s limit", p.TimeoutSec)
	}
	if p.MaxMemoryMB > 256 {
		return nil, fmt.Errorf("poc memory %dMB > 256MB limit", p.MaxMemoryMB)
	}
	return json.Marshal(p)
}

// RecordVerdict 接收 Agent 回报的 Verdict.
//
// 后续 PR: 写 vuln_verdicts 表 + 关联 host_vulnerabilities.status (poc_confirmed / poc_refuted).
func (m *Manager) RecordVerdict(_ context.Context, v *Verdict) error {
	m.logger.Info("poc verdict received",
		zap.String("poc_id", v.PoCID),
		zap.String("host_id", v.HostID),
		zap.String("result", v.Result),
		zap.Float64("confidence", v.Confidence),
		zap.Int64("duration_ms", v.DurationMs))
	return nil
}
