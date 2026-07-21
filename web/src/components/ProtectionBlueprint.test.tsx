import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProtectionBlueprint } from "@/components/ProtectionBlueprint";
import { ApiError, api } from "@/lib/api";

describe("ProtectionBlueprint", () => {
  afterEach(() => vi.restoreAllMocks());

  it("explains analysis and offers an explicit first scan", async () => {
    vi.spyOn(api, "getProjectBlueprint").mockRejectedValue(new ApiError(404, "not analyzed"));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><ProtectionBlueprint projectId="project-1" /></QueryClientProvider>);

    expect(await screen.findByRole("heading", { name: "Understand this project before protecting it" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Analyze project" })).toBeEnabled();
    expect(screen.getByText(/Secret values are never read/)).toBeInTheDocument();
  });
});
