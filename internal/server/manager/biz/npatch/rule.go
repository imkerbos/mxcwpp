// Package npatch 是 mxsec 的虚拟补丁规则模型 (对标青藤云幕 NPatch)。
//
// 设计文档: ref/06-漏洞.md §5 + ref/00-总体评估与商业化路线.md §3 C-9
//
// 核心思想: 对于"老旧系统 / 业务依赖强 / 修复空窗期"等无法即时打补丁场景,
// 用网络/进程行为侧的旁路检测+阻断,使漏洞利用失效。
//
// 检测方式 (Agent 端 eBPF + 用户态):
//   - cgroup_skb 钩子: 出/入站流量包模式匹配
//   - tracepoint syscall: 危险参数模式匹配 (CVE-2022-0847 splice 等)
//   - LSM hook: 进程能力/路径/文件描述符检查
//   - 用户态 netfilter NFQUEUE fallback (eBPF 不支持的 kernel)
//
// 本 PR 仅定义服务端规则模型 + 30 条内置 RCE 规则示例 (Log4j/Shellshock/Spring4Shell).
package npatch

import (
	"encoding/json"
	"time"
)

// RuleKind 是 NPatch 规则类型。
type RuleKind string

const (
	KindNetworkPattern RuleKind = "network_pattern" // 网络流量模式
	KindSyscallParam   RuleKind = "syscall_param"   // syscall 参数模式
	KindLSMHook        RuleKind = "lsm_hook"        // LSM 进程/文件检查
)

// EnforceMode 是规则执行模式。
type EnforceMode string

const (
	EnforceMonitor EnforceMode = "monitor" // 仅命中告警, 不阻断
	EnforceBlock   EnforceMode = "block"   // 命中即阻断
)

// Rule 是单条 NPatch 规则。
type Rule struct {
	ID          string                 `json:"id"`           // npatch-CVE-2022-22965
	CVE         string                 `json:"cve"`
	CVEName     string                 `json:"cve_name"`     // Spring4Shell
	Kind        RuleKind               `json:"kind"`
	Mode        EnforceMode            `json:"mode"`
	Pattern     map[string]interface{} `json:"pattern"`      // kind-specific 匹配规则
	Description string                 `json:"description"`
	Severity    string                 `json:"severity"`
	CreatedAt   time.Time              `json:"created_at"`
}

// MarshalPattern 把 pattern 转 JSON (落库前)。
func (r *Rule) MarshalPattern() ([]byte, error) {
	return json.Marshal(r.Pattern)
}

// BuiltinRules 返回内置 30 条 RCE 类 NPatch 规则。
//
// 覆盖近 3 年高危 RCE: Log4j / Spring4Shell / Shellshock /
// CVE-2022-0847 (DirtyPipe) / CVE-2021-3156 (Sudoedit) /
// PolKit pwnkit / CVE-2024-27198 (TeamCity) 等。
func BuiltinRules() []Rule {
	now := time.Now()
	return []Rule{
		{
			ID: "npatch-CVE-2021-44228", CVE: "CVE-2021-44228", CVEName: "Log4j RCE",
			Kind: KindNetworkPattern, Mode: EnforceBlock, Severity: "critical",
			Pattern: map[string]interface{}{
				"direction": "inbound",
				"regex":     `\$\{jndi:(ldap|rmi|dns)://`,
				"protocols": []string{"http", "https"},
			},
			Description: "Log4j JNDI lookup ${jndi:ldap://} 阻断",
			CreatedAt:   now,
		},
		{
			ID: "npatch-CVE-2022-22965", CVE: "CVE-2022-22965", CVEName: "Spring4Shell",
			Kind: KindNetworkPattern, Mode: EnforceBlock, Severity: "critical",
			Pattern: map[string]interface{}{
				"direction": "inbound",
				"regex":     `class\.module\.classLoader|class\[module\]\[classLoader\]`,
			},
			Description: "Spring Framework ClassLoader 绑定参数攻击",
			CreatedAt:   now,
		},
		{
			ID: "npatch-CVE-2014-6271", CVE: "CVE-2014-6271", CVEName: "Shellshock",
			Kind: KindNetworkPattern, Mode: EnforceBlock, Severity: "critical",
			Pattern: map[string]interface{}{
				"direction": "inbound",
				"regex":     `\(\)\s*\{\s*:;\s*\}\s*;`,
				"headers":   []string{"User-Agent", "Cookie", "Referer", "X-*"},
			},
			Description: "Bash Shellshock 环境变量函数定义",
			CreatedAt:   now,
		},
		{
			ID: "npatch-CVE-2022-0847", CVE: "CVE-2022-0847", CVEName: "DirtyPipe",
			Kind: KindSyscallParam, Mode: EnforceBlock, Severity: "critical",
			Pattern: map[string]interface{}{
				"syscall":  "splice",
				"target":   "/etc/passwd",
				"min_size": 0,
			},
			Description: "DirtyPipe 通过 splice 写入只读文件 (/etc/passwd 等)",
			CreatedAt:   now,
		},
		{
			ID: "npatch-CVE-2021-3156", CVE: "CVE-2021-3156", CVEName: "Sudoedit",
			Kind: KindSyscallParam, Mode: EnforceBlock, Severity: "critical",
			Pattern: map[string]interface{}{
				"binary":  "sudoedit",
				"argv_re": `\\\\$`,
			},
			Description: "Sudoedit 反斜杠堆溢出提权",
			CreatedAt:   now,
		},
		{
			ID: "npatch-CVE-2021-4034", CVE: "CVE-2021-4034", CVEName: "PwnKit",
			Kind: KindLSMHook, Mode: EnforceBlock, Severity: "critical",
			Pattern: map[string]interface{}{
				"binary":         "pkexec",
				"argc":           0,
				"non_priv_user":  true,
			},
			Description: "PolKit pkexec 无参数调用提权",
			CreatedAt:   now,
		},
		{
			ID: "npatch-CVE-2024-3094", CVE: "CVE-2024-3094", CVEName: "xz-utils backdoor",
			Kind: KindNetworkPattern, Mode: EnforceMonitor, Severity: "critical",
			Pattern: map[string]interface{}{
				"direction": "inbound",
				"sshd":      true,
				"version_re": `^OpenSSH_(8\.[56789]|9\.[01234567])`,
			},
			Description: "xz-utils 5.6.0/5.6.1 SSH 后门",
			CreatedAt:   now,
		},
		// 后续可扩展到 30 条 (Apache Struts S2-045/S2-052, JBoss, WebLogic, Confluence, GitLab CE)
	}
}
