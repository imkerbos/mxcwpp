"use client";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { ListChecks, CheckCircle2, XCircle, Percent } from "lucide-react";
import { useUrlState } from "@/hooks/useUrlState";
import { kubeApi } from "@/lib/api/kube";
import type { KubeBaselineResult, Severity } from "@/lib/api/types";
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
  result: string;
}

type Tone = "success" | "warning" | "danger" | "info" | "neutral";

function isSeverity(v: string): v is Severity {
  return v === "critical" || v === "high" || v === "medium" || v === "low";
}

const buildSeverityOptions = (t: TFunction) => [
  { label: t("kube.common.allSeverity"), value: "" },
  { label: t("common.severity.critical"), value: "critical" },
  { label: t("common.severity.high"), value: "high" },
  { label: t("common.severity.medium"), value: "medium" },
  { label: t("common.severity.low"), value: "low" },
];
const buildResultOptions = (t: TFunction) => [
  { label: t("kube.baseline.allResult"), value: "" },
  { label: t("kube.baseline.resultPass"), value: "pass" },
  { label: t("kube.baseline.resultFail"), value: "fail" },
];

const buildResultMeta = (t: TFunction): Record<string, { tone: Tone; label: string }> => ({
  pass: { tone: "success", label: t("kube.baseline.resultPass") },
  fail: { tone: "danger", label: t("kube.baseline.resultFail") },
});

// passRate 可能为 0-1 或 0-100，统一归一为百分数
function formatPassRate(v: number | undefined): string {
  if (v == null) return "0%";
  const pct = v <= 1 ? v * 100 : v;
  return `${Math.round(pct)}%`;
}

function Field({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex gap-3 text-sm">
      <span className="w-20 shrink-0 text-muted">{label}</span>
      <span className="text-ink">{value}</span>
    </div>
  );
}

export default function KubeBaselinePage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [params, setParams] = useUrlState({ page: 1, page_size: 20, severity: "", result: "" });

  const severityOptions = buildSeverityOptions(t);
  const resultOptions = buildResultOptions(t);
  const resultMeta = buildResultMeta(t);
  const resultTag = (r: string) => resultMeta[r] ?? { tone: "neutral" as Tone, label: r };

  const { data, isLoading } = useQuery({
    queryKey: ["kube-baseline", params],
    queryFn: () =>
      kubeApi.listBaseline({
        page: params.page,
        page_size: params.page_size,
        severity: params.severity || undefined,
        result: params.result || undefined,
      }),
  });
  const stats = data?.stats;

  const [detail, setDetail] = useState<KubeBaselineResult | null>(null);
  const [detecting, setDetecting] = useState(false);

  const detectMutation = useMutation({
    mutationFn: () => kubeApi.runBaselineDetect(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["kube-baseline"] });
      setDetecting(false);
      toast.success(t("kube.baseline.detectTriggered"));
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const columns: Column<KubeBaselineResult>[] = [
    { key: "checkId", title: t("kube.common.colCheckId"), render: (r) => <span className="font-mono text-xs text-muted">{r.checkId}</span> },
    {
      key: "title",
      title: t("kube.baseline.colTitle"),
      render: (r) => <span className="block max-w-xs truncate font-medium text-ink">{r.title}</span>,
    },
    { key: "category", title: t("kube.common.colCategory"), render: (r) => <StatusTag tone="neutral">{r.category}</StatusTag> },
    {
      key: "severity",
      title: t("common.level"),
      render: (r) =>
        isSeverity(r.severity) ? <SeverityTag level={r.severity} /> : <StatusTag tone="neutral">{r.severity}</StatusTag>,
    },
    { key: "clusterName", title: t("kube.common.colCluster"), render: (r) => <span className="text-ink">{r.clusterName}</span> },
    {
      key: "result",
      title: t("kube.baseline.colResult"),
      render: (r) => <StatusTag tone={resultTag(r.result).tone}>{resultTag(r.result).label}</StatusTag>,
    },
    {
      key: "checkedAt",
      title: t("kube.baseline.colCheckedAt"),
      align: "right",
      render: (r) => <span className="text-faint tabular-nums">{r.checkedAt}</span>,
    },
  ];

  return (
    <>
      <div className="mb-5 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard compact label={t("kube.baseline.statTotalChecks")} value={stats?.totalChecks ?? 0} icon={ListChecks} tone="default" />
        <StatCard compact label={t("kube.baseline.statPassed")} value={stats?.passed ?? 0} icon={CheckCircle2} tone="success" />
        <StatCard compact label={t("kube.baseline.statFailed")} value={stats?.failed ?? 0} icon={XCircle} tone="danger" />
        <StatCard compact label={t("kube.baseline.statPassRate")} value={formatPassRate(stats?.passRate)} icon={Percent} tone="success" />
      </div>

      <div className="space-y-4">
        <FilterBar extra={<Button onClick={() => setDetecting(true)}>{t("kube.baseline.detect")}</Button>}>
          <Select
            value={params.severity}
            onChange={(v) => setParams((p) => ({ ...p, severity: v, page: 1 }))}
            options={severityOptions}
          />
          <Select
            value={params.result}
            onChange={(v) => setParams((p) => ({ ...p, result: v, page: 1 }))}
            options={resultOptions}
          />
        </FilterBar>
        <Card>
          <DataTable
            columns={columns}
            rows={data?.items ?? []}
            rowKey={(r) => r.checkId ?? r.id}
            loading={isLoading}
            emptyText={t("kube.baseline.empty")}
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

      <Drawer open={!!detail} onClose={() => setDetail(null)} title={t("kube.baseline.detailTitle")} width={560}>
        {detail && (
          <div className="space-y-5">
            <div className="space-y-2">
              <h2 className="text-lg font-bold text-ink">{detail.title}</h2>
              <div className="flex items-center gap-2">
                {isSeverity(detail.severity) ? (
                  <SeverityTag level={detail.severity} />
                ) : (
                  <StatusTag tone="neutral">{detail.severity}</StatusTag>
                )}
                <StatusTag tone={resultTag(detail.result).tone}>{resultTag(detail.result).label}</StatusTag>
              </div>
            </div>
            <div className="space-y-2">
              <Field label={t("kube.common.fieldCheckId")} value={<span className="font-mono text-xs">{detail.checkId}</span>} />
              <Field label={t("kube.common.fieldCategory")} value={detail.category} />
              <Field label={t("kube.common.fieldCluster")} value={detail.clusterName} />
              <Field label={t("kube.baseline.fieldCheckedAt")} value={<span className="tabular-nums">{detail.checkedAt}</span>} />
            </div>
          </div>
        )}
      </Drawer>

      <ConfirmDialog
        open={detecting}
        title={t("kube.baseline.detectTitle")}
        desc={t("kube.baseline.detectConfirmDesc")}
        loading={detectMutation.isPending}
        onConfirm={() => detectMutation.mutate()}
        onCancel={() => setDetecting(false)}
      />
    </>
  );
}
