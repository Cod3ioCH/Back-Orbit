import { Menu, LogOut, User as UserIcon } from "lucide-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { ThemeToggle } from "@/components/ThemeToggle";

interface TopBarProps {
  onMenuClick: () => void;
}

export function TopBar({ onMenuClick }: TopBarProps) {
  const { user } = useAuth();
  const queryClient = useQueryClient();

  const logoutMutation = useMutation({
    mutationFn: api.logout,
    // onSettled, not onSuccess: signing out is the user's intent, so the app
    // must end up signed out either way. A failing logout call is usually a
    // session that already expired (401) — precisely when leaving the user
    // stranded in a signed-in-looking UI, with no feedback, is worst.
    onSettled: () => {
      queryClient.clear();
      // Then reload rather than relying on cache invalidation. Clearing the
      // cache does not make already-mounted queries refetch, so the app kept
      // rendering the signed-in user from observer state after the session
      // was gone. A full navigation guarantees no in-memory data from the
      // previous session survives — the property that actually matters when
      // ending a session.
      window.location.assign("/login");
    },
  });

  return (
    <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b bg-background/95 px-4 backdrop-blur">
      <Button
        variant="ghost"
        size="icon"
        className="md:hidden"
        onClick={onMenuClick}
        aria-label="Open navigation"
      >
        <Menu className="size-4" />
      </Button>
      <div className="hidden md:block" />

      <div className="flex items-center gap-1">
        <ThemeToggle />

        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="ghost" size="sm" className="gap-2" />}>
            <UserIcon className="size-4" aria-hidden="true" />
            {user?.username}
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{user?.username}</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {/* Base UI's Menu.Item fires onClick, not Radix's onSelect —
                  and because `onSelect` is a real DOM event for text
                  selection, TypeScript accepts it silently while the handler
                  never runs. */}
              <DropdownMenuItem
                onClick={() => logoutMutation.mutate()}
                disabled={logoutMutation.isPending}
              >
                <LogOut className="size-4" aria-hidden="true" />
                Log out
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
