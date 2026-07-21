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

  it("shows the matched application template and recovery contract", async () => {
    vi.spyOn(api, "getProjectBlueprint").mockResolvedValue({
      schemaVersion: 1,
      projectId: "project-1",
      fingerprint: "fingerprint",
      analyzedAt: "2026-07-21T18:00:00Z",
      drifted: false,
      findings: [],
      steps: [],
      warnings: [],
      templateMatches: [{
        templateId: "wordpress-mariadb",
        name: "WordPress with MariaDB",
        version: "1.0.0",
        category: "content",
        score: 100,
        matched: ["required image role: wordpress"],
        missing: [],
        plan: {
          classification: "stateful-mixed",
          consistency: "application-consistent",
          requiredData: ["WordPress content", "MariaDB logical dump"],
          restoreChecks: ["HTTP responds"],
        },
      }],
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><ProtectionBlueprint projectId="project-1" /></QueryClientProvider>);

    expect(await screen.findByText("Known application blueprint")).toBeInTheDocument();
    expect(screen.getByText(/WordPress with MariaDB/)).toBeInTheDocument();
    expect(screen.getByText("MariaDB logical dump")).toBeInTheDocument();
    expect(screen.getByText("100% match")).toBeInTheDocument();
  });
});
