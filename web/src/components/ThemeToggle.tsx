import { useEffect, useState } from "react";
import { useTheme } from "next-themes";
import { Monitor, Moon, Sun } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const OPTIONS = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
] as const;

export function ThemeToggle() {
  const { theme, resolvedTheme, setTheme } = useTheme();

  // The resolved theme is only known once mounted on the client; rendering the
  // icon before that would flash the wrong one.
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  const ActiveIcon = !mounted ? Monitor : resolvedTheme === "dark" ? Moon : Sun;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon" aria-label="Change theme" />}
      >
        <ActiveIcon className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>
          {OPTIONS.map((option) => (
            <DropdownMenuItem
              key={option.value}
              // Base UI's Menu.Item uses onClick; onSelect is a different
              // (text-selection) DOM event and would silently never fire.
              onClick={() => setTheme(option.value)}
              className={theme === option.value ? "bg-accent" : undefined}
            >
              <option.icon className="size-4" aria-hidden="true" />
              {option.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
