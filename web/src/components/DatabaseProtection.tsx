import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  Copy,
  Database,
  FileText,
  Loader2,
  RotateCcw,
  ShieldCheck,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, type DatabaseDump, type ProtectionLevel } from "@/lib/api";
import { cn } from "@/lib/utils";

/**
 * How well each database in a snapshot can actually be brought back.
 *
 * The question someone has in front of a backup is not which mechanism ran but
 * whether their database comes back. Three different answers exist — exported,
 * consistently captured, copied as files — and a snapshot that shows only the
 * successes leaves the gaps to be discovered at restore time.
 */
const LEVELS: Record<
  ProtectionLevel,
  { label: string; icon: typeof Database; badge: string; text: string }
> = {
  exported: {
    label: "Exported",
    icon: ShieldCheck,
    badge: "bg-success/15 text-success border-success/30",
    text: "A logical dump. It can be replayed into any compatible server.",
  },
  consistent: {
    label: "Captured consistently",
    icon: Check,
    badge: "bg-success/15 text-success border-success/30",
    text: "Taken through the engine's own backup API, so the file is coherent.",
  },
  files_only: {
    label: "Files only",
    icon: AlertTriangle,
    badge: "bg-warning/15 text-warning border-warning/30",
    text: "Only the data directory was copied.",
  },
};

// Products have names, and "Postgresql" is not one of them. CSS capitalisation
// of a machine identifier gets them all subtly wrong.
const TECHNOLOGY_NAMES: Record<string, string> = {
  postgresql: "PostgreSQL",
  mysql: "MySQL",
  mariadb: "MariaDB",
  mongodb: "MongoDB",
  sqlite: "SQLite",
  redis: "Redis",
  valkey: "Valkey",
};

/**
 * Exactly how much a restore replaces, per engine.
 *
 * Not one sentence for all of them, because they do not do the same thing:
 * mongorestore drops the collections its archive holds and leaves the rest,
 * while the SQL dumps drop and recreate whole databases. Verified against each
 * engine — a collection created after the backup does survive a MongoDB
 * restore, and saying otherwise would be a promise this cannot keep.
 */
const REPLACEMENT_SCOPE: Record<string, string> = {
  postgresql:
    "Every database in the export is dropped and recreated as it was when the backup ran.",
  mysql: "Every database in the export is dropped and recreated as it was when the backup ran.",
  mariadb: "Every database in the export is dropped and recreated as it was when the backup ran.",
  mongodb:
    "Every collection in the export is dropped and restored. Collections created since the backup are left where they are — mongorestore replaces what it holds, not the whole database.",
  default: "The contents of the export replace what is in the database now.",
};

