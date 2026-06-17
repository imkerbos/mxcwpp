"use client";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Activity, AlertTriangle, ShieldAlert } from "lucide-react";
import { useUrlState } from "@/hooks/useUrlState";
import { detectionApi } from "@/lib/api/detection";
import type { AnomalyEvent, Severity } from "@/lib/api/types";
import { Card } from "@/components/ui/Card";
import { DataTable, type Column } from "@/components/ui/DataTable";
import { Pagination } from "@/components/ui/Pagination";
import { FilterBar } from "@/components/ui/FilterBar";
import { Select } from "@/components/ui/Select";
import { Button } from "@/components/ui/Button";
import { Drawer } from "@/components/ui/Drawer";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { StatCard } from "@/components/ui/StatCard";
import { StatusTag, SeverityTag } from "@/components/ui/Tag";
import { toast } from "@/components/ui/toast";

interface ListParams {
  page: number;
  page_size: number;
  severity: string;
  status: string;
  alert_type: string;
}

type Tone = "success" | "warning" | "danger" | "info" | "neutral";

const SEVERITIES: Severity[] = ["critical", "high", "medium", "low"];

const buildAlertTypeLabels = (t: TFunction): Record<AnomalyEvent["alert_type"], string> => ({
  isolation_forest: t("detection.anomaly.typeIsolationForest"),
  correlation: t("detection.anomaly.typeCorrelation"),
});
const buildStatusMeta = (t: TFunction): Record<AnomalyEvent["status"], { tone: Tone; label: string }> => ({
  open: { tone: "warning", label: t("detection.anomaly.statusOpen") },
  confirmed: { tone: "danger", label: t("detection.anomaly.statusConfirmed") },
  false_positive: { tone: "neutral", label: t("detection.anomaly.statusFalsePositive") },
});

const buildSeverityOptions = (t: TFunction) => [
  { label: t("common.allSeverity"), value: "" },
  ...SEVERITIES.map((s) => ({ label: t(`common.severity.${s}`), value: s })),
];
const buildStatusOptions = (t: TFunction) => [
  { label: t("common.allStatus"), value: "" },
  { label: t("detection.anomaly.statusOpen"), value: "open" },
  { label: t("detection.anomaly.statusConfirmed"), value: "confirmed" },
  { label: t("detection.anomaly.statusFalsePositive"), value: "false_positive" },
];
const buildAlertTypeOptions = (t: TFunction) => [
  { label: t("common.allType"), value: "" },
  { label: t("detection.anomaly.typeIsolationForest"), value: "isolation_forest" },
  { label: t("detection.anomaly.typeCorrelation"), value: "correlation" },
];

function Field({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex gap-3 text-sm">
      <span className="w-20 shrink-0 text-muted">{label}</span>
      <span className="break-all text-ink">{value}</span>
    </div>
  );
}

