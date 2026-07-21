import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  FolderCheck,
  HardDrive,
  Loader2,
  Plus,
  RefreshCw,
  Trash2,
  Play,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { Timestamp } from "@/components/Timestamp";
import { SecretStoreGate } from "@/components/SecretStoreGate";
import {
  api,
  ApiError,
  type Repository,
  type RepositoryLocation,
  type RepositoryStatus,
} from "@/lib/api";
import { usePageTitle } from "@/hooks/usePageTitle";
import { cn } from "@/lib/utils";

const createSchema = z.object({
  name: z.string().min(1, "Required").max(128),
  location: z
    .string()
    .min(1, "Required")
    .refine((v) => v.startsWith("/"), { message: "Must be an absolute path (e.g. /srv/backups)" }),
  password: z.string().min(1, "Required"),
});
type CreateValues = z.infer<typeof createSchema>;

const STATUS_LABELS: Record<RepositoryStatus, string> = {
  unknown: "Not checked",
  uninitialized: "Needs initialising",
  ready: "Ready",
  error: "Unreachable",
};

const STATUS_STYLES: Record<RepositoryStatus, string> = {
  unknown: "bg-muted text-muted-foreground border-border",
  uninitialized: "bg-warning/15 text-warning border-warning/30",
  ready: "bg-success/15 text-success border-success/30",
  error: "bg-destructive/15 text-destructive border-destructive/30",
};

export function RepositoriesPage() {
  usePageTitle("Repositories");

  return (
    <div className="space-y-6">
      <PageHeader
        title="Repositories"
        description="Where snapshots are written. Passwords are held in the encrypted secret store, never with the repository."
      />
      <SecretStoreGate>
        <RepositoryList />
      </SecretStoreGate>
    </div>
  );
}

/**
 * Turns a repository name into a directory-safe segment, so the suggested path
 * stays predictable no matter what the name contains.
 */
function toPathSegment(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
}

/**
 * Explains where a repository can go. Inside a container there is no way to
 * discover this by looking, so the server probes it and the answer is shown
 * here rather than left to trial and error.
 */
function LocationHint({
  location,
  loading,
}: {
  location?: RepositoryLocation;
  loading: boolean;
}) {
  if (loading) {
    return <Skeleton className="h-4 w-2/3" />;
  }

  if (!location) {
    return (
      <p className="text-xs text-muted-foreground">
        No backup directory is mounted into this container. Mount a writable directory and point
        this path inside it — backups must not live in Back-Orbit's own data volume, or one
        failure would take both.
      </p>
    );
  }

  if (!location.writable) {
    return (
      <div className="flex gap-2 rounded-md border border-warning/30 bg-warning/15 p-2.5 text-xs text-warning">
        <AlertTriangle className="mt-px size-3.5 shrink-0" />
        <p>{location.detail ?? `${location.path} is not writable by Back-Orbit.`}</p>
      </div>
    );
  }

  return (
    <div className="flex gap-2 text-xs text-muted-foreground">
      <FolderCheck className="mt-px size-3.5 shrink-0 text-success" />
      <p>
        <span className="font-mono text-foreground">{location.path}</span> is ready to use.{" "}
        {location.description} Paths are inside the container, not on your desktop.
      </p>
    </div>
  );
}

