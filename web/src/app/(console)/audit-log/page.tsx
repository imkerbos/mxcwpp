"use client";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useUrlState } from "@/hooks/useUrlState";
import { auditApi } from "@/lib/api/audit";
import type { AuditLog } from "@/lib/api/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { Card } from "@/components/ui/Card";
import { DataTable, type Column } from "@/components/ui/DataTable";
import { Pagination } from "@/components/ui/Pagination";
import { FilterBar } from "@/components/ui/FilterBar";
import { SearchInput } from "@/components/ui/SearchInput";
import { Select } from "@/components/ui/Select";
import { StatusTag } from "@/components/ui/Tag";

interface ListParams {
  page: number;
  page_size: number;
  username: string;
  resource_type: string;
  action: string;
}

type Tone = "success" | "warning" | "danger" | "info" | "neutral";

const buildResourceTypeOptions = (t: TFunction) => [
  { label: t("audit.allResource"), value: "" },
  { label: t("audit.resource.hosts"), value: "hosts" },
  { label: t("audit.resource.policies"), value: "policies" },
  { label: t("audit.resource.rules"), value: "rules" },
  { label: t("audit.resource.tasks"), value: "tasks" },
  { label: t("audit.resource.users"), value: "users" },
  { label: t("audit.resource.alerts"), value: "alerts" },
  { label: t("audit.resource.notifications"), value: "notifications" },
  { label: t("audit.resource.system-config"), value: "system-config" },
  { label: t("audit.resource.fim-policies"), value: "fim-policies" },
];
const buildActionOptions = (t: TFunction) => [
  { label: t("audit.allAction"), value: "" },
  { label: "POST", value: "POST" },
  { label: "PUT", value: "PUT" },
  { label: "DELETE", value: "DELETE" },
];

function actionTone(action: string): Tone {
  if (action === "POST") return "success";
  if (action === "PUT") return "info";
  if (action === "DELETE") return "danger";
  return "neutral";
}
function statusTone(code: number): Tone {
  if (code < 300) return "success";
  if (code < 400) return "info";
  if (code < 500) return "warning";
  return "danger";
}

export default function AuditLogPage() {
  const { t } = useTranslation();
  const resourceTypeOptions = buildResourceTypeOptions(t);
  const actionOptions = buildActionOptions(t);
  const [params, setParams] = useUrlState({
    page: 1,
    page_size: 20,
    username: "",
    resource_type: "",
    action: "",
  });

  const { data, isLoading } = useQuery({
    queryKey: ["audit-logs", params],
    queryFn: () =>
      auditApi.list({
        page: params.page,
        page_size: params.page_size,
        username: params.username || undefined,
        resource_type: params.resource_type || undefined,
        action: params.action || undefined,
      }),
  });

  const columns: Column<AuditLog>[] = [
    {
      key: "created_at",
      title: t("audit.colTime"),
      render: (r) => <span className="text-faint tabular-nums">{r.created_at}</span>,
    },
    { key: "username", title: t("audit.colUser"), render: (r) => <span className="font-medium text-ink">{r.username}</span> },
    {
      key: "action",
      title: t("audit.colAction"),
      render: (r) => <StatusTag tone={actionTone(r.action)}>{r.action}</StatusTag>,
    },
    {
      key: "resource",
      title: t("audit.colResource"),
      render: (r) => (
        <div className="leading-tight">
          <div className="font-medium text-ink">{r.resource_type}</div>
          {r.resource_id && <div className="text-xs text-faint">{r.resource_id}</div>}
        </div>
      ),
    },
    {
      key: "path",
      title: t("audit.colPath"),
      render: (r) => <span className="font-mono text-xs text-muted">{r.path}</span>,
    },
    { key: "ip", title: "IP", render: (r) => <span className="text-muted">{r.ip}</span> },
    {
      key: "status_code",
      title: t("audit.colStatusCode"),
      render: (r) => <StatusTag tone={statusTone(r.status_code)}>{r.status_code}</StatusTag>,
    },
  ];

  return (
    <>
      <PageHeader title={t("audit.title")} desc={t("audit.desc")} />
      <div className="space-y-4">
        <FilterBar>
          <SearchInput
            value={params.username}
            onChange={(v) => setParams((p) => ({ ...p, username: v, page: 1 }))}
            placeholder={t("audit.search")}
          />
          <Select
            value={params.resource_type}
            onChange={(v) => setParams((p) => ({ ...p, resource_type: v, page: 1 }))}
            options={resourceTypeOptions}
          />
          <Select
            value={params.action}
            onChange={(v) => setParams((p) => ({ ...p, action: v, page: 1 }))}
            options={actionOptions}
          />
        </FilterBar>
        <Card>
          <DataTable
            columns={columns}
            rows={data?.items ?? []}
            rowKey={(r) => r.id}
            loading={isLoading}
            emptyText={t("audit.empty")}
          />
          <Pagination
            page={params.page}
            pageSize={params.page_size}
            total={data?.total ?? 0}
            onChange={(page) => setParams((p) => ({ ...p, page }))}
          />
        </Card>
      </div>
    </>
  );
}
