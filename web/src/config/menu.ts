import {
  LayoutDashboard, Database, Bell, Bug, ShieldCheck, FileSearch,
  ScanLine, Boxes, Zap, Wrench, Settings, Activity, ScrollText,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

export interface MenuItem { key: string; path: string; title: string; icon: LucideIcon; }

export const MENUS: MenuItem[] = [
  { key: "dashboard", path: "/dashboard", title: "安全概览", icon: LayoutDashboard },
  { key: "assets", path: "/assets", title: "资产中心", icon: Database },
  { key: "alert-center", path: "/alert-center", title: "告警中心", icon: Bell },
  { key: "vuln-management", path: "/vuln-management", title: "漏洞管理", icon: Bug },
  { key: "baseline", path: "/baseline", title: "基线安全", icon: ShieldCheck },
  { key: "fim", path: "/fim", title: "文件完整性", icon: FileSearch },
  { key: "virus", path: "/virus", title: "病毒查杀", icon: ScanLine },
  { key: "kube", path: "/kube", title: "容器集群", icon: Boxes },
  { key: "detection", path: "/detection", title: "威胁检测", icon: Zap },
  { key: "operations", path: "/operations", title: "运维中心", icon: Wrench },
  { key: "system", path: "/system", title: "系统管理", icon: Settings },
  { key: "monitoring", path: "/monitoring", title: "系统监控", icon: Activity },
  { key: "audit", path: "/audit-log", title: "审计日志", icon: ScrollText },
];
