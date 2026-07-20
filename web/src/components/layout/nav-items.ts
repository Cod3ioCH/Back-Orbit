import type { LucideIcon } from "lucide-react";
import {
  LayoutDashboard,
  FolderKanban,
  CalendarClock,
  Camera,
  History,
  HardDrive,
  Activity,
  KeyRound,
  Bell,
  Settings,
} from "lucide-react";

export interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  end?: boolean;
}

export const navItems: NavItem[] = [
  { to: "/", label: "Overview", icon: LayoutDashboard, end: true },
  { to: "/projects", label: "Projects", icon: FolderKanban },
  { to: "/plans", label: "Backup Plans", icon: CalendarClock },
  { to: "/snapshots", label: "Snapshots", icon: Camera },
  { to: "/restore", label: "Restore", icon: History },
  { to: "/repositories", label: "Repositories", icon: HardDrive },
  { to: "/activity", label: "Activity", icon: Activity },
  { to: "/secrets", label: "Secrets", icon: KeyRound },
  { to: "/alerts", label: "Alerts", icon: Bell },
  { to: "/settings", label: "Settings", icon: Settings },
];
