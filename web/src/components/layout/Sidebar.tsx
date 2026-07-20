import { NavLink } from "react-router-dom";
import { X, OrbitIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { navItems } from "@/components/layout/nav-items";
import { Button } from "@/components/ui/button";

interface SidebarProps {
  open: boolean;
  onClose: () => void;
}

export function Sidebar({ open, onClose }: SidebarProps) {
  return (
    <>
      {open && (
        <div
          className="fixed inset-0 z-40 bg-black/40 md:hidden"
          onClick={onClose}
          aria-hidden="true"
        />
      )}
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-50 flex w-64 flex-col border-r bg-card transition-transform md:sticky md:top-0 md:h-screen md:translate-x-0",
          open ? "translate-x-0" : "-translate-x-full",
        )}
      >
        <div className="flex h-14 items-center justify-between border-b px-4">
          <div className="flex items-center gap-2 font-semibold">
            <OrbitIcon className="size-5 text-primary" aria-hidden="true" />
            <span>Back-Orbit</span>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            onClick={onClose}
            aria-label="Close navigation"
          >
            <X className="size-4" />
          </Button>
        </div>

        <nav className="flex-1 space-y-1 overflow-y-auto p-3" aria-label="Main navigation">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              onClick={onClose}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                )
              }
            >
              <item.icon className="size-4 shrink-0" aria-hidden="true" />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>
    </>
  );
}
