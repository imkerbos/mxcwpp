package config

import (
	"fmt"
	"net"
	"strings"
)

// MinInternalSecretLen 是 internal_secret 的最小长度门槛（商业级要求 ≥32）。
const MinInternalSecretLen = 32

// weakSecretSubstrings 命中即判为弱密钥（模板/常见默认值）。
var weakSecretSubstrings = []string{"change-me", "changeme", "change_me", "changthis", "change-this"}

// weakSecretExact 精确匹配（小写）的常见弱值。
var weakSecretExact = map[string]bool{
	"secret": true, "password": true, "default": true, "test": true, "example": true,
	"mxcwppinternalsecret789": true, // deploy/env.v2.example 历史示例值
}

// ValidateInternalSecret 校验 internal_secret 强度：非空、非模板占位符、非弱默认值、长度 ≥32。
// Manager↔AgentCenter 管理面鉴权与集群渲染共用同一策略。
func ValidateInternalSecret(secret string) error {
	s := strings.TrimSpace(secret)
	if s == "" {
		return fmt.Errorf("internal_secret 为空")
	}
	if strings.Contains(s, "__") {
		return fmt.Errorf("internal_secret 仍是未替换的模板占位符（%q），请生成真实密钥：openssl rand -hex 32", secret)
	}
	low := strings.ToLower(s)
	if weakSecretExact[low] {
		return fmt.Errorf("internal_secret 为常见弱默认值，请生成真实密钥：openssl rand -hex 32")
	}
	for _, w := range weakSecretSubstrings {
		if strings.Contains(low, w) {
			return fmt.Errorf("internal_secret 含明显默认标记（%q），请生成真实密钥：openssl rand -hex 32", w)
		}
	}
	if len(s) < MinInternalSecretLen {
		return fmt.Errorf("internal_secret 长度不足（%d < %d），请生成足够强度的密钥：openssl rand -hex 32", len(s), MinInternalSecretLen)
	}
	return nil
}

// ValidateAgentCenter 校验 AgentCenter 特有的启动前置条件。
//
// 仅由 AgentCenter 初始化调用（Manager 与 AC 共用同一 Config 结构，
// 故此规则不能塞进通用 Config.Validate）。
//
// 网络边界 fail-closed：AC 的 HTTP 管理端口（承载 /command、/dependency/install 等
// 高危接口）一旦绑定到非 loopback 地址（0.0.0.0 / 通配 / 可路由 IP），就必须配置
// 非空、非模板占位符、足够强度的 internal_secret；否则拒绝启动，避免管理面裸奔。
func (c *Config) ValidateAgentCenter() error {
	err := ValidateInternalSecret(c.Server.InternalSecret)
	if err == nil {
		return nil
	}
	// 无论绑定地址如何都必须有强密钥，否则 AC 受保护接口全部 401、注册/命令下发永久失败——
	// fail-fast，不允许“健康但控制链路失效”的静默半失效。非 loopback 绑定额外强调网络风险。
	if !isLoopbackBind(c.Server.HTTP.Host) {
		return fmt.Errorf("AgentCenter HTTP 管理端口绑定到非 loopback 地址 %q（管理面裸奔风险），且 %w", c.Server.HTTP.Address(), err)
	}
	return fmt.Errorf("AgentCenter 要求强 server.internal_secret（否则 /command 等接口全部 401、AC 注册失败）：%w", err)
}

// ValidateManager 校验 Manager 启动前置条件：内部路由（/api/v1/internal/*）鉴权
// 要求强 server.internal_secret，缺失即 fail-fast，避免内部链路静默失效。
// 仅由 Manager 专用初始化路径调用，不影响 Consumer/Engine 等共用 Config 的进程。
func (c *Config) ValidateManager() error {
	if err := ValidateInternalSecret(c.Server.InternalSecret); err != nil {
		return fmt.Errorf("Manager 要求强 server.internal_secret（内部服务路由鉴权 + AC 命令下发）：%w", err)
	}
	return nil
}

// isLoopbackBind 判断监听 host 是否仅回环可达（此时无需强制 internal_secret）。
// 空 host 由 setDefaults 归一为 0.0.0.0（对外可达），按非 loopback 处理。
func isLoopbackBind(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost":
		return true
	case "", "0.0.0.0", "::", "[::]":
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
