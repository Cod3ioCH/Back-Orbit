import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { ProjectStatus } from "@/lib/api";

const STATUS_STYLES: Record<ProjectStatus, string> = {
  healthy: "bg-success/15 text-success border-success/30",
  running: "bg-success/15 text-success border-success/30",
  warning: "bg-warning/15 text-warning border-warning/30",
  paused: "bg-muted text-muted-foreground border-border",
  unprotected: "bg-muted text-muted-foreground border-border",
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
