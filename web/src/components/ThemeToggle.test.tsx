import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ThemeToggle } from "@/components/ThemeToggle";

const setTheme = vi.fn();

vi.mock("next-themes", () => ({
  useTheme: () => ({ theme: "system", resolvedTheme: "light", setTheme }),
}));

/**
 * Guards a bug that TypeScript cannot catch: the menu items previously used
 * Radix's `onSelect`, which Base UI ignores. Because `onSelect` is also a real
 * DOM event (text selection), the compiler accepted it and the theme switcher
 * silently did nothing when clicked. This test drives the real interaction.
 */
describe("ThemeToggle", () => {
  it("applies the chosen theme when a menu item is clicked", async () => {
    const user = userEvent.setup();
    render(<ThemeToggle />);

    await user.click(screen.getByRole("button", { name: /change theme/i }));
    await user.click(await screen.findByRole("menuitem", { name: /dark/i }));

    expect(setTheme).toHaveBeenCalledWith("dark");
  });
});
