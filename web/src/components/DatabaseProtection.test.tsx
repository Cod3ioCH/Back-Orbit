import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { DatabaseProtection } from "@/components/DatabaseProtection";
import type { DatabaseDump } from "@/lib/api";

const exported: DatabaseDump = {
  technology: "postgresql",
  service: "db",
  level: "exported",
  path: "back-orbit-dumps/db-postgresql.sql",
  user: "app",
  bytes: 7756,
  replay: "docker compose exec -T db psql -U app < back-orbit-dumps/db-postgresql.sql",
};

const filesOnly: DatabaseDump = {
  technology: "mongodb",
  service: "mongo",
  level: "files_only",
  bytes: 0,
  note: "Back-Orbit does not export this engine yet, so only its data directory was copied.",
};

describe("DatabaseProtection", () => {
  it("shows that a database was exported, and how to put it back", () => {
    render(<DatabaseProtection databases={[exported]} />);

    expect(screen.getByText("PostgreSQL")).toBeInTheDocument();
    expect(screen.getByText("Exported")).toBeInTheDocument();
    // The command is the difference between a file in a snapshot and a restore
    // someone can perform.
    expect(screen.getByText(exported.replay!)).toBeInTheDocument();
  });

  // A snapshot that shows only its successes leaves the gaps to be discovered
  // at restore time, which is the worst moment to find them.
  it("does not hide a database that was only copied", () => {
    render(<DatabaseProtection databases={[exported, filesOnly]} />);

    expect(screen.getByText("MongoDB")).toBeInTheDocument();
    expect(screen.getByText("Files only")).toBeInTheDocument();
    expect(screen.getByText(filesOnly.note!)).toBeInTheDocument();
  });

  // Offering a command for a file-level copy would imply a guarantee that does
  // not exist.
  it("offers no command where there is nothing to replay", () => {
    render(<DatabaseProtection databases={[filesOnly]} />);

    expect(screen.queryByText(/To put this database back/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /copy the restore command/i })).not.toBeInTheDocument();
  });

  it("renders nothing when the snapshot holds no databases", () => {
    const { container } = render(<DatabaseProtection databases={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
