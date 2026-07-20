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

interface TopBarProps {
  onMenuClick: () => void;
}

export function TopBar({ onMenuClick }: TopBarProps) {
  const { user } = useAuth();
  const queryClient = useQueryClient();

  const logoutMutation = useMutation({
    mutationFn: api.logout,
    onSuccess: () => {
      queryClient.setQueryData(["session"], undefined);
      queryClient.invalidateQueries();
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

      <DropdownMenu>
        <DropdownMenuTrigger render={<Button variant="ghost" size="sm" className="gap-2" />}>
          <UserIcon className="size-4" aria-hidden="true" />
          {user?.username}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuGroup>
            <DropdownMenuLabel>{user?.username}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onSelect={() => logoutMutation.mutate()}
              disabled={logoutMutation.isPending}
            >
              <LogOut className="size-4" aria-hidden="true" />
              Log out
            </DropdownMenuItem>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </header>
  );
}
