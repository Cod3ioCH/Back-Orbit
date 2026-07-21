import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { Repository } from "@/lib/api";

/**
 * Chooses which repository a backup is written to.
 *
 * With a single destination there is no decision to make, so no control is
 * shown — just where the backup goes. A dropdown holding one option is a
 * widget that asks a question with only one answer.
 *
 * The trigger renders the repository's *name*. The value it carries is an id,
 * and letting the select render that unresolved would put a UUID in front of
 * someone deciding where their data goes.
 */
export function DestinationPicker({
  repositories,
  value,
  onChange,
}: {
  repositories: Repository[];
  value: string;
  onChange: (id: string) => void;
}) {
  if (repositories.length === 0) {
    return null;
  }

  if (repositories.length === 1) {
    return (
      <span className="text-sm text-muted-foreground">
        to <span className="text-foreground">{repositories[0].name}</span>
      </span>
    );
  }

  return (
    <Select<string>
      value={value}
      // Base UI reports a cleared selection as null. Falling back to "" keeps
      // the caller's state on its default rather than leaving the control with
      // no destination at all.
      onValueChange={(next) => onChange(next ?? "")}
    >
      <SelectTrigger aria-label="Backup destination">
        {/* The value is a repository id. Without this the trigger renders it
            raw, putting a UUID in front of someone choosing where their data
            goes. Same idiom as the snapshot and restore selectors. */}
        <SelectValue>
          {(id) =>
            repositories.find((repo) => repo.id === id)?.name ?? "Choose a destination"
          }
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {repositories.map((repo) => (
          <SelectItem key={repo.id} value={repo.id}>
            {repo.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
