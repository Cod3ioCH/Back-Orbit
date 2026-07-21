import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertTriangle,
  CheckCircle2,
  CircleSlash,
  Loader2,
  Play,
  ShieldCheck,
  XCircle,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Timestamp } from "@/components/Timestamp";
import {
  api,
  ApiError,
  type BackupPhase,
  type BackupRun,
  type BackupRunStatus,
  type ProjectDetail,
} from "@/lib/api";
import { cn } from "@/lib/utils";

const PHASE_LABELS: Record<BackupPhase, string> = {
  preparing: "Preparing",
  staging: "Reading the volumes",
  snapshotting: "Writing the snapshot",
  verifying: "Reading the backup back",
  finished: "Finished",
};

const STATUS_LABELS: Record<BackupRunStatus, string> = {
  running: "Running",
  completed: "Verified",
  completed_with_warnings: "Verified, with warnings",
  failed: "Failed",
  cancelled: "Cancelled",
};

const STATUS_STYLES: Record<BackupRunStatus, string> = {
  running: "bg-muted text-muted-foreground border-border",
  completed: "bg-success/15 text-success border-success/30",
  completed_with_warnings: "bg-warning/15 text-warning border-warning/30",
  failed: "bg-destructive/15 text-destructive border-destructive/30",
  cancelled: "bg-muted text-muted-foreground border-border",
};

