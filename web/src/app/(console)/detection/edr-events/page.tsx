"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Activity, Cpu, FileText, Network } from "lucide-react";
import { useUrlState } from "@/hooks/useUrlState";
import { detectionApi } from "@/lib/api/detection";
import type { EdrEvent } from "@/lib/api/types";
import { Card } from "@/components/ui/Card";
import { DataTable, type Column } from "@/components/ui/DataTable";
import { Pagination } from "@/components/ui/Pagination";
import { FilterBar } from "@/components/ui/FilterBar";
import { SearchInput } from "@/components/ui/SearchInput";
import { Select } from "@/components/ui/Select";
import { Drawer } from "@/components/ui/Drawer";
import { StatCard } from "@/components/ui/StatCard";
import { StatusTag } from "@/components/ui/Tag";

interface ListParams {
  page: number;
  page_size: number;
  keyword: string;
  data_type: string;
  host_id: string;
}

const buildDataTypeLabel = (t: TFunction): Record<number, string> => ({
  3000: t("detection.edrEvents.typeProcess"),
  3001: t("detection.edrEvents.typeFile"),
  3002: t("detection.edrEvents.typeNetwork"),
  3003: t("detection.edrEvents.typeOther"),
});

const buildDataTypeOptions = (t: TFunction) => [
  { label: t("common.allType"), value: "" },
  { label: t("detection.edrEvents.optProcess"), value: "3000" },
  { label: t("detection.edrEvents.optFile"), value: "3001" },
  { label: t("detection.edrEvents.optNetwork"), value: "3002" },
  { label: t("detection.edrEvents.optOther"), value: "3003" },
];

function dash(v: string | undefined) {
  return v && v.length > 0 ? v : "—";
}

function Field({ label, value, mono }: { label: string; value: React.ReactNode; mono?: boolean }) {
  return (
    <div className="flex gap-3 text-sm">
      <span className="w-24 shrink-0 text-muted">{label}</span>
      <span className={`min-w-0 break-all text-ink${mono ? " font-mono text-xs" : ""}`}>{value}</span>
    </div>
  );
}

export default function EdrEventsPage() {
  const { t } = useTranslation();
  const DATA_TYPE_LABEL = buildDataTypeLabel(t);
  const dataTypeOptions = buildDataTypeOptions(t);
  const [params, setParams] = useUrlState({
    page: 1,
    page_size: 20,
    keyword: "",
    data_type: "",
    host_id: "",
  });

  const { data: stats } = useQuery({
    queryKey: ["edr-stats"],
    queryFn: () => detectionApi.edrEventStats(),
  });

  const { data, isLoading } = useQuery({
    queryKey: ["edr-events", params],
    queryFn: () =>
      detectionApi.listEdrEvents({
        page: params.page,
        page_size: params.page_size,
        keyword: params.keyword || undefined,
        data_type: params.data_type ? Number(params.data_type) : undefined,
        host_id: params.host_id || undefined,
      }),
  });

  const [detail, setDetail] = useState<EdrEvent | null>(null);

  const columns: Column<EdrEvent>[] = [
    {
      key: "timestamp",
      title: t("detection.edrEvents.colTime"),
      render: (r) => <span className="text-faint tabular-nums">{r.timestamp}</span>,
    },
    { key: "hostname", title: t("detection.edrEvents.colHost"), render: (r) => <span className="font-medium text-ink">{r.hostname || r.host_id}</span> },
    { key: "event_type", title: t("detection.edrEvents.colEventType"), render: (r) => <StatusTag tone="neutral">{r.event_type}</StatusTag> },
    {
      key: "exe",
      title: t("detection.edrEvents.colExe"),
      render: (r) => <span className="block max-w-[240px] truncate font-mono text-xs text-ink">{dash(r.exe)}</span>,
    },
    {
      key: "file_path",
      title: t("detection.edrEvents.colFilePath"),
      render: (r) => <span className="block max-w-[240px] truncate font-mono text-xs text-ink">{dash(r.file_path)}</span>,
    },
    {
      key: "remote_addr",
      title: t("detection.edrEvents.colRemoteAddr"),
      render: (r) => <span className="font-mono text-xs text-ink">{dash(r.remote_addr)}</span>,
    },
  ];

  return (
    <>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4 mb-5">
        <StatCard compact label={t("detection.edrEvents.statTotal")} value={(stats?.total ?? 0).toLocaleString()} icon={Activity} tone="default" />
        <StatCard compact label={t("detection.edrEvents.statProcessExec")} value={(stats?.process_exec ?? 0).toLocaleString()} icon={Cpu} tone="default" />
        <StatCard compact label={t("detection.edrEvents.statFileOp")} value={(stats?.file_open ?? 0).toLocaleString()} icon={FileText} tone="default" />
        <StatCard compact label={t("detection.edrEvents.statNetwork")} value={(stats?.network_connect ?? 0).toLocaleString()} icon={Network} tone="default" />
      </div>

      <div className="space-y-4">
        <FilterBar>
          <SearchInput
            value={params.keyword}
            onChange={(v) => setParams((p) => ({ ...p, keyword: v, page: 1 }))}
            placeholder={t("detection.edrEvents.searchPlaceholder")}
          />
          <Select
            value={params.data_type}
            onChange={(v) => setParams((p) => ({ ...p, data_type: v, page: 1 }))}
            options={dataTypeOptions}
          />
        </FilterBar>
        <Card>
          <DataTable
            columns={columns}
            rows={data?.items ?? []}
            rowKey={(r) => `${r.host_id}-${r.timestamp}-${r.pid}`}
            loading={isLoading}
            emptyText={t("detection.edrEvents.empty")}
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

      <Drawer open={!!detail} onClose={() => setDetail(null)} title={t("detection.edrEvents.detailTitle")} width={560}>
        {detail && (
          <div className="space-y-5">
            <div className="space-y-2">
              <Field label={t("detection.edrEvents.fieldTime")} value={<span className="tabular-nums">{detail.timestamp}</span>} />
              <Field label={t("detection.edrEvents.fieldHost")} value={detail.hostname || detail.host_id} />
              <Field label={t("detection.edrEvents.fieldHostId")} value={detail.host_id} mono />
              <Field label={t("detection.edrEvents.fieldEventType")} value={<StatusTag tone="neutral">{detail.event_type}</StatusTag>} />
              <Field label={t("detection.edrEvents.fieldDataType")} value={`${detail.data_type} ${DATA_TYPE_LABEL[detail.data_type] ?? ""}`.trim()} />
              <Field label={t("detection.edrEvents.fieldPid")} value={dash(detail.pid)} mono />
              <Field label={t("detection.edrEvents.fieldExe")} value={dash(detail.exe)} mono />
              <Field label={t("detection.edrEvents.fieldFilePath")} value={dash(detail.file_path)} mono />
              <Field label={t("detection.edrEvents.fieldRemoteAddr")} value={dash(detail.remote_addr)} mono />
            </div>
          </div>
        )}
      </Drawer>
    </>
  );
}
