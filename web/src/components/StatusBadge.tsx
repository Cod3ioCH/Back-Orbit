import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { ProjectStatus } from "@/lib/api";

const STATUS_STYLES: Record<ProjectStatus, string> = {
  healthy: "bg-success/15 text-success border-success/30",
  running: "bg-success/15 text-success border-success/30",
  warning: "bg-warning/15 text-warning border-warning/30",
  paused: "bg-muted text-muted-foreground border-border",
  // "Unprotected" means nothing is backed up — a risk state, not a neutral
  // one. Styling it like a disabled chip understated the single most
  // important thing this product reports.
  unprotected: "bg-warning/15 text-warning border-warning/30",
  failed: "bg-destructive/15 text-destructive border-destructive/30",
};

const STATUS_LABELS: Record<ProjectStatus, string> = {
  healthy: "Healthy",
  running: "Running",
  warning: "Warning",
  paused: "Paused",
  unprotected: "Unprotected",
  failed: "Failed",
};

export function StatusBadge({ status }: { status: ProjectStatus }) {
  return (
    <Badge variant="outline" className={cn("font-medium", STATUS_STYLES[status])}>
      {STATUS_LABELS[status]}
    </Badge>
  );
}
