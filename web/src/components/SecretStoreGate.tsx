import { useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Lock, ShieldAlert } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { api, ApiError } from "@/lib/api";

const setupSchema = z
  .object({
    passphrase: z.string().min(12, "At least 12 characters"),
    confirm: z.string(),
  })
  .refine((values) => values.passphrase === values.confirm, {
    message: "Passphrases do not match",
    path: ["confirm"],
  });

const unlockSchema = z.object({ passphrase: z.string().min(1, "Required") });

/**
 * SecretStoreGate renders its children only once the secret store can actually
 * be used, and otherwise shows what needs doing. Repository credentials live
 * in that store, so a page that manages repositories is meaningless while it
 * is locked — better to say so plainly than to show controls that fail.
 */
export function SecretStoreGate({ children }: { children: ReactNode }) {
  const query = useQuery({ queryKey: ["secret-store"], queryFn: api.secretStoreStatus });

  if (query.isLoading) {
    return <Skeleton className="h-40 w-full" />;
  }
  if (query.isError || !query.data) {
    return (
      <Alert variant="destructive">
        <AlertDescription>Could not read the secret store status.</AlertDescription>
      </Alert>
    );
  }

  if (!query.data.initialized) {
    return <SetupCard />;
  }
  if (!query.data.unlocked) {
    return <UnlockCard />;
  }

  return (
    <div className="space-y-6">
      {!query.data.unattendedUnlockConfigured && <UnattendedUnlockWarning />}
      {children}
    </div>
  );
}

/**
 * Without a key file the store comes up locked after every restart, and any
 * scheduled work that needs a credential simply stops. That is worth saying
 * before someone relies on backups that will quietly not run.
 */
function UnattendedUnlockWarning() {
  return (
    <Alert>
      <ShieldAlert className="size-4" />
      <AlertDescription>
        The secret store will be locked again after a restart, because no master key file is
        configured. Scheduled backups cannot read repository passwords while it is locked. See{" "}
        <span className="font-mono text-xs">deploy/docker-compose.secret.yml</span> to mount the
        passphrase as a Docker secret.
      </AlertDescription>
    </Alert>
  );
}

function SetupCard() {
  const queryClient = useQueryClient();
  const form = useForm<z.infer<typeof setupSchema>>({ resolver: zodResolver(setupSchema) });

  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof setupSchema>) => api.initializeSecretStore(values.passphrase),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["secret-store"] });
      toast.success("Secret store created.");
    },
    onError: (error) =>
      toast.error(error instanceof ApiError ? error.message : "Could not create the secret store."),
  });

  return (
    <Card>
      <CardContent className="max-w-lg space-y-4 p-6">
        <div className="flex items-center gap-2">
          <KeyRound className="size-5 text-muted-foreground" aria-hidden="true" />
          <h2 className="font-medium">Set up the secret store</h2>
        </div>
        <p className="text-sm text-muted-foreground">
          Repository and database credentials are encrypted with a master passphrase. It is never
          stored, so nobody — including Back-Orbit — can recover it. Keep a copy somewhere safe:
          without it, the credentials in this store are gone for good.
        </p>

        <form
          className="space-y-4"
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
          noValidate
        >
          <div className="space-y-2">
            <Label htmlFor="passphrase">Master passphrase</Label>
            <Input id="passphrase" type="password" autoComplete="new-password" {...form.register("passphrase")} />
            {form.formState.errors.passphrase && (
              <p className="text-sm text-destructive">{form.formState.errors.passphrase.message}</p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="confirm">Confirm passphrase</Label>
            <Input id="confirm" type="password" autoComplete="new-password" {...form.register("confirm")} />
            {form.formState.errors.confirm && (
              <p className="text-sm text-destructive">{form.formState.errors.confirm.message}</p>
            )}
          </div>
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "Creating…" : "Create secret store"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function UnlockCard() {
  const queryClient = useQueryClient();
  const [failed, setFailed] = useState(false);
  const form = useForm<z.infer<typeof unlockSchema>>({ resolver: zodResolver(unlockSchema) });

  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof unlockSchema>) => api.unlockSecretStore(values.passphrase),
    onSuccess: () => {
      setFailed(false);
      queryClient.invalidateQueries();
      toast.success("Secret store unlocked.");
    },
    onError: () => setFailed(true),
  });

  return (
    <Card>
      <CardContent className="max-w-lg space-y-4 p-6">
        <div className="flex items-center gap-2">
          <Lock className="size-5 text-muted-foreground" aria-hidden="true" />
          <h2 className="font-medium">The secret store is locked</h2>
        </div>
        <p className="text-sm text-muted-foreground">
          Repository passwords cannot be read until it is unlocked.
        </p>

        <form
          className="space-y-4"
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
          noValidate
        >
          <div className="space-y-2">
            <Label htmlFor="unlock-passphrase">Master passphrase</Label>
            <Input
              id="unlock-passphrase"
              type="password"
              autoComplete="current-password"
              {...form.register("passphrase")}
            />
          </div>
          {failed && (
            <Alert variant="destructive">
              <AlertDescription>Incorrect master passphrase.</AlertDescription>
            </Alert>
          )}
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "Unlocking…" : "Unlock"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