function RepositoryList() {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);

  const query = useQuery({ queryKey: ["repositories"], queryFn: api.listRepositories });
  const locationsQuery = useQuery({
    queryKey: ["repository-locations"],
    queryFn: api.repositoryLocations,
  });
  const suggested = locationsQuery.data?.find((l) => l.recommended) ?? locationsQuery.data?.[0];

  const form = useForm<CreateValues>({ resolver: zodResolver(createSchema) });

  // The directory follows the name until the operator edits it themselves,
  // after which it is left alone. Most people never need to think about the
  // path at all; anyone who does keeps full control of it.
  const [locationEdited, setLocationEdited] = useState(false);
  const name = form.watch("name");

  useEffect(() => {
    if (locationEdited || !suggested?.writable) return;
    const segment = toPathSegment(name ?? "");
    form.setValue("location", segment ? `${suggested.path}/${segment}` : "");
  }, [name, suggested, locationEdited, form]);

  // A rejected repository is explained in the dialog rather than in a toast.
  // The reason names a path and what to use instead, which is more than can be
  // read before a toast disappears — and it belongs next to the field it is
  // about, while that field is still on screen.
  const [submitError, setSubmitError] = useState<string | null>(null);

  const createMutation = useMutation({
    mutationFn: (values: CreateValues) =>
      api.createRepository({ ...values, kind: "local" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["repositories"] });
      toast.success("Repository added. Check it to see whether it needs initialising.");
      setDialogOpen(false);
      form.reset();
      setLocationEdited(false);
      setSubmitError(null);
    },
    onError: (error) =>
      setSubmitError(
        error instanceof ApiError ? error.message : "Could not add the repository.",
      ),
  });

  return (
    <>
      <div className="flex justify-end">
        <Dialog
          open={dialogOpen}
          onOpenChange={(open) => {
            setDialogOpen(open);
            if (!open) setSubmitError(null);
          }}
        >
          <DialogTrigger render={<Button />}>
            <Plus className="size-4" />
            Add repository
          </DialogTrigger>
          <DialogContent>
            <form
              onSubmit={form.handleSubmit((values) => {
                setSubmitError(null);
                createMutation.mutate(values);
              })}
              noValidate
            >
              <DialogHeader>
                <DialogTitle>Add a local repository</DialogTitle>
                <DialogDescription>
                  A directory on this host that snapshots are written to. Remote destinations
                  (SFTP, S3) arrive in a later phase.
                </DialogDescription>
              </DialogHeader>

              <div className="space-y-4 py-4">
                {submitError && (
                  <div
                    role="alert"
                    className="flex gap-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
                  >
                    <AlertTriangle className="mt-0.5 size-4 shrink-0" />
                    <p>{submitError}</p>
                  </div>
                )}
                <div className="space-y-2">
                  <Label htmlFor="repo-name">Name</Label>
                  <Input id="repo-name" {...form.register("name")} />
                  {form.formState.errors.name && (
                    <p className="text-sm text-destructive">{form.formState.errors.name.message}</p>
                  )}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="repo-location">Directory</Label>
                  <Input
                    id="repo-location"
                    placeholder={suggested ? `${suggested.path}/main` : "/backups/main"}
                    {...form.register("location", {
                      onChange: () => setLocationEdited(true),
                    })}
                  />
                  {form.formState.errors.location && (
                    <p className="text-sm text-destructive">
                      {form.formState.errors.location.message}
                    </p>
                  )}
                  <LocationHint location={suggested} loading={locationsQuery.isLoading} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="repo-password">Repository password</Label>
                  <Input
                    id="repo-password"
                    type="password"
                    autoComplete="new-password"
                    {...form.register("password")}
                  />
                  {form.formState.errors.password && (
                    <p className="text-sm text-destructive">
                      {form.formState.errors.password.message}
                    </p>
                  )}
                  <p className="text-xs text-muted-foreground">
                    This encrypts the repository itself. Losing it means losing every snapshot in
                    it — there is no recovery path, by design.
                  </p>
                </div>
              </div>

              <DialogFooter>
                <Button type="submit" disabled={createMutation.isPending}>
                  {createMutation.isPending ? "Adding…" : "Add repository"}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      {query.isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      ) : !query.data || query.data.length === 0 ? (
        <Card>
          <EmptyState
            icon={HardDrive}
            title="No repositories yet"
            description="A repository is the destination snapshots are written to. Add one to give backups somewhere to go."
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {query.data.map((repo) => (
            <RepositoryCard key={repo.id} repository={repo} />
          ))}
        </div>
      )}
    </>
  );
}

/**
 * Confirms removing a repository, and lets the operator opt into erasing the
 * snapshots with it.
 *
 * Two separate decisions are deliberately kept separate. Removing a
 * configuration is routine and reversible — the repository can be added back.
 * Erasing the snapshots is neither, and it is the one action that can leave
 * someone with no copy of their data at all. Making it the default, or hiding
 * it behind the same click, would put the worst outcome one stray press away.
 *
 * Typing the name is the deliberate friction: it cannot be satisfied by
 * muscle memory the way a second "are you sure" can.
 */
