import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
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

// Mutable so each test can put the app in the state it is about, rather than
// every test sharing one fixed situation.
let authState: {
  user: { id: string; username: string } | undefined;
  isLoading: boolean;
  setupComplete: boolean | undefined;
};

vi.mock("@/lib/auth-context", () => ({
  useAuth: () => ({ ...authState, refresh: vi.fn() }),
}));

beforeEach(() => {
  authState = { user: undefined, isLoading: false, setupComplete: true };
});

function renderLoginPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/login"]}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/setup" element={<p>Create the administrator account</p>} />
          <Route path="/" element={<p>Signed in</p>} />
        </Routes>
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

  // A fresh install reached through a remembered /login URL — after wiping the
  // data volume, or simply because logging out navigates here and the browser
  // restored the tab. Showing a sign-in form asks for a password that cannot
  // exist yet, and leaves no way to reach initial setup.
  it("sends you to setup when no administrator account exists", () => {
    authState = { user: undefined, isLoading: false, setupComplete: false };

    renderLoginPage();

    expect(screen.getByText(/create the administrator account/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /sign in/i })).not.toBeInTheDocument();
  });

  // Deciding before the setup status is known would flash the sign-in form at
  // someone on their way to setup.
  it("decides nothing while the setup status is still loading", () => {
    authState = { user: undefined, isLoading: true, setupComplete: undefined };

    renderLoginPage();

    expect(screen.queryByRole("button", { name: /sign in/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/create the administrator account/i)).not.toBeInTheDocument();
  });
});
