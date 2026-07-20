import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { LoginPage } from "@/pages/LoginPage";
import { api } from "@/lib/api";

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    api: { ...actual.api, login: vi.fn() },
  };
});

vi.mock("@/lib/auth-context", () => ({
  useAuth: () => ({
    user: undefined,
    isLoading: false,
    setupComplete: true,
    refresh: vi.fn(),
  }),
}));

function renderLoginPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/login"]}>
        <LoginPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("LoginPage", () => {
  it("shows validation errors when submitted empty", async () => {
    const user = userEvent.setup();
    renderLoginPage();

    await user.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findAllByText("Required")).toHaveLength(2);
    expect(api.login).not.toHaveBeenCalled();
  });

  it("submits the entered credentials", async () => {
    const user = userEvent.setup();
    vi.mocked(api.login).mockResolvedValue({
      id: "1",
      username: "admin",
      sessionExpiresAt: new Date().toISOString(),
    });

    renderLoginPage();

    await user.type(screen.getByLabelText(/username/i), "admin");
    await user.type(screen.getByLabelText(/password/i), "correct-horse-battery-staple");
    await user.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => {
      expect(api.login).toHaveBeenCalledWith("admin", "correct-horse-battery-staple");
    });
  });
});
