"use client";
import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Globe, Hash, Network, Link2, Database } from "lucide-react";
import { detectionApi } from "@/lib/api/detection";
import type { ThreatIntelCheckResult } from "@/lib/api/types";
import { Card, CardHeader } from "@/components/ui/Card";
import { DataTable, type Column } from "@/components/ui/DataTable";
import { Pagination } from "@/components/ui/Pagination";
import { FilterBar } from "@/components/ui/FilterBar";
import { SearchInput } from "@/components/ui/SearchInput";
import { Select } from "@/components/ui/Select";
import { Button } from "@/components/ui/Button";
import { StatCard } from "@/components/ui/StatCard";
import { Tabs } from "@/components/ui/Tabs";
import { StatusTag } from "@/components/ui/Tag";
import { toast } from "@/components/ui/toast";

const IOC_TYPES = ["ip", "hash", "domain", "url"] as const;
type IocType = (typeof IOC_TYPES)[number];
const buildTypeLabels = (t: TFunction): Record<IocType, string> => ({
  ip: t("detection.threatIntel.typeIp"),
  hash: t("detection.threatIntel.typeHash"),
  domain: t("detection.threatIntel.typeDomain"),
  url: t("detection.threatIntel.typeUrl"),
});

interface IocRow {
  value: string;
}

export default function ThreatIntelPage() {
  const { t } = useTranslation();
  const typeLabels = buildTypeLabels(t);
  const typeTabs = IOC_TYPES.map((tp) => ({ key: tp, label: typeLabels[tp] }));
  const checkTypeOptions = IOC_TYPES.map((tp) => ({ label: typeLabels[tp], value: tp }));
  const iocColumns: Column<IocRow>[] = [
    { key: "value", title: t("detection.threatIntel.colIocValue"), render: (r) => <span className="break-all font-mono text-xs text-ink">{r.value}</span> },
  ];
  const [type, setType] = useState<IocType>("ip");
  const [page, setPage] = useState(1);
  const pageSize = 20;

  const { data: stats } = useQuery({
    queryKey: ["ti-stats"],
    queryFn: () => detectionApi.threatIntelStats(),
  });

  const { data: iocs, isLoading } = useQuery({
    queryKey: ["ti-iocs", type, page],
    queryFn: () => detectionApi.listIocs({ type, page, page_size: pageSize }),
  });

  const [checkType, setCheckType] = useState<IocType>("ip");
  const [checkValue, setCheckValue] = useState("");
  const [checkResult, setCheckResult] = useState<ThreatIntelCheckResult | null>(null);

  const checkMutation = useMutation({
    mutationFn: () => detectionApi.checkIoc(checkType, checkValue.trim()),
    onSuccess: (res) => setCheckResult(res),
    onError: (e: Error) => toast.error(e.message),
  });

  const syncMutation = useMutation({
    mutationFn: () => detectionApi.syncThreatIntel(),
    onSuccess: (res) => toast.success(res?.message || t("detection.threatIntel.syncTriggered")),
    onError: (e: Error) => toast.error(e.message),
  });

  const rows: IocRow[] = (iocs?.items ?? []).map((v) => ({ value: v }));

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
        <StatCard compact label={t("detection.threatIntel.statIp")} value={stats?.ip ?? 0} icon={Network} tone="default" />
        <StatCard compact label={t("detection.threatIntel.statHash")} value={stats?.hash ?? 0} icon={Hash} tone="default" />
        <StatCard compact label={t("detection.threatIntel.statDomain")} value={stats?.domain ?? 0} icon={Globe} tone="default" />
        <StatCard compact label={t("detection.threatIntel.statUrl")} value={stats?.url ?? 0} icon={Link2} tone="default" />
        <StatCard compact label={t("detection.threatIntel.statTotal")} value={stats?.total ?? 0} icon={Database} tone="success" />
      </div>

      <Card>
        <CardHeader
          title={t("detection.threatIntel.queryTitle")}
          extra={
            <Button variant="ghost" onClick={() => syncMutation.mutate()} disabled={syncMutation.isPending}>
              {syncMutation.isPending ? t("detection.threatIntel.syncing") : t("detection.threatIntel.sync")}
            </Button>
          }
        />
        <div className="px-5 pb-5">
          <div className="flex flex-wrap items-center gap-2">
            <Select value={checkType} onChange={(v) => setCheckType(v as IocType)} options={checkTypeOptions} />
            <SearchInput value={checkValue} onChange={setCheckValue} placeholder={t("detection.threatIntel.checkPlaceholder")} className="w-72" />
            <Button onClick={() => checkMutation.mutate()} disabled={!checkValue.trim() || checkMutation.isPending}>
              {checkMutation.isPending ? t("detection.threatIntel.checking") : t("detection.threatIntel.check")}
            </Button>
          </div>
          {checkResult && (
            <div className="mt-4 flex items-center gap-3 rounded-control bg-surface-muted px-4 py-3 text-sm">
              <StatusTag tone={checkResult.hit ? "danger" : "success"}>{checkResult.hit ? t("detection.threatIntel.hit") : t("detection.threatIntel.miss")}</StatusTag>
              <span className="text-muted">{typeLabels[checkResult.type as IocType] ?? checkResult.type}</span>
              <span className="break-all font-mono text-xs text-ink">{checkResult.value}</span>
            </div>
          )}
        </div>
      </Card>

      <div className="space-y-4">
        <FilterBar>
          <Tabs
            items={typeTabs}
            active={type}
            onChange={(k) => {
              setType(k as IocType);
              setPage(1);
            }}
          />
        </FilterBar>
        <Card>
          <DataTable columns={iocColumns} rows={rows} rowKey={(r) => r.value} loading={isLoading} emptyText={t("detection.threatIntel.empty")} />
          <Pagination page={page} pageSize={pageSize} total={iocs?.total ?? 0} onChange={setPage} />
        </Card>
      </div>
    </div>
  );
}