function RemoveRepositoryDialog({
  repository,
  open,
  onOpenChange,
  onConfirm,
  pending,
}: {
  repository: Repository;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (deleteData: boolean) => void;
  pending: boolean;
}) {
  const [typedName, setTypedName] = useState("");
  const [deleteData, setDeleteData] = useState(false);

  // Reopening always starts from the safe state: a checkbox left ticked from
  // a previous, abandoned attempt would be the exact accident this guards.
  useEffect(() => {
    if (open) {
      setTypedName("");
      setDeleteData(false);
    }
  }, [open]);

  const nameMatches = typedName === repository.name;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Remove “{repository.name}”?</DialogTitle>
          <DialogDescription>
            Back-Orbit will forget this repository and delete its stored password.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          {deleteData ? (
            <div
              role="alert"
              className="flex gap-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              <div className="space-y-1">
                <p className="font-medium">Every snapshot in this repository will be destroyed.</p>
                <p>
                  All backups under{" "}
                  <span className="font-mono break-all">{repository.location}</span> are deleted
                  permanently. This cannot be undone, and nothing can restore them unless you
                  hold a copy elsewhere.
                </p>
              </div>
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">
              The snapshots at{" "}
              <span className="font-mono break-all text-foreground">{repository.location}</span>{" "}
              stay where they are. You can add this repository again later, or delete the
              directory yourself.
            </p>
          )}

          <label className="flex items-start gap-2.5 text-sm">
            <input
              type="checkbox"
              checked={deleteData}
              onChange={(event) => setDeleteData(event.target.checked)}
              className="mt-0.5 size-4 shrink-0 accent-destructive"
            />
            <span>Also delete every snapshot stored at this location</span>
          </label>

          <div className="space-y-2">
            <Label htmlFor="confirm-repo-name">
              Type <span className="font-mono font-semibold">{repository.name}</span> to confirm
            </Label>
            <Input
              id="confirm-repo-name"
              value={typedName}
              autoComplete="off"
              onChange={(event) => setTypedName(event.target.value)}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant={deleteData ? "destructive" : "default"}
            onClick={() => onConfirm(deleteData)}
            disabled={!nameMatches || pending}
            aria-busy={pending}
          >
            {pending && <Loader2 className="size-4 animate-spin" />}
            {deleteData ? "Delete repository and all backups" : "Remove repository"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RepositoryCard({ repository }: { repository: Repository }) {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["repositories"] });

  const checkMutation = useMutation({
    mutationFn: () => api.checkRepository(repository.id),
    onSuccess: (result) => {
      invalidate();
      if (result.status === "ready") {
        toast.success(
          `${repository.name} is ready — ${result.snapshotCount} snapshot${result.snapshotCount === 1 ? "" : "s"}.`,
        );
      } else if (result.status === "uninitialized") {
        toast.warning(`${repository.name} has no repository yet. Initialise it to start using it.`);
      } else {
        toast.error(`${repository.name} could not be reached.`);
      }
    },
    onError: (error) =>
      toast.error(error instanceof ApiError ? error.message : "Check failed."),
  });

  const initializeMutation = useMutation({
    mutationFn: () => api.initializeRepository(repository.id),
    onSuccess: () => {
      invalidate();
      toast.success(`${repository.name} initialised.`);
    },
    onError: (error) =>
      toast.error(error instanceof ApiError ? error.message : "Could not initialise."),
  });

  const [removeOpen, setRemoveOpen] = useState(false);

  const deleteMutation = useMutation({
    mutationFn: (deleteData: boolean) => api.deleteRepository(repository.id, deleteData),
    onSuccess: (_result, deleteData) => {
      invalidate();
      setRemoveOpen(false);
      toast.success(
        deleteData
          ? `${repository.name} and every snapshot at ${repository.location} were deleted.`
          : `${repository.name} removed. The snapshots at ${repository.location} were left untouched.`,
      );
    },
    // The dialog stays open on failure. A refused deletion has a reason worth
    // reading — most often that the location holds something that is not a
    // restic repository — and closing would throw it away.
    onError: (error) =>
      toast.error(error instanceof ApiError ? error.message : "Could not remove."),
  });

  const busy = checkMutation.isPending || initializeMutation.isPending || deleteMutation.isPending;

  return (
    <Card>
      <CardContent className="flex flex-wrap items-start justify-between gap-4 p-4">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{repository.name}</span>
            <Badge variant="outline" className={cn("font-medium", STATUS_STYLES[repository.status])}>
              {STATUS_LABELS[repository.status]}
            </Badge>
            <Badge variant="outline" className="font-normal text-muted-foreground">
              {repository.kind}
            </Badge>
          </div>
          <p className="truncate font-mono text-xs text-muted-foreground" title={repository.location}>
            {repository.location}
          </p>
          {repository.lastCheckedAt && (
            <p className="text-xs text-muted-foreground">
              Checked <Timestamp iso={repository.lastCheckedAt} />
            </p>
          )}
          {repository.status === "error" && repository.lastError && (
            <p className="max-w-xl text-xs text-destructive">{repository.lastError}</p>
          )}
        </div>

        {/* Each action shows its own progress. Every button is disabled while
            any one of them runs, so without a per-button signal the whole row
            greys out with no indication of which action is under way — and
            initialising a fresh repository takes long enough to look stuck. */}
        <div className="flex shrink-0 flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => checkMutation.mutate()}
            disabled={busy}
            aria-busy={checkMutation.isPending}
          >
            <RefreshCw className={cn("size-4", checkMutation.isPending && "animate-spin")} />
            Check
          </Button>
          {repository.status !== "ready" && (
            <Button
              size="sm"
              onClick={() => initializeMutation.mutate()}
              disabled={busy}
              aria-busy={initializeMutation.isPending}
            >
              {initializeMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Play className="size-4" />
              )}
              Initialise
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setRemoveOpen(true)}
            disabled={busy}
            aria-busy={deleteMutation.isPending}
            aria-label={`Remove ${repository.name}`}
          >
            {deleteMutation.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Trash2 className="size-4" />
            )}
          </Button>

          <RemoveRepositoryDialog
            repository={repository}
            open={removeOpen}
            onOpenChange={setRemoveOpen}
            onConfirm={(deleteData) => deleteMutation.mutate(deleteData)}
            pending={deleteMutation.isPending}
          />
        </div>
      </CardContent>
    </Card>
  );
}
