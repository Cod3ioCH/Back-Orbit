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
  /**
   * Whether this section actually does something yet. Sections that only show
   * a "coming soon" placeholder are marked so the navigation can say so up
   * front, instead of letting people discover it by clicking and being
   * disappointed — most of the menu is still placeholder in this phase.
   */
  available?: boolean;
}

export const navItems: NavItem[] = [
  { to: "/", label: "Overview", icon: LayoutDashboard, end: true, available: true },
  { to: "/projects", label: "Projects", icon: FolderKanban, available: true },
  { to: "/plans", label: "Backup Plans", icon: CalendarClock },
  { to: "/snapshots", label: "Snapshots", icon: Camera, available: true },
  { to: "/restore", label: "Restore", icon: History, available: true },
  { to: "/repositories", label: "Repositories", icon: HardDrive, available: true },
  { to: "/activity", label: "Activity", icon: Activity, available: true },
  { to: "/secrets", label: "Secrets", icon: KeyRound },
  { to: "/alerts", label: "Alerts", icon: Bell },
  { to: "/settings", label: "Settings", icon: Settings },
];
