"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useUrlState } from "@/hooks/useUrlState";
import { alertsApi } from "@/lib/api/alerts";
import type { Alert, Severity } from "@/lib/api/types";
import { Card } from "@/components/ui/Card";
import { DataTable, type Column } from "@/components/ui/DataTable";
import { Pagination } from "@/components/ui/Pagination";
import { FilterBar } from "@/components/ui/FilterBar";
import { Select } from "@/components/ui/Select";
import { Input } from "@/components/ui/Input";
import { Drawer } from "@/components/ui/Drawer";
import { StatusTag, SeverityTag } from "@/components/ui/Tag";
import { CopyButton } from "@/components/ui/CopyButton";

const isSeverity = (v: string): v is Severity => ["critical", "high", "medium", "low"].includes(v);

function Field({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex gap-3 text-sm">
      <span className="w-20 shrink-0 text-muted">{label}</span>
      <span className="min-w-0 break-all text-ink">{value}</span>
    </div>
  );
}

export default function ThreatAlertsPage() {
  const { t } = useTranslation();
  const [params, setParams] = useUrlState({
    page: 1,
    page_size: 20,
    severity: "",
    status: "",
    host_id: "",
    onlyChain: "",
  });

  const statusOptions = [
    { label: t("detection.threatAlerts.allStatus"), value: "" },
    { label: t("detection.threatAlerts.statusActive"), value: "active" },
    { label: t("detection.threatAlerts.statusResolved"), value: "resolved" },
    { label: t("detection.threatAlerts.statusIgnored"), value: "ignored" },
  ];
  const severityOptions = [
    { label: t("detection.threatAlerts.allSeverity"), value: "" },
    { label: t("common.severity.critical"), value: "critical" },
    { label: t("common.severity.high"), value: "high" },
    { label: t("common.severity.medium"), value: "medium" },
    { label: t("common.severity.low"), value: "low" },
  ];
  const kindOptions = [
    { label: t("detection.threatAlerts.allKind"), value: "" },
    { label: t("detection.threatAlerts.onlyChain"), value: "1" },
  ];

  const { data, isLoading } = useQuery({
    queryKey: ["threat-alerts", params],
    queryFn: () =>
      alertsApi.list({
        page: params.page,
        page_size: params.page_size,
        alert_type: "detection", // 仅 EDR/检测来源
        severity: params.severity || undefined,
        status: params.status || undefined,
        host_id: params.host_id || undefined,
        category: params.onlyChain === "1" ? "attack_chain" : undefined,
      }),
  });

  const [detail, setDetail] = useState<Alert | null>(null);

  const columns: Column<Alert>[] = [
    { key: "last_seen_at", title: t("detection.threatAlerts.colTime"), render: (r) => <span className="text-faint tabular-nums">{r.last_seen_at}</span> },
    {
      key: "title",
      title: t("detection.threatAlerts.colTitle"),
      render: (r) => (
        <div className="flex items-center gap-2">
          {r.category === "attack_chain" && <StatusTag tone="danger">{t("detection.threatAlerts.chainTag")}</StatusTag>}
          <span className="font-medium text-ink">{r.title}</span>
        </div>
      ),
    },
    { key: "severity", title: t("common.level"), render: (r) => (isSeverity(r.severity) ? <SeverityTag level={r.severity} /> : "—") },
    { key: "category", title: t("detection.threatAlerts.colCategory"), render: (r) => <span className="font-mono text-xs text-faint">{r.category || "—"}</span> },
    {
      key: "host",
      title: t("detection.threatAlerts.colHost"),
      render: (r) => (
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            className="font-medium text-ink transition-colors hover:text-primary"
            onClick={(e) => { e.stopPropagation(); setParams((p) => ({ ...p, host_id: r.host_id, page: 1 })); }}
          >
            {r.host?.hostname || r.host_id}
          </button>
          <CopyButton text={r.host_id} />
        </div>
      ),
    },
    { key: "hit_count", title: t("detection.threatAlerts.colHitCount"), align: "right", render: (r) => <span className="tabular-nums text-muted">{r.hit_count}</span> },
    {
      key: "status",
      title: t("common.status"),
      render: (r) => (
        <StatusTag tone={r.status === "active" ? "danger" : r.status === "resolved" ? "success" : "neutral"}>
          {t(`detection.threatAlerts.status${r.status === "active" ? "Active" : r.status === "resolved" ? "Resolved" : "Ignored"}`)}
        </StatusTag>
      ),
    },
  ];

  return (
    <>
      <div className="space-y-4">
        <p className="text-sm text-muted">{t("detection.threatAlerts.intro")}</p>
        <FilterBar>
          <Select value={params.onlyChain} onChange={(v) => setParams((p) => ({ ...p, onlyChain: v, page: 1 }))} options={kindOptions} />
          <Select value={params.severity} onChange={(v) => setParams((p) => ({ ...p, severity: v, page: 1 }))} options={severityOptions} />
          <Select value={params.status} onChange={(v) => setParams((p) => ({ ...p, status: v, page: 1 }))} options={statusOptions} />
          <Input
            value={params.host_id}
            onChange={(e) => setParams((p) => ({ ...p, host_id: e.target.value, page: 1 }))}
            placeholder={t("detection.threatAlerts.filterHostId")}
            className="w-56"
          />
        </FilterBar>
        <Card>
          <DataTable
            columns={columns}
            rows={data?.items ?? []}
            rowKey={(r) => r.id}
            loading={isLoading}
            emptyText={t("detection.threatAlerts.empty")}
            onRowClick={setDetail}
          />
          <Pagination page={params.page} pageSize={params.page_size} total={data?.total ?? 0} onChange={(page) => setParams((p) => ({ ...p, page }))} />
        </Card>
      </div>

      <Drawer open={!!detail} onClose={() => setDetail(null)} title={t("detection.threatAlerts.detailTitle")} width={560}>
        {detail && (
          <div className="space-y-2">
            <div className="mb-2 flex items-center gap-2">
              {detail.category === "attack_chain" && <StatusTag tone="danger">{t("detection.threatAlerts.chainTag")}</StatusTag>}
              {isSeverity(detail.severity) && <SeverityTag level={detail.severity} />}
            </div>
            <Field label={t("detection.threatAlerts.colTitle")} value={detail.title} />
            <Field label={t("detection.threatAlerts.colCategory")} value={detail.category || "—"} />
            <Field label={t("detection.threatAlerts.colHost")} value={<span className="inline-flex items-center gap-1.5">{detail.host?.hostname || detail.host_id}<CopyButton text={detail.host_id} /></span>} />
            <Field label="host_id" value={<span className="inline-flex items-center gap-1.5 font-mono text-xs">{detail.host_id}<CopyButton text={detail.host_id} /></span>} />
            <Field label={t("detection.threatAlerts.colHitCount")} value={detail.hit_count} />
            <Field label={t("detection.threatAlerts.firstSeen")} value={<span className="tabular-nums">{detail.first_seen_at}</span>} />
            <Field label={t("detection.threatAlerts.lastSeen")} value={<span className="tabular-nums">{detail.last_seen_at}</span>} />
            {detail.description && <Field label={t("detection.threatAlerts.desc")} value={detail.description} />}
            {detail.actual && (
              <div>
                <div className="mb-1.5 mt-2 text-sm font-medium text-ink">{t("detection.threatAlerts.matched")}</div>
                <pre className="overflow-x-auto rounded-control bg-surface-muted p-3 font-mono text-xs text-ink whitespace-pre-wrap break-all">{detail.actual}</pre>
              </div>
            )}
          </div>
        )}
      </Drawer>
    </>
  );
}