function StatusIcon({ status }: { status: BackupRunStatus }) {
  switch (status) {
    case "running":
      return <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" />;
    case "completed":
      return <ShieldCheck className="size-4 shrink-0 text-success" />;
    case "completed_with_warnings":
      return <AlertTriangle className="size-4 shrink-0 text-warning" />;
    case "failed":
      return <XCircle className="size-4 shrink-0 text-destructive" />;
    case "cancelled":
      return <CircleSlash className="size-4 shrink-0 text-muted-foreground" />;
  }
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`;
}

/**
 * Starting point for a backup, and the record of what happened to previous
 * ones.
 *
 * The list deliberately keeps failed and cancelled runs. A backup history that
 * only shows successes is the one thing worse than no history: it reads as
 * "everything is fine" precisely when it is not.
 */
export function BackupPanel({ project }: { project: ProjectDetail }) {
  const queryClient = useQueryClient();
  const [repositoryId, setRepositoryId] = useState("");

  const repositories = useQuery({ queryKey: ["repositories"], queryFn: api.listRepositories });

  const runs = useQuery({
    queryKey: ["backup-runs", project.id],
    queryFn: () => api.listBackupRuns(25),
    select: (all) => all.filter((run) => run.projectId === project.id),
    // While something is running the row is the only progress indicator there
    // is, so it is polled rather than left until the next navigation.
    refetchInterval: (query) =>
      query.state.data?.some((run) => run.status === "running") ? 1500 : false,
  });

  const activeRun = runs.data?.find((run) => run.status === "running");

  const startMutation = useMutation({
    mutationFn: (id: string) => api.startBackup(project.id, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["backup-runs", project.id] });
      toast.success("Backup started.");
    },
    onError: (error) =>
      toast.error(error instanceof ApiError ? error.message : "Could not start the backup."),
  });

  const cancelMutation = useMutation({
    mutationFn: (id: string) => api.cancelBackupRun(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["backup-runs", project.id] });
      toast.info("Cancelling…");
    },
    onError: (error) =>
      toast.error(error instanceof ApiError ? error.message : "Could not cancel."),
  });

  const ready = repositories.data?.filter((repo) => repo.status === "ready") ?? [];
  const selected = repositoryId || ready[0]?.id || "";

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-3">
        <CardTitle className="text-base">Backups</CardTitle>

        {activeRun ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => cancelMutation.mutate(activeRun.id)}
            disabled={cancelMutation.isPending}
            aria-busy={cancelMutation.isPending}
          >
            {cancelMutation.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <CircleSlash className="size-4" />
            )}
            Cancel
          </Button>
        ) : (
          <div className="flex flex-wrap items-end gap-2">
            <div className="space-y-1">
              <Label htmlFor="backup-repository" className="text-xs text-muted-foreground">
                Destination
              </Label>
              <select
                id="backup-repository"
                value={selected}
                onChange={(event) => setRepositoryId(event.target.value)}
                disabled={ready.length === 0}
                className="h-9 rounded-lg border border-border bg-background px-3 text-sm disabled:opacity-50"
              >
                {ready.length === 0 ? (
                  <option value="">No ready repository</option>
                ) : (
                  ready.map((repo) => (
                    <option key={repo.id} value={repo.id}>
                      {repo.name}
                    </option>
                  ))
                )}
              </select>
            </div>
            <Button
              size="sm"
              onClick={() => startMutation.mutate(selected)}
              disabled={!selected || startMutation.isPending || project.volumes.length === 0}
              aria-busy={startMutation.isPending}
            >
              {startMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Play className="size-4" />
              )}
              Back up now
            </Button>
          </div>
        )}
      </CardHeader>

      <CardContent className="space-y-3">
        {/* Said before the button is pressed rather than after: these are the
            two reasons a backup cannot start, and each has a different fix. */}
        {ready.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No repository is ready yet. Add one under Repositories and initialise it first.
          </p>
        )}
        {project.volumes.length === 0 && (
          <p className="text-sm text-muted-foreground">
            This project has no named volumes, so there is nothing to back up yet.
          </p>
        )}

        {runs.isLoading ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : !runs.data || runs.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No backup has run for this project yet.
          </p>
        ) : (
          <ul className="divide-y divide-border">
            {runs.data.map((run) => (
              <RunRow key={run.id} run={run} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function RunRow({ run }: { run: BackupRun }) {
  const [expanded, setExpanded] = useState(false);
  const snapshot = run.snapshot;

  return (
    <li className="py-3 first:pt-0 last:pb-0">
      <div className="flex flex-wrap items-start gap-3">
        <StatusIcon status={run.status} />

        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className={cn("font-medium", STATUS_STYLES[run.status])}>
              {STATUS_LABELS[run.status]}
            </Badge>
            {run.status === "running" && (
              <span className="text-sm text-muted-foreground">{PHASE_LABELS[run.phase]}…</span>
            )}
            <span className="text-sm text-muted-foreground">
              <Timestamp iso={run.startedAt} /> → {run.repositoryName}
            </span>
          </div>

          {run.status !== "running" && (
            <p className="text-xs text-muted-foreground">
              {run.filesTotal} files, {formatBytes(run.bytesTotal)}
              {run.bytesAdded > 0 && ` · ${formatBytes(run.bytesAdded)} new in the repository`}
            </p>
          )}

          {run.error && <p className="max-w-2xl text-xs text-destructive">{run.error}</p>}

          {run.warnings.map((warning) => (
            <p key={warning} className="max-w-2xl text-xs text-warning">
              {warning}
            </p>
          ))}

          {/* The verification is spelled out rather than reduced to a tick:
              what was checked is the difference between a backup that was
              reported and one that was confirmed. */}
          {snapshot && (
            <div className="space-y-1 pt-1">
              <button
                type="button"
                onClick={() => setExpanded((open) => !open)}
                className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
              >
                <CheckCircle2 className="size-3.5 text-success" />
                Snapshot{" "}
                <span className="font-mono">{snapshot.resticSnapshotId.slice(0, 8)}</span>{" "}
                verified · {expanded ? "hide details" : "what was checked?"}
              </button>

              {expanded && (
                <div className="space-y-2 rounded-md border border-border bg-muted/40 p-3 text-xs">
                  <ul className="space-y-0.5">
                    {(snapshot.verification.checks ?? []).map((check) => (
                      <li key={check} className="text-muted-foreground">
                        ✓ {check}
                      </li>
                    ))}
                  </ul>
                  <p className="text-muted-foreground">
                    The stored data blocks were not re-read — that is what a scheduled
                    repository check covers.
                  </p>

                  {snapshot.manifest.volumes.map((volume) => (
                    <div key={volume.name} className="border-t border-border pt-2">
                      <p className="font-medium">{volume.name}</p>
                      <p className="text-muted-foreground">
                        {volume.files} entries · {formatBytes(volume.bytes)} ·{" "}
                        {volume.ownership.length} ownership records kept
                      </p>
                      {!volume.ownershipPreserved && (
                        <p className="text-warning">
                          Original file owners could not be applied to the staged copy, so
                          they are recorded with the snapshot for a restore to reapply.
                        </p>
                      )}
                    </div>
                  ))}

                  <p className="border-t border-border pt-2 text-muted-foreground">
                    Restorable with restic alone:{" "}
                    <span className="font-mono break-all">
                      restic -r &lt;repository&gt; restore {snapshot.resticSnapshotId.slice(0, 8)}{" "}
                      --target /somewhere
                    </span>
                  </p>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </li>
  );
}
