import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { HardDrive, Plus, RefreshCw, Trash2, Play } from "lucide-react";
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
import { api, ApiError, type Repository, type RepositoryStatus } from "@/lib/api";
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

function RepositoryList() {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);

  const query = useQuery({ queryKey: ["repositories"], queryFn: api.listRepositories });

  const form = useForm<CreateValues>({ resolver: zodResolver(createSchema) });
  const createMutation = useMutation({
    mutationFn: (values: CreateValues) =>
      api.createRepository({ ...values, kind: "local" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["repositories"] });
      toast.success("Repository added. Check it to see whether it needs initialising.");
      setDialogOpen(false);
      form.reset();
    },
    onError: (error) =>
      toast.error(error instanceof ApiError ? error.message : "Could not add the repository."),
  });

  return (
    <>
      <div className="flex justify-end">
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger render={<Button />}>
            <Plus className="size-4" />
            Add repository
          </DialogTrigger>
          <DialogContent>
            <form
              onSubmit={form.handleSubmit((values) => createMutation.mutate(values))}
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
                <div className="space-y-2">
                  <Label htmlFor="repo-name">Name</Label>
                  <Input id="repo-name" {...form.register("name")} />
                  {form.formState.errors.name && (
                    <p className="text-sm text-destructive">{form.formState.errors.name.message}</p>
                  )}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="repo-location">Directory</Label>
                  <Input id="repo-location" placeholder="/srv/backups/main" {...form.register("location")} />
                  {form.formState.errors.location && (
                    <p className="text-sm text-destructive">
                      {form.formState.errors.location.message}
                    </p>
                  )}
                  <p className="text-xs text-muted-foreground">
                    Must be a path Back-Orbit itself can write to — inside the container, not on
                    your desktop.
                  </p>
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

  const deleteMutation = useMutation({
    mutationFn: () => api.deleteRepository(repository.id),
    onSuccess: () => {
      invalidate();
      toast.success(`${repository.name} removed. The data at the destination was left untouched.`);
    },
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

        <div className="flex shrink-0 flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => checkMutation.mutate()}
            disabled={busy}
          >
            <RefreshCw className={checkMutation.isPending ? "size-4 animate-spin" : "size-4"} />
            Check
          </Button>
          {repository.status !== "ready" && (
            <Button size="sm" onClick={() => initializeMutation.mutate()} disabled={busy}>
              <Play className="size-4" />
              Initialise
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => deleteMutation.mutate()}
            disabled={busy}
            aria-label={`Remove ${repository.name}`}
          >
            <Trash2 className="size-4" />
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
