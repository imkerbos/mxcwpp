"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Bug, ShieldAlert, Boxes, Radar } from "lucide-react";
import { operationsApi } from "@/lib/api/operations";
import type {
  TaskReportType,
  AntivirusTaskReport,
  VulnerabilityTaskReport,
  KubeTaskReport,
  EdrTaskReport,
} from "@/lib/api/types";
import { Tabs } from "@/components/ui/Tabs";
import { Card, CardHeader } from "@/components/ui/Card";
import { StatCard } from "@/components/ui/StatCard";
import { EmptyState } from "@/components/ui/EmptyState";
import { StatusTag } from "@/components/ui/Tag";
import type { LucideIcon } from "lucide-react";

const buildTabItems = (t: TFunction): { key: TaskReportType; label: string }[] => [
  { key: "antivirus", label: t("operations.taskReport.tabAntivirus") },
  { key: "vulnerability", label: t("operations.taskReport.tabVulnerability") },
  { key: "kube", label: t("operations.taskReport.tabKube") },
  { key: "edr", label: t("operations.taskReport.tabEdr") },
];

const buildSevLabel = (t: TFunction): Record<string, string> => ({
  critical: t("common.severity.critical"),
  high: t("common.severity.high"),
  medium: t("common.severity.medium"),
  low: t("common.severity.low"),
});

