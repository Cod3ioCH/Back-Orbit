import type { LucideIcon } from "lucide-react";
import {
  FolderPlus,
  FolderSearch,
  FolderX,
  LogIn,
  LogOut,
  PencilLine,
  ShieldAlert,
  ShieldCheck,
  Activity,
} from "lucide-react";
import type { AuditEvent } from "@/lib/api";

export type EventTone = "neutral" | "success" | "warning";

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
};
