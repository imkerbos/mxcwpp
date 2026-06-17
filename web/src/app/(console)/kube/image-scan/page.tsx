"use client";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useUrlState } from "@/hooks/useUrlState";
import { kubeApi } from "@/lib/api/kube";
import type { KubeImageScan, Severity } from "@/lib/api/types";
import { Card } from "@/components/ui/Card";
import { DataTable, type Column } from "@/components/ui/DataTable";
import { Pagination } from "@/components/ui/Pagination";
import { FilterBar } from "@/components/ui/FilterBar";
import { SearchInput } from "@/components/ui/SearchInput";
import { Select } from "@/components/ui/Select";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { Drawer } from "@/components/ui/Drawer";
import { FormField } from "@/components/ui/FormField";
import { Input } from "@/components/ui/Input";
import { StatusTag, SeverityTag } from "@/components/ui/Tag";
import { EmptyState } from "@/components/ui/EmptyState";
import { toast } from "@/components/ui/toast";

interface ListParams {
  page: number;
  page_size: number;
  image: string;
  status: string;
  [k: string]: unknown;
}

type ScanTone = "success" | "danger" | "info" | "neutral";
const buildStatusMeta = (t: TFunction): Record<string, { label: string; tone: ScanTone }> => ({
  completed: { label: t("kube.imageScan.statusCompleted"), tone: "success" },
  success: { label: t("kube.imageScan.statusCompleted"), tone: "success" },
  failed: { label: t("kube.imageScan.statusFailed"), tone: "danger" },
  scanning: { label: t("kube.imageScan.statusScanning"), tone: "info" },
  pending: { label: t("kube.imageScan.statusPending"), tone: "info" },
});

const buildStatusOptions = (t: TFunction) => [
  { label: t("kube.common.allStatus"), value: "" },
  { label: t("kube.imageScan.statusCompleted"), value: "completed" },
  { label: t("kube.imageScan.statusScanning"), value: "scanning" },
  { label: t("kube.imageScan.statusPending"), value: "pending" },
  { label: t("kube.imageScan.statusFailed"), value: "failed" },
];

function isSeverity(v: string): v is Severity {
  return v === "critical" || v === "high" || v === "medium" || v === "low";
}

function Field({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex gap-3 text-sm">
      <span className="w-20 shrink-0 text-muted">{label}</span>
      <span className="text-ink break-all">{value}</span>
    </div>
  );
}