// 分布键值对列表
function DistList({ title, data, t }: { title: string; data: Record<string, number>; t: TFunction }) {
  const SEV_LABEL = buildSevLabel(t);
  const entries = Object.entries(data);
  return (
    <Card>
      <CardHeader title={title} />
      <div className="px-5 pb-5">
        {entries.length === 0 ? (
          <EmptyState title={t("operations.taskReport.noData")} desc="" />
        ) : (
          <div className="space-y-1.5 text-sm">
            {entries.map(([k, v]) => (
              <div key={k} className="flex justify-between border-b border-border/50 py-1">
                <span className="text-muted">{SEV_LABEL[k] ?? k}</span>
                <span className="tabular-nums text-ink">{v}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </Card>
  );
}

function StatGrid({ items }: { items: { label: string; value: string | number; icon: LucideIcon; tone?: "default" | "danger" | "warning" | "success" }[] }) {
  return (
    <div className="grid grid-cols-2 gap-4 md:grid-cols-5">
      {items.map((it) => (
        <StatCard key={it.label} label={it.label} value={it.value} icon={it.icon} tone={it.tone} />
      ))}
    </div>
  );
}

function AntivirusView({ d, t }: { d: AntivirusTaskReport; t: TFunction }) {
  const SEV_LABEL = buildSevLabel(t);
  return (
    <div className="space-y-6">
      <StatGrid
        items={[
          { label: t("operations.taskReport.avScanTasks"), value: d.summary.totalTasks, icon: Bug },
          { label: t("operations.taskReport.avTotalThreats"), value: d.summary.totalThreats, icon: Bug, tone: "danger" },
          { label: t("operations.taskReport.avDetected"), value: d.summary.detectedThreats, icon: Bug, tone: "warning" },
          { label: t("operations.taskReport.avQuarantined"), value: d.summary.quarantinedThreats, icon: Bug, tone: "success" },
          { label: t("operations.taskReport.avAffectedHosts"), value: d.summary.affectedHosts, icon: Bug },
        ]}
      />
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <DistList title={t("operations.taskReport.distSeverity")} data={d.severityDistribution} t={t} />
        <DistList title={t("operations.taskReport.avThreatType")} data={d.threatTypeDistribution} t={t} />
        <DistList title={t("operations.taskReport.avActionDist")} data={d.actionDistribution} t={t} />
      </div>
      <Card>
        <CardHeader title={t("operations.taskReport.avTopThreats")} />
        <div className="px-5 pb-5">
          {d.topThreats.length === 0 ? (
            <EmptyState title={t("operations.taskReport.avEmptyThreats")} desc="" />
          ) : (
            <div className="space-y-2 text-sm">
              {d.topThreats.map((item) => (
                <div key={item.threatName} className="flex items-center justify-between border-b border-border/50 py-1.5">
                  <span className="text-ink">{item.threatName}</span>
                  <span className="flex items-center gap-3 text-muted">
                    <StatusTag tone="neutral">{SEV_LABEL[item.severity] ?? item.severity}</StatusTag>
                    <span className="tabular-nums">{t("operations.taskReport.countTimesHosts", { count: item.count, hosts: item.affectedHosts })}</span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}

function VulnerabilityView({ d, t }: { d: VulnerabilityTaskReport; t: TFunction }) {
  const SEV_LABEL = buildSevLabel(t);
  return (
    <div className="space-y-6">
      <StatGrid
        items={[
          { label: t("operations.taskReport.vulnTotal"), value: d.summary.totalVulns, icon: ShieldAlert },
          { label: t("operations.taskReport.vulnUnpatched"), value: d.summary.unpatchedVulns, icon: ShieldAlert, tone: "danger" },
          { label: t("operations.taskReport.vulnFixed"), value: d.summary.fixedVulns, icon: ShieldAlert, tone: "success" },
          { label: t("operations.taskReport.vulnIgnored"), value: d.summary.ignoredVulns, icon: ShieldAlert },
          { label: t("operations.taskReport.vulnAffectedHosts"), value: d.summary.affectedHosts, icon: ShieldAlert, tone: "warning" },
        ]}
      />
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <DistList title={t("operations.taskReport.distSeverity")} data={d.severityDistribution} t={t} />
        <Card>
          <CardHeader title={t("operations.taskReport.vulnComponentTop")} />
          <div className="px-5 pb-5">
            {d.componentDistribution.length === 0 ? (
              <EmptyState title={t("operations.taskReport.noData")} desc="" />
            ) : (
              <div className="space-y-1.5 text-sm">
                {d.componentDistribution.map((c) => (
                  <div key={c.component} className="flex justify-between border-b border-border/50 py-1">
                    <span className="text-muted">{c.component}</span>
                    <span className="tabular-nums text-ink">{c.count}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </Card>
      </div>
      <Card>
        <CardHeader title={t("operations.taskReport.vulnTopCve")} />
        <div className="px-5 pb-5">
          {d.topVulns.length === 0 ? (
            <EmptyState title={t("operations.taskReport.vulnEmpty")} desc="" />
          ) : (
            <div className="space-y-2 text-sm">
              {d.topVulns.map((v) => (
                <div key={v.cveId} className="flex items-center justify-between border-b border-border/50 py-1.5">
                  <span className="font-medium text-ink">{v.cveId}</span>
                  <span className="flex items-center gap-3 text-muted">
                    <StatusTag tone="neutral">{SEV_LABEL[v.severity] ?? v.severity}</StatusTag>
                    <span className="tabular-nums">{t("operations.taskReport.cvssHosts", { score: v.cvssScore, hosts: v.affectedHosts })}</span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}

function KubeView({ d, t }: { d: KubeTaskReport; t: TFunction }) {
  return (
    <div className="space-y-6">
      <StatGrid
        items={[
          { label: t("operations.taskReport.kubeTotalAlarms"), value: d.summary.totalAlarms, icon: Boxes },
          { label: t("operations.taskReport.kubePending"), value: d.summary.pendingAlarms, icon: Boxes, tone: "danger" },
          { label: t("operations.taskReport.kubeProcessed"), value: d.summary.processedAlarms, icon: Boxes, tone: "success" },
          { label: t("operations.taskReport.kubeIgnored"), value: d.summary.ignoredAlarms, icon: Boxes },
          { label: t("operations.taskReport.kubeClusterCount"), value: d.summary.clusterCount, icon: Boxes },
        ]}
      />
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <StatCard label={t("operations.taskReport.kubeBaselineChecks")} value={d.baselineOverview.totalChecks} icon={Boxes} />
        <StatCard label={t("operations.taskReport.kubeBaselinePassed")} value={d.baselineOverview.passed} icon={Boxes} tone="success" />
        <StatCard label={t("operations.taskReport.kubeBaselineFailed")} value={d.baselineOverview.failed} icon={Boxes} tone="danger" />
        <StatCard label={t("operations.taskReport.kubeBaselinePassRate")} value={`${(d.baselineOverview.passRate || 0).toFixed(1)}%`} icon={Boxes} />
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <DistList title={t("operations.taskReport.distSeverity")} data={d.severityDistribution} t={t} />
        <DistList title={t("operations.taskReport.kubeAlarmType")} data={d.alarmTypeDistribution} t={t} />
      </div>
    </div>
  );
}

function EdrView({ d, t }: { d: EdrTaskReport; t: TFunction }) {
  const SEV_LABEL = buildSevLabel(t);
  return (
    <div className="space-y-6">
      <StatGrid
        items={[
          { label: t("operations.taskReport.edrTotalAlerts"), value: d.summary.totalAlerts, icon: Radar },
          { label: t("operations.taskReport.edrActive"), value: d.summary.activeAlerts, icon: Radar, tone: "danger" },
          { label: t("operations.taskReport.edrResolved"), value: d.summary.resolvedAlerts, icon: Radar, tone: "success" },
          { label: t("operations.taskReport.edrTotalStories"), value: d.summary.totalStories, icon: Radar },
          { label: t("operations.taskReport.edrHighRiskStories"), value: d.summary.highRiskStories, icon: Radar, tone: "warning" },
        ]}
      />
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <StatCard label={t("operations.taskReport.edrTotalRules")} value={d.ruleEfficacy.totalRules} icon={Radar} />
        <StatCard label={t("operations.taskReport.edrEnabledRules")} value={d.ruleEfficacy.enabledRules} icon={Radar} />
        <StatCard label={t("operations.taskReport.edrHitRules")} value={d.ruleEfficacy.hitRules} icon={Radar} tone="success" />
        <StatCard label={t("operations.taskReport.edrHitRate")} value={`${(d.ruleEfficacy.hitRate || 0).toFixed(1)}%`} icon={Radar} />
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <DistList title={t("operations.taskReport.distSeverity")} data={d.severityDistribution} t={t} />
        <DistList title={t("operations.taskReport.edrTactic")} data={d.tacticDistribution} t={t} />
      </div>
      <Card>
        <CardHeader title={t("operations.taskReport.edrTopRules")} />
        <div className="px-5 pb-5">
          {d.topRules.length === 0 ? (
            <EmptyState title={t("operations.taskReport.edrEmptyRules")} desc="" />
          ) : (
            <div className="space-y-2 text-sm">
              {d.topRules.map((r) => (
                <div key={r.title} className="flex items-center justify-between border-b border-border/50 py-1.5">
                  <span className="text-ink">{r.title}</span>
                  <span className="flex items-center gap-3 text-muted">
                    <StatusTag tone="neutral">{SEV_LABEL[r.severity] ?? r.severity}</StatusTag>
                    <span className="tabular-nums">{t("operations.taskReport.countTimes", { count: r.count })}</span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}

function TabContent({ type, t }: { type: TaskReportType; t: TFunction }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["ops-task-report", type],
    queryFn: () => operationsApi.taskReport(type),
    retry: false,
  });

  if (isLoading) return <Card className="p-10 text-center text-muted">{t("operations.taskReport.loading")}</Card>;
  if (isError || !data) return <EmptyState title={t("operations.taskReport.emptyData")} desc={t("operations.taskReport.emptyDataDesc")} />;

  switch (type) {
    case "antivirus":
      return <AntivirusView d={data as AntivirusTaskReport} t={t} />;
    case "vulnerability":
      return <VulnerabilityView d={data as VulnerabilityTaskReport} t={t} />;
    case "kube":
      return <KubeView d={data as KubeTaskReport} t={t} />;
    case "edr":
      return <EdrView d={data as EdrTaskReport} t={t} />;
  }
}

export default function TaskReportPage() {
  const { t } = useTranslation();
  const TAB_ITEMS = buildTabItems(t);
  const [active, setActive] = useState<TaskReportType>("antivirus");
  return (
    <div className="space-y-5">
      <Tabs items={TAB_ITEMS} active={active} onChange={(k) => setActive(k as TaskReportType)} />
      <TabContent type={active} t={t} />
    </div>
  );
}
