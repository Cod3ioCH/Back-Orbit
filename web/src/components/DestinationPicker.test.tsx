import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { DestinationPicker } from "@/components/DestinationPicker";
import type { Repository } from "@/lib/api";

function repository(id: string, name: string): Repository {
  return {
    id,
    name,
    kind: "local",
    location: `/backups/${name}`,
    status: "ready",
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  };
}

const main = repository("ec9df4e1-1ed7-4309-98c1-a3cfd5eacb7d", "main-backup");
const nas = repository("7b21c0aa-0d5f-4c11-9a44-2f0c9d1e33ab", "nas-offsite");

describe("DestinationPicker", () => {
  // The bug this guards: the select carries repository *ids*, and rendering
  // the value unresolved put a raw UUID in front of someone choosing where
  // their backups go.
  it("shows the repository name, never its id", async () => {
    render(<DestinationPicker repositories={[main, nas]} value={main.id} onChange={vi.fn()} />);

    const trigger = screen.getByRole("combobox", { name: /backup destination/i });

    expect(trigger).toHaveTextContent("main-backup");
    expect(trigger.textContent).not.toContain(main.id);
  });

  // A dropdown holding one option asks a question with only one answer.
  it("states the destination instead of offering a choice when there is only one", () => {
    render(<DestinationPicker repositories={[main]} value={main.id} onChange={vi.fn()} />);

    expect(screen.getByText("main-backup")).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("renders nothing when no repository is ready", () => {
    const { container } = render(
      <DestinationPicker repositories={[]} value="" onChange={vi.fn()} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("reports the chosen repository by id", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();

    render(<DestinationPicker repositories={[main, nas]} value={main.id} onChange={onChange} />);

    await user.click(screen.getByRole("combobox", { name: /backup destination/i }));
    await user.click(await screen.findByRole("option", { name: "nas-offsite" }));

    expect(onChange).toHaveBeenCalledWith(nas.id);
  });
});