export default function KubeImageScanPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [params, setParams] = useUrlState({ page: 1, page_size: 20, image: "", status: "" });

  const statusMeta = buildStatusMeta(t);
  const statusOptions = buildStatusOptions(t);

  const { data, isLoading } = useQuery({
    queryKey: ["kube-images", params],
    queryFn: () => kubeApi.listImageScans(params),
  });

  // ---- 扫描镜像 ----
  const [scanOpen, setScanOpen] = useState(false);
  const [scanImage, setScanImage] = useState("");
  const scanMutation = useMutation({
    mutationFn: (image: string) => kubeApi.scanImage(image),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["kube-images"] });
      setScanOpen(false);
      setScanImage("");
      toast.success(t("kube.imageScan.submitted"));
    },
    onError: (e: Error) => toast.error(e.message),
  });

  // ---- 详情 ----
  const [detail, setDetail] = useState<KubeImageScan | null>(null);
  const vulnsQuery = useQuery({
    queryKey: ["kube-image-vulns", detail?.id],
    queryFn: () => kubeApi.getImageScanVulns(detail!.id),
    enabled: !!detail?.id,
  });

  const columns: Column<KubeImageScan>[] = [
    {
      key: "image",
      title: t("kube.imageScan.colImage"),
      render: (r) => <span className="font-mono font-medium text-ink truncate block max-w-[280px]">{r.image}</span>,
    },
    { key: "os", title: t("kube.imageScan.colOs"), render: (r) => <span className="text-muted">{r.os || "—"}</span> },
    {
      key: "totalVulns",
      title: t("kube.imageScan.colTotalVulns"),
      align: "right",
      render: (r) => <span className="tabular-nums">{r.totalVulns ?? 0}</span>,
    },
    {
      key: "criticalCnt",
      title: t("kube.imageScan.colCritical"),
      align: "right",
      render: (r) => <span className="tabular-nums text-danger font-semibold">{r.criticalCnt ?? 0}</span>,
    },
    {
      key: "highCnt",
      title: t("kube.imageScan.colHigh"),
      align: "right",
      render: (r) => <span className="tabular-nums text-warning">{r.highCnt ?? 0}</span>,
    },
    {
      key: "status",
      title: t("common.status"),
      render: (r) => {
        const meta = statusMeta[r.status] ?? { label: r.status || "—", tone: "neutral" as ScanTone };
        return <StatusTag tone={meta.tone}>{meta.label}</StatusTag>;
      },
    },
    {
      key: "scannedAt",
      title: t("kube.imageScan.colScannedAt"),
      render: (r) => <span className="text-faint tabular-nums">{r.scannedAt || "—"}</span>,
    },
    {
      key: "actions",
      title: t("common.actions"),
      align: "right",
      render: (r) => (
        <div className="flex justify-end gap-3">
          <button
            type="button"
            className="text-sm text-muted transition-colors hover:text-ink"
            onClick={(e) => {
              e.stopPropagation();
              setDetail(r);
            }}
          >
            {t("common.details")}
          </button>
        </div>
      ),
    },
  ];

  return (
    <>
      <div className="space-y-4">
        <FilterBar extra={<Button onClick={() => setScanOpen(true)}>{t("kube.imageScan.scan")}</Button>}>
          <SearchInput
            value={params.image}
            onChange={(v) => setParams((p) => ({ ...p, image: v, page: 1 }))}
            placeholder={t("kube.imageScan.searchPlaceholder")}
          />
          <Select
            value={params.status}
            onChange={(v) => setParams((p) => ({ ...p, status: v, page: 1 }))}
            options={statusOptions}
          />
        </FilterBar>
        <Card>
          <DataTable
            columns={columns}
            rows={data?.items ?? []}
            rowKey={(r) => r.id ?? r.digest ?? r.image}
            loading={isLoading}
            emptyText={t("kube.imageScan.empty")}
            onRowClick={(r) => setDetail(r)}
          />
          <Pagination
            page={params.page}
            pageSize={params.page_size}
            total={data?.total ?? 0}
            onChange={(page) => setParams((p) => ({ ...p, page }))}
          />
        </Card>
      </div>

      {/* 扫描镜像 */}
      <Modal
        open={scanOpen}
        onClose={() => setScanOpen(false)}
        title={t("kube.imageScan.scanTitle")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setScanOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={() => scanMutation.mutate(scanImage)} disabled={scanMutation.isPending || !scanImage.trim()}>
              {scanMutation.isPending ? t("common.submitting") : t("kube.imageScan.scanSubmit")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <FormField label={t("kube.imageScan.fieldImage")} required>
            <Input value={scanImage} onChange={(e) => setScanImage(e.target.value)} placeholder={t("kube.imageScan.fieldImagePlaceholder")} />
          </FormField>
          <p className="text-xs text-faint">{t("kube.imageScan.scanHint")}</p>
        </div>
      </Modal>

      {/* 详情 Drawer */}
      <Drawer
        open={!!detail}
        onClose={() => setDetail(null)}
        title={t("kube.imageScan.detailTitle")}
        width={640}
      >
        {detail && (
          <div className="space-y-5">
            <div className="rounded-card border border-border bg-surface-muted/50 p-4">
              <div className="flex items-center gap-2">
                {(() => {
                  const meta = statusMeta[detail.status] ?? { label: detail.status || "—", tone: "neutral" as ScanTone };
                  return <StatusTag tone={meta.tone}>{meta.label}</StatusTag>;
                })()}
                <span className="font-mono font-semibold text-ink break-all">{detail.image}</span>
              </div>
              {detail.errorMsg && <p className="mt-2 text-sm text-danger">{detail.errorMsg}</p>}
            </div>

            <div className="space-y-2">
              <Field label={t("kube.imageScan.fieldDigest")} value={<span className="font-mono">{detail.digest || "—"}</span>} />
              <Field label={t("kube.imageScan.fieldOs")} value={detail.os || "—"} />
              <Field label={t("kube.imageScan.fieldTotalVulns")} value={<span className="tabular-nums">{detail.totalVulns ?? 0}</span>} />
              <Field label={t("kube.imageScan.fieldCritical")} value={<span className="tabular-nums text-danger font-semibold">{detail.criticalCnt ?? 0}</span>} />
              <Field label={t("kube.imageScan.fieldHigh")} value={<span className="tabular-nums text-warning">{detail.highCnt ?? 0}</span>} />
              <Field label={t("kube.imageScan.fieldScannedAt")} value={<span className="tabular-nums">{detail.scannedAt || "—"}</span>} />
            </div>

            <div className="space-y-3">
              <h4 className="text-[13px] font-semibold text-muted">{t("kube.imageScan.vulnListTitle")}</h4>
              {vulnsQuery.isLoading && <div className="text-sm text-muted">{t("kube.imageScan.vulnLoading")}</div>}
              {vulnsQuery.isError && <div className="text-sm text-danger">{t("kube.imageScan.vulnLoadError")}</div>}
              {vulnsQuery.data && vulnsQuery.data.length === 0 && <EmptyState title={t("kube.imageScan.vulnEmpty")} desc="" />}
              {vulnsQuery.data && vulnsQuery.data.length > 0 && (
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-[11px] uppercase tracking-wide text-faint">
                      <th className="py-1 text-left font-semibold">{t("kube.imageScan.vulnColCve")}</th>
                      <th className="py-1 text-left font-semibold">{t("kube.imageScan.vulnColLevel")}</th>
                      <th className="py-1 text-left font-semibold">{t("kube.imageScan.vulnColPackage")}</th>
                      <th className="py-1 text-left font-semibold">{t("kube.imageScan.vulnColFixedVersion")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {vulnsQuery.data.map((v) => (
                      <tr key={v.id} className="border-t border-border">
                        <td className="py-1.5 font-mono text-ink">{v.cveId}</td>
                        <td className="py-1.5">
                          {isSeverity(v.severity) ? (
                            <SeverityTag level={v.severity} />
                          ) : (
                            <StatusTag tone="neutral">{v.severity || "—"}</StatusTag>
                          )}
                        </td>
                        <td className="py-1.5 text-muted">
                          {v.package || "—"}
                          {v.version ? <span className="text-faint"> @ {v.version}</span> : null}
                        </td>
                        <td className="py-1.5 font-mono text-muted">{v.fixedVersion || "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        )}
      </Drawer>
    </>
  );
}
