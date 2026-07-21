import type { LucideIcon } from "lucide-react";
import {
  Activity,
  FolderPlus,
  FolderSearch,
  FolderX,
  HardDrive,
  Lock,
  LockOpen,
  LogIn,
  LogOut,
  PencilLine,
  ShieldAlert,
  ShieldCheck,
  CircleSlash,
  Play,
  XCircle,
} from "lucide-react";
import type { AuditEvent } from "@/lib/api";

export type EventTone = "neutral" | "success" | "warning" | "danger";

interface EventPresentation {
  /** Human-readable label shown in the UI instead of the raw action id. */
  label: string;
  icon: LucideIcon;
  tone: EventTone;
}

// The API emits stable machine-readable action ids (e.g. "auth.login_failed")
// so the audit trail stays parseable across versions. Those ids are for the
// log, not for people — this map is the single place that turns them into
// language a human reads, and it keeps Overview and Activity consistent.
const PRESENTATION: Record<string, EventPresentation> = {
  "auth.admin_account_created": {
    label: "Administrator account created",
    icon: ShieldCheck,
    tone: "success",
  },
  "auth.login_succeeded": { label: "Signed in", icon: LogIn, tone: "neutral" },
  "auth.login_failed": { label: "Failed sign-in attempt", icon: ShieldAlert, tone: "warning" },
  "auth.logout": { label: "Signed out", icon: LogOut, tone: "neutral" },

  "project.registered": { label: "Project registered", icon: FolderPlus, tone: "success" },
  "project.updated": { label: "Project updated", icon: PencilLine, tone: "neutral" },
  "project.removed": { label: "Project removed", icon: FolderX, tone: "warning" },
  "project.scanned": { label: "Scanned for projects", icon: FolderSearch, tone: "neutral" },

  // An unlock is the moment every stored credential becomes readable, so it
  // is worth spotting in the feed rather than blending in.
  "secrets.store_initialized": {
    label: "Secret store set up",
    icon: ShieldCheck,
    tone: "success",
  },
  "secrets.store_unlocked": { label: "Secret store unlocked", icon: LockOpen, tone: "warning" },
  "secrets.store_locked": { label: "Secret store locked", icon: Lock, tone: "neutral" },

  "repository.created": { label: "Repository added", icon: HardDrive, tone: "success" },
  "repository.deleted": { label: "Repository removed", icon: HardDrive, tone: "warning" },
  "repository.initialized": { label: "Repository initialised", icon: HardDrive, tone: "success" },
  "repository.checked": { label: "Repository checked", icon: HardDrive, tone: "neutral" },

  "backup.started": { label: "Backup started", icon: Play, tone: "neutral" },
  // Success is claimed only for a backup whose snapshot was read back, so the
  // wording says that rather than the weaker "completed".
  "backup.completed": { label: "Backup verified", icon: ShieldCheck, tone: "success" },
  "backup.failed": { label: "Backup failed", icon: XCircle, tone: "danger" },
  "backup.cancelled": { label: "Backup cancelled", icon: CircleSlash, tone: "neutral" },
};

/**
 * describeEvent turns a raw audit event into something presentable. Unknown
 * actions (e.g. emitted by a newer server than this frontend) degrade to a
 * readable form of the id rather than being hidden or crashing.
 */
export function describeEvent(event: AuditEvent): EventPresentation {
  const known = PRESENTATION[event.action];
  if (known) {
    return known;
  }
  return { label: humanizeActionId(event.action), icon: Activity, tone: "neutral" };
}

function humanizeActionId(action: string): string {
  const withoutNamespace = action.includes(".") ? action.slice(action.indexOf(".") + 1) : action;
  const words = withoutNamespace.replace(/[._-]+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * eventDetail returns a short, human-meaningful qualifier for an event, or
 * undefined when there is nothing worth showing. Opaque identifiers (UUIDs)
 * are deliberately never surfaced: they mean nothing to the reader and only
 * add noise to the feed.
 */
export function eventDetail(event: AuditEvent): string | undefined {
  const metadata = event.metadata ?? {};

  const name = metadata.name ?? metadata.username;
  if (typeof name === "string" && name.length > 0) {
    return name;
  }

  if (typeof metadata.discoveredCount === "number") {
    const count = metadata.discoveredCount;
    return `${count} ${count === 1 ? "project" : "projects"} found`;
  }

  return undefined;
}

export const TONE_CLASSES: Record<EventTone, string> = {
  neutral: "text-muted-foreground",
  success: "text-success",
  warning: "text-warning",
  // A failed backup is not a caution — it means the protection someone
  // believes they have does not exist.
  danger: "text-destructive",
};
