"use client";
import { clsx } from "clsx";

// MITRE ATT&CK 战术覆盖热力。我们告警带 attck_tactic，P2 直接聚合真实计数。
type Tactic = { id: string; name: string; count: number };

// ⚠️ P1 mock 计数。
const TACTICS: Tactic[] = [
  { id: "TA0001", name: "初始访问", count: 6 },
  { id: "TA0002", name: "执行", count: 1 },
  { id: "TA0003", name: "持久化", count: 7 },
  { id: "TA0004", name: "提权", count: 15 },
  { id: "TA0005", name: "防御绕过", count: 10 },
  { id: "TA0006", name: "凭据访问", count: 11 },
  { id: "TA0007", name: "发现", count: 38 },
  { id: "TA0008", name: "横向移动", count: 30 },
  { id: "TA0009", name: "收集", count: 3 },
  { id: "TA0010", name: "数据渗出", count: 5 },
  { id: "TA0011", name: "命令控制", count: 2 },
  { id: "TA0040", name: "影响", count: 1 },
];

function heat(count: number, max: number): string {
  if (count === 0) return "bg-slate-700/20 text-slate-500 border-slate-600/20";
  const r = count / max;
  if (r > 0.66) return "bg-rose-500/25 text-rose-200 border-rose-500/40 shadow-[0_0_8px_-2px_rgba(244,63,94,0.6)]";
  if (r > 0.33) return "bg-orange-500/20 text-orange-200 border-orange-500/35";
  return "bg-cyan-500/12 text-cyan-200 border-cyan-500/30";
}

export function AttackMatrix() {
  const max = Math.max(...TACTICS.map((t) => t.count));
  const covered = TACTICS.filter((t) => t.count > 0).length;
  return (
    <div className="flex h-full flex-col">
      <div className="mb-2 flex items-center justify-between text-[11px] text-slate-400">
        <span>MITRE ATT&CK 战术覆盖</span>
        <span className="text-cyan-300">
          命中 {covered}/{TACTICS.length} 战术
        </span>
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-6 gap-1.5">
        {TACTICS.map((t) => (
          <div
            key={t.id}
            className={clsx(
              "flex flex-col items-center justify-center rounded border px-1 py-1 transition-colors",
              heat(t.count, max),
            )}
          >
            <span className="font-mono text-lg font-bold tabular-nums leading-none">{t.count}</span>
            <span className="mt-0.5 truncate text-[9px] leading-tight opacity-80">{t.name}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
