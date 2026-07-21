import { useState } from "react";
import { AlertTriangle, Check, Copy, Database, FileText, ShieldCheck } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { DatabaseDump, ProtectionLevel } from "@/lib/api";
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

export function DatabaseProtection({ databases }: { databases: DatabaseDump[] }) {
  if (databases.length === 0) return null;

  return (
    <div className="space-y-2">
      <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
        Databases in this snapshot
      </p>
      <ul className="space-y-2">
        {databases.map((database) => (
          <DatabaseRow key={`${database.service}-${database.technology}`} database={database} />
        ))}
      </ul>
    </div>
  );
}

function DatabaseRow({ database }: { database: DatabaseDump }) {
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

      {/* The command turns a file in a snapshot into a restore someone can
          actually perform. Shown rather than run: replaying a dump replaces a
          live database, which needs the same deliberate confirmation as any
          other destructive action. */}
      {database.replay && <ReplayCommand command={database.replay} />}
    </li>
  );
}

function ReplayCommand({ command }: { command: string }) {
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
    <div className="mt-2 space-y-1">
      <p className="text-xs text-muted-foreground">To put this database back:</p>
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
