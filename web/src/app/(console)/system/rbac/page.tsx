"use client";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { systemApi } from "@/lib/api/system";
import type { Permission } from "@/lib/api/types";
import { Card, CardHeader } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils/cn";
import { toast } from "@/components/ui/toast";

export default function RbacPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const moduleLabels: Record<string, string> = {
    core: t("system.rbac.moduleCore"),
    security: t("system.rbac.moduleSecurity"),
    system: t("system.rbac.moduleSystem"),
  };

  const { data: roles, isLoading: rolesLoading } = useQuery({
    queryKey: ["sys-roles"],
    queryFn: () => systemApi.listRoles(),
  });
  const { data: permissions, isLoading: permsLoading } = useQuery({
    queryKey: ["sys-permissions"],
    queryFn: () => systemApi.listPermissions(),
  });

  const [selectedRole, setSelectedRole] = useState<string>("");

  // default to first role once loaded
  useEffect(() => {
    if (!selectedRole && roles && roles.length > 0) {
      setSelectedRole(roles[0].code);
    }
  }, [roles, selectedRole]);

  const { data: rolePerms, isLoading: rolePermsLoading } = useQuery({
    queryKey: ["sys-role-perms", selectedRole],
    queryFn: () => systemApi.getRolePermissions(selectedRole),
    enabled: !!selectedRole,
  });

  const [checked, setChecked] = useState<Set<string>>(new Set());
  const [dirty, setDirty] = useState(false);

  // sync local editable set when role perms change or role changes
  useEffect(() => {
    setChecked(new Set(rolePerms?.permissions ?? []));
    setDirty(false);
  }, [rolePerms, selectedRole]);

  const toggle = (code: string) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(code)) next.delete(code);
      else next.add(code);
      return next;
    });
    setDirty(true);
  };

  const grouped = useMemo(() => {
    const map = new Map<string, Permission[]>();
    for (const p of permissions ?? []) {
      const arr = map.get(p.module) ?? [];
      arr.push(p);
      map.set(p.module, arr);
    }
    return [...map.entries()];
  }, [permissions]);

  const saveMutation = useMutation({
    mutationFn: () => systemApi.updateRolePermissions(selectedRole, [...checked]),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["sys-role-perms", selectedRole] });
      setDirty(false);
      toast.success(t("system.rbac.updated"));
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const selectedRoleName = roles?.find((r) => r.code === selectedRole)?.name ?? "";

  return (
    <div className="grid grid-cols-1 gap-5 lg:grid-cols-[260px_1fr]">
      <Card>
        <CardHeader title={t("system.rbac.roles")} />
        <div className="px-3 pb-4">
          {rolesLoading && <p className="px-2 py-4 text-sm text-muted">{t("common.loading")}</p>}
          {!rolesLoading && (roles?.length ?? 0) === 0 && (
            <p className="px-2 py-4 text-sm text-muted">{t("system.rbac.emptyRoles")}</p>
          )}
          <div className="space-y-1">
            {roles?.map((role) => {
              const active = role.code === selectedRole;
              return (
                <button
                  key={role.code}
                  type="button"
                  onClick={() => setSelectedRole(role.code)}
                  className={cn(
                    "flex w-full flex-col rounded-control px-3 py-2 text-left transition-colors",
                    active ? "bg-primary/10 text-primary font-medium" : "hover:bg-bg",
                  )}
                >
                  <span className="text-sm">{role.name}</span>
                  <span className="text-xs text-faint">{role.code}</span>
                </button>
              );
            })}
          </div>
        </div>
      </Card>

      <Card>
        <CardHeader
          title={selectedRoleName ? t("system.rbac.permissionsOf", { role: selectedRoleName }) : t("system.rbac.permissions")}
          extra={
            <Button disabled={saveMutation.isPending || !dirty} onClick={() => saveMutation.mutate()}>
              {saveMutation.isPending ? t("common.saving") : t("common.save")}
            </Button>
          }
        />
        <div className="px-5 pb-5">
          {(permsLoading || rolePermsLoading) && <p className="py-4 text-sm text-muted">{t("common.loading")}</p>}
          {!permsLoading && (permissions?.length ?? 0) === 0 && (
            <p className="py-4 text-sm text-muted">{t("system.rbac.emptyPermissions")}</p>
          )}
          {!permsLoading && !rolePermsLoading && !selectedRole && (
            <p className="py-4 text-sm text-muted">{t("system.rbac.selectRole")}</p>
          )}
          {!permsLoading && !rolePermsLoading && selectedRole && (
            <div className="space-y-6">
              {grouped.map(([module, perms]) => (
                <div key={module}>
                  <div className="mb-2 flex items-center gap-2">
                    <span className="text-sm font-semibold text-muted">{moduleLabels[module] ?? module}</span>
                    <span className="text-xs uppercase tracking-wide text-faint">{module}</span>
                  </div>
                  <div className="grid grid-cols-2 gap-2 lg:grid-cols-3">
                    {perms.map((p) => (
                      <label
                        key={p.code}
                        className="flex cursor-pointer items-start gap-2 rounded-control px-2 py-1.5 transition-colors hover:bg-bg"
                      >
                        <input
                          type="checkbox"
                          className="mt-0.5 h-4 w-4 accent-primary"
                          checked={checked.has(p.code)}
                          onChange={() => toggle(p.code)}
                        />
                        <span className="flex flex-col">
                          <span className="text-sm text-ink">{p.name}</span>
                          <span className="text-xs text-faint">{p.code}</span>
                        </span>
                      </label>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}