function formatBytes(bytes: number): string {
  if (!bytes) return "";
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

export function DatabaseProtection({
  databases,
  snapshotId,
}: {
  databases: DatabaseDump[];
  /** Omitted where no snapshot context exists; the restore action is then hidden. */
  snapshotId?: string;
}) {
  if (databases.length === 0) return null;

  return (
    <div className="space-y-2">
      <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
        Databases in this snapshot
      </p>
      <ul className="space-y-2">
        {databases.map((database) => (
          <DatabaseRow
            key={`${database.service}-${database.technology}`}
            database={database}
            snapshotId={snapshotId}
          />
        ))}
      </ul>
    </div>
  );
}

function DatabaseRow({
  database,
  snapshotId,
}: {
  database: DatabaseDump;
  snapshotId?: string;
}) {
  const level = LEVELS[database.level] ?? LEVELS.files_only;
  const Icon = level.icon;

  return (
    <li className="rounded-lg border bg-background p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Icon
          className={cn(
            "size-4 shrink-0",
            database.level === "files_only" ? "text-warning" : "text-success",
          )}
        />
        <span className="font-medium">
          {TECHNOLOGY_NAMES[database.technology] ?? database.technology}
        </span>
        <span className="font-mono text-xs text-muted-foreground">{database.service}</span>
        <Badge variant="outline" className={cn("font-medium", level.badge)}>
          {level.label}
        </Badge>
        {database.bytes > 0 && (
          <span className="text-xs text-muted-foreground">{formatBytes(database.bytes)}</span>
        )}
      </div>

      <p className="mt-1.5 text-xs text-muted-foreground">{database.note || level.text}</p>

      {/* Proven rather than assumed. An export that cannot be restored looks
          exactly like one that can, so this is the only line here that
          describes evidence instead of intent. */}
      {database.restoreCheck && (
        <p
          className={cn(
            "mt-1.5 flex items-start gap-1.5 text-xs",
            database.restoreCheck.loaded ? "text-success" : "text-destructive",
          )}
        >
          {database.restoreCheck.loaded ? (
            <>
              <ShieldCheck className="mt-px size-3.5 shrink-0" />
              Test-restored into an empty server: {database.restoreCheck.objects} table
              {database.restoreCheck.objects === 1 ? "" : "s"} came back.
            </>
          ) : (
            <>
              <AlertTriangle className="mt-px size-3.5 shrink-0" />
              Did not restore into an empty server: {database.restoreCheck.detail}
            </>
          )}
        </p>
      )}

      {/* Putting the database back — either by Back-Orbit, or by hand.
          Both are offered: the button is the practised path, and the command
          is what still works when this UI is the thing that is down. */}
      {database.replay && (
        <div className="mt-2 space-y-2">
          {snapshotId && database.level === "exported" && (
            <RestoreAction database={database} snapshotId={snapshotId} />
          )}
          <ReplayCommand command={database.replay} manual={Boolean(snapshotId)} />
        </div>
      )}
    </li>
  );
}

/**
 * Replaying an export into the running service.
 *
 * This is the only control in Back-Orbit that writes into a database someone
 * is using, so it asks for the service name to be typed — the same bar as
 * deleting a repository. The server enforces the same rule independently; the
 * dialog is the explanation, not the safeguard.
 */
function RestoreAction({
  database,
  snapshotId,
}: {
  database: DatabaseDump;
  snapshotId: string;
}) {
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!open) setTyped("");
  }, [open]);

  const restore = useMutation({
    mutationFn: () => api.restoreDatabase(snapshotId, database.service, typed),
    onSuccess: () => {
      setOpen(false);
      toast.success(
        `Restoring ${database.service}. Watch it finish under Restore.`,
      );
      queryClient.invalidateQueries({ queryKey: ["restores"] });
    },
    onError: (error: Error) => toast.error(error.message),
  });

  const matches = typed === database.service;

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        onClick={() => setOpen(true)}
        className="border-destructive/40 text-destructive hover:bg-destructive/10 hover:text-destructive"
      >
        <RotateCcw className="size-3.5" />
        Restore into {database.service}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Restore “{database.service}” from this snapshot?</DialogTitle>
            <DialogDescription>
              The export is replayed into the running{" "}
              {TECHNOLOGY_NAMES[database.technology] ?? database.technology} server.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div
              role="alert"
              className="flex gap-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              <div className="space-y-1">
                <p className="font-medium">
                  Everything currently in this database is replaced.
                </p>
                <p>{REPLACEMENT_SCOPE[database.technology] ?? REPLACEMENT_SCOPE.default}</p>
                <p>Anything written since the backup is gone, and nothing here can bring it back.</p>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor={`confirm-restore-${database.service}`}>
                Type <span className="font-mono font-semibold">{database.service}</span> to
                confirm
              </Label>
              <Input
                id={`confirm-restore-${database.service}`}
                value={typed}
                autoComplete="off"
                onChange={(event) => setTyped(event.target.value)}
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={restore.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => restore.mutate()}
              disabled={!matches || restore.isPending}
              aria-busy={restore.isPending}
            >
              {restore.isPending && <Loader2 className="size-4 animate-spin" />}
              Replace the database
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function ReplayCommand({ command, manual }: { command: string; manual: boolean }) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard access can be refused; the command stays selectable by hand.
      setCopied(false);
    }
  };

  return (
    <div className="space-y-1">
      <p className="text-xs text-muted-foreground">
        {manual ? "Or put it back yourself:" : "To put this database back:"}
      </p>
      <div className="flex items-start gap-2">
        <code className="min-w-0 flex-1 rounded-md bg-muted/60 px-2 py-1.5 font-mono text-xs break-all">
          {command}
        </code>
        <Button
          variant="ghost"
          size="sm"
          onClick={copy}
          aria-label="Copy the restore command"
          className="shrink-0"
        >
          {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Run it from the project directory, against the extracted snapshot. It replaces the
        database's current contents.
        {manual && " Useful for restoring into a different server than the one this came from."}
      </p>
    </div>
  );
}

/** A compact count for places that only need the headline. */
export function DatabaseProtectionSummary({ databases }: { databases: DatabaseDump[] }) {
  if (databases.length === 0) return null;

  const exported = databases.filter((d) => d.level !== "files_only").length;
  const filesOnly = databases.length - exported;

  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
      <FileText className="size-3.5" />
      {exported > 0 && (
        <span className="text-success">
          {exported} database{exported === 1 ? "" : "s"} exported
        </span>
      )}
      {exported > 0 && filesOnly > 0 && <span>·</span>}
      {filesOnly > 0 && (
        <span className="text-warning">
          {filesOnly} copied as files
        </span>
      )}
    </span>
  );
}
