"use client";
import { useTranslation } from "react-i18next";
import { StatCard } from "@/components/ui/StatCard";
import { ScoreGauge } from "./ScoreGauge";
import { ServerCog, ServerOff, Bell, Bug, ShieldCheck } from "lucide-react";
import type { DashboardStats } from "@/lib/api/types";

export function KpiRow({ s }: { s: DashboardStats }) {
  const { t } = useTranslation();
  return (
    <div className="grid grid-cols-2 lg:grid-cols-6 gap-4">
      <ScoreGauge score={s.securityScore} />
      <StatCard label={t("dashboard.onlineAgents")} value={s.onlineAgents} icon={ServerCog} />
      <StatCard label={t("dashboard.offlineAgents")} value={s.offlineAgents} icon={ServerOff} tone="warning" />
      <StatCard label={t("dashboard.pendingAlerts")} value={s.pendingAlerts} icon={Bell} tone="danger" />
      <StatCard label={t("dashboard.openVulnerabilities")} value={s.pendingVulnerabilities} icon={Bug} tone="warning" />
      <StatCard label={t("dashboard.baselineCompliance")} value={`${s.baselineHardeningPercent.toFixed(1)}%`} icon={ShieldCheck} tone="success" />
    </div>
  );
}
