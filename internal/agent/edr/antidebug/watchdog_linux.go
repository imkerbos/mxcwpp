//go:build linux

package antidebug

// 双进程互保 (Watchdog) 设计。
//
// Agent 启动后 fork 一个 watchdog 子进程, 两进程互相用 prctl(PR_SET_PDEATHSIG)
// + 心跳管道相互监控:
//
//   - 子进程 prctl(PR_SET_PDEATHSIG, SIGKILL): 父进程被 kill, 子立刻 kill 自身
//   - 父进程 wait4(child): 子退出 → 父立刻重启子
//   - 双方通过 pipe 周期发心跳, 超 30s 未收到 → 视对方异常
//
// 攻击者杀单进程 → 另一进程立即检测 + 重启 + 上报告警。
// 真正杀死 Agent 需精确同时 kill 两进程 + 阻断 systemd 重启, 大幅提高门槛。
//
// 本 PR 仅提供 API + 文档骨架, 完整实现 Sprint 5 单独 PR:
//   - WatchdogConfig: 心跳周期 / 重启策略
//   - watchdogProcess: 子进程主循环
//   - heartbeatPipe: 心跳 IPC
//   - systemd ExecStartPre 集成 (preset preset NoNewPrivileges=yes)

import (
	"errors"

	"go.uber.org/zap"
)

// Watchdog 双进程互保管理器。
//
// 当前为骨架 + 接口契约, 完整实现待 Sprint 5。
type Watchdog struct {
	logger *zap.Logger
}

// NewWatchdog 构造。
func NewWatchdog(logger *zap.Logger) *Watchdog {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Watchdog{logger: logger}
}

// Start fork watchdog 子进程 + 设 PDEATHSIG (待 Sprint 5 实现)。
func (w *Watchdog) Start() error {
	w.logger.Info("Watchdog Start() — 骨架 (Sprint 5 完整实现)")
	return errors.New("watchdog not yet implemented (skeleton)")
}

// Stop 优雅停止。
func (w *Watchdog) Stop() error { return nil }
