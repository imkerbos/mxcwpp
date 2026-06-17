import { Card } from "./Card";
import { cn } from "@/lib/utils/cn";
import type { LucideIcon } from "lucide-react";

interface Props {
  label: string;
  value: string | number;
  icon: LucideIcon;
  tone?: "default" | "danger" | "warning" | "success";
  compact?: boolean;
}
const tones = {
  default: "text-primary bg-gradient-to-br from-primary/15 to-primary/5",
  danger: "text-danger bg-gradient-to-br from-danger/15 to-danger/5",
  warning: "text-warning bg-gradient-to-br from-warning/15 to-warning/5",
  success: "text-success bg-gradient-to-br from-success/15 to-success/5",
};

export function StatCard({ label, value, icon: Icon, tone = "default", compact = false }: Props) {
  return (
    <Card className={cn("flex items-center gap-3 transition-all duration-200 hover:-translate-y-0.5", compact ? "p-3.5" : "p-5 gap-4")}>
      <div className={cn("rounded-control flex items-center justify-center shrink-0", tones[tone], compact ? "h-9 w-9" : "h-11 w-11")}>
        <Icon size={compact ? 17 : 20} />
      </div>
      <div className="min-w-0">
        <div className={cn("font-bold text-ink leading-tight tabular-nums truncate", compact ? "text-xl" : "text-2xl")}>{value}</div>
        <div className={cn("text-muted truncate", compact ? "text-xs" : "text-sm")}>{label}</div>
      </div>
    </Card>
  );
}
