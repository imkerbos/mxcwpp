"use client";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Server, ShieldCheck, FileText, ListChecks } from "lucide-react";
import { operationsApi } from "@/lib/api/operations";
import { Card, CardHeader } from "@/components/ui/Card";
import { StatCard } from "@/components/ui/StatCard";
import { ChartCard } from "@/components/ui/ChartCard";
import { EmptyState } from "@/components/ui/EmptyState";

export default function ReportsPage() {
  const { t } = useTranslation();
  const SEV_LABEL: Record<string, string> = {
    critical: t("common.severity.critical"),
    high: t("common.severity.high"),
    medium: t("common.severity.medium"),
    low: t("common.severity.low"),
  };
  const { data, isLoading } = useQuery({
    queryKey: ["ops-reports"],
    queryFn: () => operationsApi.reportStats(),
  });

  if (isLoading) {
    return <Card className="p-10 text-center text-muted">{t("operations.reports.loading")}</Card>;
  }
  if (!data) {
    return <EmptyState title={t("operations.reports.emptyData")} desc="" />;
  }

  const { hostStats, baselineStats, policyStats, taskStats } = data;

  // 主机状态分布饼图
  const hostStatusOption = {
    tooltip: { trigger: "item", formatter: "{b}: {c} ({d}%)" },
    legend: { orient: "vertical", left: "left" },
    series: [
      {
        type: "pie",
        radius: ["40%", "70%"],
        data: [
          { value: hostStats.online, name: t("operations.reports.online"), itemStyle: { color: "#22C55E" } },
          { value: hostStats.offline, name: t("operations.reports.offline"), itemStyle: { color: "#EF4444" } },
        ].filter((d) => d.value > 0),
      },
    ],
  };

  // 基线检查结果统计饼图
  const baselineResultOption = {
    tooltip: { trigger: "item", formatter: "{b}: {c} ({d}%)" },
    legend: { orient: "vertical", left: "left" },
    series: [
      {
        type: "pie",
        radius: "65%",
        data: [
          { value: baselineStats.passed, name: t("operations.reports.statPassed"), itemStyle: { color: "#22C55E" } },
          { value: baselineStats.failed, name: t("operations.reports.statFailed"), itemStyle: { color: "#EF4444" } },
          { value: baselineStats.warning, name: t("operations.reports.statWarning"), itemStyle: { color: "#F59E0B" } },
        ].filter((d) => d.value > 0),
      },
    ],
  };

  // 基线严重级别分布柱状
  const severityOption = {
    tooltip: { trigger: "axis", axisPointer: { type: "shadow" } },
    grid: { left: "3%", right: "4%", bottom: "3%", containLabel: true },
    xAxis: { type: "category", data: [SEV_LABEL.critical, SEV_LABEL.high, SEV_LABEL.medium, SEV_LABEL.low] },
    yAxis: { type: "value" },
    series: [
      {
        type: "bar",
        data: [
          { value: baselineStats.bySeverity.critical, itemStyle: { color: "#EF4444" } },
          { value: baselineStats.bySeverity.high, itemStyle: { color: "#ff7875" } },
          { value: baselineStats.bySeverity.medium, itemStyle: { color: "#ffa940" } },
          { value: baselineStats.bySeverity.low, itemStyle: { color: "#ffc53d" } },
        ],
      },
    ],
  };

  // 操作系统分布
  const osEntries = Object.entries(hostStats.byOsFamily);
  const osOption = {
    tooltip: { trigger: "item", formatter: "{b}: {c} ({d}%)" },
    legend: { orient: "vertical", left: "left" },
    series: [{ type: "pie", radius: "65%", data: osEntries.map(([name, value]) => ({ name, value })) }],
  };

  return (
    <div className="space-y-6">
      {/* 主机统计 */}
      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-ink">{t("operations.reports.sectionHosts")}</h2>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <StatCard label={t("operations.reports.statTotalHosts")} value={hostStats.total} icon={Server} />
          <StatCard label={t("operations.reports.statOnlineHosts")} value={hostStats.online} icon={Server} tone="success" />
          <StatCard label={t("operations.reports.statOfflineHosts")} value={hostStats.offline} icon={Server} tone="danger" />
          <StatCard label={t("operations.reports.statOsKinds")} value={osEntries.length} icon={Server} />
        </div>
      </section>

      {/* 基线统计 */}
      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-ink">{t("operations.reports.sectionBaseline")}</h2>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <StatCard label={t("operations.reports.statTotalChecks")} value={baselineStats.totalChecks} icon={ShieldCheck} />
          <StatCard label={t("operations.reports.statPassed")} value={baselineStats.passed} icon={ShieldCheck} tone="success" />
          <StatCard label={t("operations.reports.statFailed")} value={baselineStats.failed} icon={ShieldCheck} tone="danger" />
          <StatCard label={t("operations.reports.statWarning")} value={baselineStats.warning} icon={ShieldCheck} tone="warning" />
        </div>
      </section>

      {/* 策略统计 */}
      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-ink">{t("operations.reports.sectionPolicy")}</h2>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <StatCard label={t("operations.reports.statTotalPolicies")} value={policyStats.total} icon={FileText} />
          <StatCard label={t("operations.reports.statEnabled")} value={policyStats.enabled} icon={FileText} tone="success" />
          <StatCard label={t("operations.reports.statDisabled")} value={policyStats.disabled} icon={FileText} />
          <StatCard label={t("operations.reports.statAvgPassRate")} value={`${(policyStats.avgPassRate || 0).toFixed(1)}%`} icon={FileText} />
        </div>
      </section>

      {/* 任务统计 */}
      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-ink">{t("operations.reports.sectionTask")}</h2>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <StatCard label={t("operations.reports.statTotalTasks")} value={taskStats.total} icon={ListChecks} />
          <StatCard label={t("operations.reports.statCompleted")} value={taskStats.completed} icon={ListChecks} tone="success" />
          <StatCard label={t("operations.reports.statRunning")} value={taskStats.running} icon={ListChecks} tone="warning" />
          <StatCard label={t("operations.reports.statTaskFailed")} value={taskStats.failed} icon={ListChecks} tone="danger" />
        </div>
      </section>

      {/* 图表 */}
      <section className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ChartCard title={t("operations.reports.chartHostStatus")} option={hostStatusOption} />
        <ChartCard title={t("operations.reports.chartBaselineResult")} option={baselineResultOption} />
        <ChartCard title={t("operations.reports.chartSeverity")} option={severityOption} />
        {osEntries.length > 0 && <ChartCard title={t("operations.reports.chartOs")} option={osOption} />}
      </section>

      {/* 基线类别分布（列表） */}
      <Card>
        <CardHeader title={t("operations.reports.categoryTitle")} />
        <div className="px-5 pb-5">
          {Object.keys(baselineStats.byCategory).length === 0 ? (
            <EmptyState title={t("operations.reports.emptyCategory")} desc="" />
          ) : (
            <div className="grid grid-cols-2 gap-x-6 gap-y-1.5 text-sm md:grid-cols-3">
              {Object.entries(baselineStats.byCategory).map(([k, v]) => (
                <div key={k} className="flex justify-between border-b border-border/50 py-1">
                  <span className="truncate text-muted">{SEV_LABEL[k] ?? k}</span>
                  <span className="tabular-nums text-ink">{v}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}
