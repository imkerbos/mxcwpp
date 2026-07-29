"use client";
import { useEffect, useRef, useState } from "react";
import ReactECharts from "echarts-for-react";
import * as echarts from "echarts";

// 平台侧（GKE asia-east2，香港）为攻击线汇聚中心。
const CENTER: [number, number] = [114.1, 22.3];

type Src = { name: string; country: string; coord: [number, number]; sev: "critical" | "high" };

// ⚠️ P1 示例攻击源池。P3 接 GeoIP 后由后端将真实攻击源 IP 解析为经纬度。
const POOL: Src[] = [
  { name: "Moscow", country: "俄罗斯", coord: [37.6, 55.7], sev: "critical" },
  { name: "Amsterdam", country: "荷兰", coord: [4.9, 52.4], sev: "high" },
  { name: "Sao Paulo", country: "巴西", coord: [-46.6, -23.5], sev: "high" },
  { name: "Virginia", country: "美国", coord: [-78.5, 37.4], sev: "critical" },
  { name: "Singapore", country: "新加坡", coord: [103.8, 1.35], sev: "high" },
  { name: "Frankfurt", country: "德国", coord: [8.7, 50.1], sev: "high" },
  { name: "Lagos", country: "尼日利亚", coord: [3.4, 6.5], sev: "high" },
  { name: "Mumbai", country: "印度", coord: [72.9, 19.1], sev: "high" },
  { name: "Kyiv", country: "乌克兰", coord: [30.5, 50.5], sev: "critical" },
  { name: "Seoul", country: "韩国", coord: [127.0, 37.5], sev: "high" },
];

const TOP = [
  { country: "美国", n: 42 },
  { country: "俄罗斯", n: 31 },
  { country: "荷兰", n: 24 },
  { country: "德国", n: 18 },
  { country: "巴西", n: 15 },
];

const sevColor = (s: string) => (s === "critical" ? "#f43f5e" : "#fb923c");

type Flash = { id: number; src: Src };

export function AttackMap() {
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const [flashes, setFlashes] = useState<Flash[]>([]);
  const [count, setCount] = useState(1287);
  const idRef = useRef(0);

  useEffect(() => {
    let alive = true;
    fetch("/geo/world.json")
      .then((r) => r.json())
      .then((geo) => {
        if (!alive) return;
        echarts.registerMap("world", geo);
        setReady(true);
      })
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
    };
  }, []);

  // 事件驱动：每 ~1.6s 触发一条攻击线，3s 后淡出（不常驻）。计数器累加。
  useEffect(() => {
    if (!ready) return;
    const timer = setInterval(() => {
      const src = POOL[Math.floor(Math.random() * POOL.length)];
      const id = ++idRef.current;
      setFlashes((f) => [...f, { id, src }]);
      setCount((c) => c + 1);
      setTimeout(() => setFlashes((f) => f.filter((x) => x.id !== id)), 3000);
    }, 1600);
    return () => clearInterval(timer);
  }, [ready]);

  if (failed) return <Centered text="世界地图加载失败" />;
  if (!ready) return <Centered text="加载世界地图…" />;

  const option = {
    // 关闭数据更新过渡动画：攻击线/源点增删直接生效，不按索引插值滑动（消除 source 漂移）。
    // 流光(effect)与涟漪(rippleEffect)是独立连续动画，不受此影响。
    animationDurationUpdate: 0,
    geo: {
      map: "world",
      roam: false,
      silent: true,
      left: 0,
      right: 0,
      top: 20,
      bottom: 10,
      itemStyle: { areaColor: "#0A1526", borderColor: "rgba(56,189,248,0.18)", borderWidth: 0.5 },
      emphasis: { disabled: true },
    },
    series: [
      {
        type: "lines",
        coordinateSystem: "geo",
        zlevel: 2,
        // constantSpeed：长短攻击线以相同视觉速度流动，避免长短不一造成的怪异过渡。
        effect: { show: true, constantSpeed: 36, trailLength: 0.45, symbol: "circle", symbolSize: 4, color: "#fff" },
        lineStyle: { width: 1.4, opacity: 0.55, curveness: 0.15 },
        data: flashes.map((f) => ({ coords: [f.src.coord, CENTER], lineStyle: { color: sevColor(f.src.sev) } })),
      },
      {
        type: "effectScatter",
        coordinateSystem: "geo",
        zlevel: 3,
        rippleEffect: { brushType: "stroke", scale: 3 },
        symbolSize: 6,
        data: flashes.map((f) => ({ name: f.src.name, value: [...f.src.coord, 1], itemStyle: { color: sevColor(f.src.sev) } })),
      },
      {
        type: "effectScatter",
        coordinateSystem: "geo",
        zlevel: 4,
        rippleEffect: { brushType: "stroke", scale: 5 },
        symbolSize: 10,
        itemStyle: { color: "#22d3ee", shadowBlur: 10, shadowColor: "#22d3ee" },
        data: [{ name: "本平台", value: [...CENTER, 1] }],
      },
    ],
  };

  return (
    <div className="relative h-full w-full">
      {/* 实时攻击计数器 */}
      <div className="absolute left-2 top-1 z-10">
        <div className="font-mono text-xl font-extrabold tabular-nums text-rose-300">{count.toLocaleString()}</div>
        <div className="text-[9px] tracking-wide text-slate-400">今日累计攻击</div>
      </div>
      {/* TOP 攻击源国家 */}
      <div className="absolute right-2 top-1 z-10 space-y-0.5 text-right">
        <div className="text-[9px] tracking-wide text-cyan-400/70">TOP 攻击源</div>
        {TOP.map((t) => (
          <div key={t.country} className="flex items-center justify-end gap-1.5 text-[10px]">
            <span className="text-slate-400">{t.country}</span>
            <span className="font-mono font-bold tabular-nums text-orange-300">{t.n}</span>
          </div>
        ))}
      </div>
      <span className="absolute bottom-1 right-2 z-10 rounded bg-cyan-500/10 px-1.5 py-0.5 text-[9px] tracking-wide text-cyan-400/60">
        示例数据 · GeoIP 待接后端(P3)
      </span>
      {/* notMerge=false（默认）：新增/淡出攻击线时平滑合并，不硬重置其余线的流光动画。 */}
      <ReactECharts option={option} style={{ height: "100%", width: "100%" }} lazyUpdate />
    </div>
  );
}

function Centered({ text }: { text: string }) {
  return <div className="flex h-full items-center justify-center text-xs text-slate-500">{text}</div>;
}
