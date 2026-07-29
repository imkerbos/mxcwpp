"use client";
import { motion } from "framer-motion";
import { Panel } from "@/components/screen/Panel";
import { ScreenHeader } from "@/components/screen/ScreenHeader";
import { KpiTicker } from "@/components/screen/KpiTicker";
import { PostureGauge, SeverityRing, TrendChart } from "@/components/screen/ScreenCharts";
import { EngineHealthWall, type EngineStat } from "@/components/screen/EngineHealthWall";
import { AlertFeed, type FeedAlert } from "@/components/screen/AlertFeed";
import { AttackMap } from "@/components/screen/AttackMap";
import { AttackMatrix } from "@/components/screen/AttackMatrix";
import { HostRank, ComplianceBar } from "@/components/screen/HostRank";

// ⚠️ P1 骨架：以下为静态 mock 数据，仅供查看大屏视觉效果。
// P2 接后端 stats 端点 + SSE 告警流，P3 接 GeoIP 攻击地图。

const ENGINES: EngineStat[] = [
  { key: "edr", name: "EDR 检测", count: 148, unit: "活跃告警", status: "healthy" },
  { key: "bde", name: "行为引擎", count: 478, unit: "待处置", status: "healthy" },
  { key: "ml", name: "ML 异常", count: 3791, unit: "critical", status: "warn" },
  { key: "fim", name: "文件完整性", count: 96413, unit: "24h 事件", status: "healthy" },
  { key: "kube", name: "K8s 基线", count: 172, unit: "活跃项", status: "warn" },
  { key: "ac", name: "接入中心", count: 227, unit: "在线探针", status: "healthy" },
];

const SEVERITY = { critical: 4, high: 52, medium: 53, low: 39 };

const HOURS = Array.from({ length: 12 }, (_, i) => `${String(i * 2).padStart(2, "0")}:00`);
const TREND = {
  edr: [12, 8, 6, 5, 4, 7, 15, 22, 30, 26, 18, 14],
  bde: [4, 3, 2, 2, 1, 3, 8, 12, 10, 9, 6, 5],
  ml: [40, 35, 28, 30, 25, 33, 52, 60, 48, 42, 38, 44],
};

const FEED: FeedAlert[] = [
  { id: "1", time: "16:42:07", severity: "critical", title: "检测到内存马注入 - java 进程异常加载", host: "g32-bet-settle-01" },
  { id: "2", time: "16:41:53", severity: "high", title: "端口扫描检测 - 来自 203.0.113.44", host: "openapi-gw-03" },
  { id: "3", time: "16:41:22", severity: "high", title: "可疑提权 - sudo 异常调用链", host: "wallet-client-07" },
  { id: "4", time: "16:40:58", severity: "medium", title: "敏感文件访问 - /etc/shadow 读取", host: "base-central-02" },
  { id: "5", time: "16:40:31", severity: "critical", title: "勒索行为特征 - 批量文件加密", host: "game-center-11" },
  { id: "6", time: "16:39:47", severity: "high", title: "横向移动 - SSH 爆破成功", host: "user-client-04" },
  { id: "7", time: "16:39:12", severity: "low", title: "新增用户 SSH 公钥", host: "devops-nacos-01" },
  { id: "8", time: "16:38:50", severity: "medium", title: "异常外联 - 连接已知 C2 IP", host: "telegram-promo-02" },
  { id: "9", time: "16:38:19", severity: "high", title: "凭据访问 - /proc/*/environ 遍历", host: "merchant-client-05" },
];

const MARQUEE = [
  "攻击链溯源 TOP：SSH爆破 → 提权 → 横向移动 (g32-user)",
  "真实待修严重漏洞 17 · 高危 448",
  "K8s 基线严重项 9 · 明文密码待迁 Secret 136",
  "APT 情报命中：Lazarus C2 IP × 2",
  "ML 异常 critical 3791 待研判 · 误报压制率 96%",
];

export default function ScreenPage() {
  return (
    <div className="flex h-screen flex-col">
      <ScreenHeader online={227} total={228} />
      <KpiTicker />

      <main className="flex min-h-0 flex-1 gap-3 px-3 pb-2">
        {/* 左列 */}
        <div className="flex w-[23%] flex-col gap-3">
          <Panel title="安全态势评分" accent="cyan" className="flex-[4]">
            <PostureGauge score={72} />
          </Panel>
          <Panel title="检测引擎健康墙" accent="emerald" className="flex-[4]">
            <EngineHealthWall engines={ENGINES} />
          </Panel>
          <Panel title="主机安全评分榜" accent="rose" className="flex-[5]">
            <HostRank />
          </Panel>
        </div>

        {/* 中列 */}
        <div className="flex flex-1 flex-col gap-3">
          <Panel title="攻击态势 · 实时" accent="rose" className="flex-[6]">
            <AttackMap />
          </Panel>
          <div className="flex flex-[4] gap-3">
            <Panel title="ATT&CK 战术覆盖" accent="amber" className="flex-1">
              <AttackMatrix />
            </Panel>
            <Panel title="24h 告警趋势（多引擎）" accent="violet" className="flex-1">
              <TrendChart hours={HOURS} edr={TREND.edr} bde={TREND.bde} ml={TREND.ml} />
            </Panel>
          </div>
        </div>

        {/* 右列 */}
        <div className="flex w-[23%] flex-col gap-3">
          <Panel title="实时告警流" accent="rose" className="flex-[5]">
            <AlertFeed alerts={FEED} />
          </Panel>
          <Panel title="告警等级分布" accent="amber" className="flex-[4]">
            <SeverityRing d={SEVERITY} />
          </Panel>
          <Panel title="漏洞 · 基线合规态势" accent="emerald" className="flex-[4]">
            <ComplianceBar />
          </Panel>
        </div>
      </main>

      {/* KPI 跑马灯 */}
      <footer className="relative h-8 shrink-0 overflow-hidden border-t border-cyan-400/15 bg-[#070D1B]">
        <motion.div
          className="absolute flex h-full items-center gap-10 whitespace-nowrap px-6 font-mono text-xs text-cyan-200/80"
          animate={{ x: ["0%", "-50%"] }}
          transition={{ duration: 30, repeat: Infinity, ease: "linear" }}
        >
          {[...MARQUEE, ...MARQUEE].map((m, i) => (
            <span key={i} className="flex items-center gap-2">
              <span className="h-1.5 w-1.5 rounded-full bg-cyan-400" />
              {m}
            </span>
          ))}
        </motion.div>
      </footer>
    </div>
  );
}