export default function AnomalyPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const alertTypeLabels = buildAlertTypeLabels(t);
  const statusMeta = buildStatusMeta(t);
  const severityOptions = buildSeverityOptions(t);
  const statusOptions = buildStatusOptions(t);
  const alertTypeOptions = buildAlertTypeOptions(t);
  const [params, setParams] = useUrlState({
    page: 1,
    page_size: 20,
    severity: "",
    status: "",
    alert_type: "",
  });

  const { data: stats } = useQuery({
    queryKey: ["anomaly-stats"],
    queryFn: () => detectionApi.anomalyStats(),
  });

  const { data, isLoading } = useQuery({
    queryKey: ["anomalies", params],
    queryFn: () =>
      detectionApi.listAnomalies({
        page: params.page,
        page_size: params.page_size,
        severity: params.severity || undefined,
        status: params.status || undefined,
        alert_type: params.alert_type || undefined,
      }),
  });

  const [detail, setDetail] = useState<AnomalyEvent | null>(null);
  const [confirming, setConfirming] = useState<AnomalyEvent | null>(null);
  const [markingFp, setMarkingFp] = useState<AnomalyEvent | null>(null);

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["anomalies"] });
    queryClient.invalidateQueries({ queryKey: ["anomaly-stats"] });
  };

  const resolveMutation = useMutation({
    mutationFn: ({ id, status }: { id: number; status: "confirmed" | "false_positive" }) =>
      detectionApi.resolveAnomaly(id, status),
    onSuccess: (_data, vars) => {
      invalidate();
      setConfirming(null);
      setMarkingFp(null);
      setDetail(null);
      toast.success(vars.status === "confirmed" ? t("detection.anomaly.confirmed") : t("detection.anomaly.markedFp"));
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const renderContext = (ctx: AnomalyEvent["trigger_context"]) => {
    if (ctx === undefined || ctx === null) return "—";
    if (typeof ctx === "string") return ctx;
    return JSON.stringify(ctx, null, 2);
  };

  const columns: Column<AnomalyEvent>[] = [
    {
      key: "hostname",
      title: t("detection.anomaly.colHost"),
      render: (r) => (
        <div>
          <div className="font-medium text-ink">{r.hostname || r.host_id}</div>
          <div className="text-xs text-faint">{r.host_id}</div>
        </div>
      ),
    },
    {
      key: "alert_type",
      title: t("detection.anomaly.colType"),
      render: (r) => <StatusTag tone="neutral">{alertTypeLabels[r.alert_type] ?? r.alert_type}</StatusTag>,
    },
    { key: "severity", title: t("common.level"), render: (r) => <SeverityTag level={r.severity} /> },
    {
      key: "anomaly_score",
      title: t("detection.anomaly.colAnomalyScore"),
      render: (r) => <span className="font-semibold tabular-nums text-ink">{r.anomaly_score.toFixed(2)}</span>,
    },
    {
      key: "top_metric",
      title: t("detection.anomaly.colTopMetric"),
      render: (r) => <span className="font-mono text-xs text-faint">{r.top_metric || "—"}</span>,
    },
    {
      key: "status",
      title: t("common.status"),
      render: (r) => <StatusTag tone={statusMeta[r.status].tone}>{statusMeta[r.status].label}</StatusTag>,
    },
    {
      key: "actions",
      title: t("common.actions"),
      align: "right",
      render: (r) => (
        <div className="flex justify-end gap-2" onClick={(e) => e.stopPropagation()}>
          <button type="button" className="text-sm text-muted transition-colors hover:text-ink" onClick={() => setDetail(r)}>
            {t("common.details")}
          </button>
          {r.status === "open" && (
            <>
              <button type="button" className="text-sm text-danger transition-colors hover:opacity-80" onClick={() => setConfirming(r)}>
                {t("detection.anomaly.actionConfirm")}
              </button>
              <button type="button" className="text-sm text-muted transition-colors hover:text-ink" onClick={() => setMarkingFp(r)}>
                {t("detection.anomaly.actionFalsePositive")}
              </button>
            </>
          )}
        </div>
      ),
    },
  ];

  return (
    <>
      <div className="mb-5 grid grid-cols-2 gap-3 md:grid-cols-3">
        <StatCard compact label={t("detection.anomaly.statTotal")} value={stats?.total ?? 0} icon={Activity} tone="default" />
        <StatCard compact label={t("detection.anomaly.statOpen")} value={stats?.open ?? 0} icon={AlertTriangle} tone="warning" />
        <StatCard compact label={t("detection.anomaly.statCritical")} value={stats?.critical ?? 0} icon={ShieldAlert} tone="danger" />
      </div>

      <div className="space-y-4">
        <FilterBar>
          <Select
            value={params.severity}
            onChange={(v) => setParams((p) => ({ ...p, severity: v, page: 1 }))}
            options={severityOptions}
          />
          <Select
            value={params.status}
            onChange={(v) => setParams((p) => ({ ...p, status: v, page: 1 }))}
            options={statusOptions}
          />
          <Select
            value={params.alert_type}
            onChange={(v) => setParams((p) => ({ ...p, alert_type: v, page: 1 }))}
            options={alertTypeOptions}
          />
        </FilterBar>
        <Card>
          <DataTable
            columns={columns}
            rows={data?.items ?? []}
            rowKey={(r) => r.id}
            loading={isLoading}
            emptyText={t("detection.anomaly.empty")}
            onRowClick={setDetail}
          />
          <Pagination
            page={params.page}
            pageSize={params.page_size}
            total={data?.total ?? 0}
            onChange={(page) => setParams((p) => ({ ...p, page }))}
          />
        </Card>
      </div>

      <Drawer
        open={!!detail}
        onClose={() => setDetail(null)}
        title={t("detection.anomaly.detailTitle")}
        width={560}
        footer={
          detail?.status === "open" ? (
            <>
              <Button variant="ghost" onClick={() => detail && setMarkingFp(detail)}>
                {t("detection.anomaly.actionFalsePositive")}
              </Button>
              <Button onClick={() => detail && setConfirming(detail)}>{t("detection.anomaly.actionConfirm")}</Button>
            </>
          ) : undefined
        }
      >
        {detail && (
          <div className="space-y-5">
            <div className="space-y-2">
              <h2 className="text-lg font-bold text-ink">{detail.hostname || detail.host_id}</h2>
              <div className="flex items-center gap-2">
                <SeverityTag level={detail.severity} />
                <StatusTag tone="neutral">{alertTypeLabels[detail.alert_type] ?? detail.alert_type}</StatusTag>
                <StatusTag tone={statusMeta[detail.status].tone}>{statusMeta[detail.status].label}</StatusTag>
              </div>
            </div>
            <div className="space-y-2">
              <Field label={t("detection.anomaly.fieldHostId")} value={detail.host_id} />
              <Field label={t("detection.anomaly.fieldPattern")} value={detail.pattern_name || "—"} />
              <Field label={t("detection.anomaly.fieldAnomalyScore")} value={<span className="tabular-nums">{detail.anomaly_score.toFixed(2)}</span>} />
              <Field label={t("detection.anomaly.fieldTopMetric")} value={<span className="font-mono">{detail.top_metric || "—"}</span>} />
              <Field label={t("detection.anomaly.fieldTopValue")} value={<span className="tabular-nums">{detail.top_value}</span>} />
              <Field label={t("detection.anomaly.fieldFoundAt")} value={<span className="tabular-nums">{detail.created_at}</span>} />
            </div>
            {detail.description && (
              <div>
                <div className="mb-1.5 text-sm font-medium text-ink">{t("detection.anomaly.description")}</div>
                <p className="text-sm leading-relaxed text-muted">{detail.description}</p>
              </div>
            )}
            <div>
              <div className="mb-1.5 text-sm font-medium text-ink">{t("detection.anomaly.triggerContext")}</div>
              <pre className="overflow-x-auto rounded-control bg-surface-muted p-3 font-mono text-xs text-ink whitespace-pre-wrap break-all">
                {renderContext(detail.trigger_context)}
              </pre>
            </div>
          </div>
        )}
      </Drawer>

      <ConfirmDialog
        open={!!confirming}
        title={t("detection.anomaly.confirmTitle")}
        desc={confirming ? t("detection.anomaly.confirmDesc", { host: confirming.hostname || confirming.host_id }) : undefined}
        loading={resolveMutation.isPending}
        onConfirm={() => confirming && resolveMutation.mutate({ id: confirming.id, status: "confirmed" })}
        onCancel={() => setConfirming(null)}
      />
      <ConfirmDialog
        open={!!markingFp}
        title={t("detection.anomaly.fpTitle")}
        desc={markingFp ? t("detection.anomaly.fpDesc", { host: markingFp.hostname || markingFp.host_id }) : undefined}
        loading={resolveMutation.isPending}
        onConfirm={() => markingFp && resolveMutation.mutate({ id: markingFp.id, status: "false_positive" })}
        onCancel={() => setMarkingFp(null)}
      />
    </>
  );
}
