import { NavLink } from "react-router-dom";
import { X, OrbitIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { navItems } from "@/components/layout/nav-items";
import { Button } from "@/components/ui/button";
import { DockerStatusIndicator } from "@/components/DockerStatus";

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
        <div className="flex h-14 shrink-0 items-center justify-between border-b px-4">
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
                    : item.available
                      ? "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                      : // Placeholder sections stay reachable but visibly quieter,
                        // so the menu communicates what is ready today.
                        "text-muted-foreground/60 hover:bg-accent/50 hover:text-accent-foreground",
                )
              }
            >
              {({ isActive }) => (
                <>
                  <item.icon className="size-4 shrink-0" aria-hidden="true" />
                  <span className="flex-1 truncate">{item.label}</span>
                  {!item.available && (
                    <span
                      className={cn(
                        "rounded-full px-1.5 py-0.5 text-[10px] font-medium tracking-wide uppercase",
                        isActive
                          ? "bg-primary-foreground/20 text-primary-foreground"
                          : "bg-muted text-muted-foreground/70",
                      )}
                    >
                      Soon
                    </span>
                  )}
                </>
              )}
            </NavLink>
          ))}
        </nav>

        <div className="shrink-0 border-t p-2">
          <DockerStatusIndicator />
        </div>
      </aside>
    </>
  );
}
