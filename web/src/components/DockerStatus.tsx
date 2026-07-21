import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ShieldAlert } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";

/**
 * DockerUnreachableAlert surfaces only the *actionable* Docker problem: the
 * daemon cannot be reached, so discovery and backups are broken right now.
 *
 * The permanent "socket access is root-equivalent" security notice
 * deliberately does not live here any more. It never changes, so repeating it
 * as a full-width block at the top of every page cost the most valuable space
 * on screen and trained people to ignore the spot where real warnings appear.
 * It is now always visible, but quietly, in the sidebar's Docker status
 * (see DockerStatusIndicator).
 */
export function DockerUnreachableAlert() {
  const { data } = useQuery({ queryKey: ["docker-status"], queryFn: api.dockerStatus });

  if (!data || data.connected) {
    return null;
  }

  return (
    <Alert variant="destructive">
      <AlertTriangle className="size-4" />
      <AlertTitle>Docker daemon unreachable</AlertTitle>
      <AlertDescription>
        {data.error ?? "Back-Orbit could not reach the Docker daemon."} Project discovery and
        backups are unavailable until this is resolved.
      </AlertDescription>
    </Alert>
  );
}

/**
 * DockerStatusIndicator is the always-present, low-key home for Docker
 * connection state. It sits in the sidebar footer: a glance tells you whether
 * Back-Orbit can talk to Docker, and the security implications of socket
 * access are one click away instead of occupying the top of every page.
 */
export function DockerStatusIndicator() {
  const { data, isLoading } = useQuery({
    queryKey: ["docker-status"],
    queryFn: api.dockerStatus,
  });

  const connected = data?.connected ?? false;
  const label = isLoading ? "Checking Docker…" : connected ? "Docker connected" : "Docker offline";

  return (
    <Dialog>
      <DialogTrigger
        render={
          <button
            type="button"
            className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          />
        }
      >
        <span
          className={cn(
            "size-2 shrink-0 rounded-full",
            isLoading ? "bg-muted-foreground/40" : connected ? "bg-success" : "bg-destructive",
          )}
          aria-hidden="true"
        />
        <span className="truncate">{label}</span>
        <ShieldAlert className="ml-auto size-3.5 shrink-0 opacity-60" aria-hidden="true" />
      </DialogTrigger>

      <DialogContent>
        <DialogHeader>
          <DialogTitle>Docker access</DialogTitle>
          <DialogDescription>
            How Back-Orbit reaches Docker, and what that means for the security of this host.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 text-sm">
          <div className="flex items-center gap-2">
            <span
              className={cn(
                "size-2 rounded-full",
                connected ? "bg-success" : "bg-destructive",
              )}
              aria-hidden="true"
            />
            <span className="font-medium">{connected ? "Connected" : "Not connected"}</span>
          </div>

          {data?.host && (
            <dl className="space-y-1">
              <div className="flex gap-2">
                <dt className="w-24 shrink-0 text-muted-foreground">Endpoint</dt>
                <dd className="min-w-0 truncate font-mono text-xs">{data.host}</dd>
              </div>
              {data.serverVersion && (
                <div className="flex gap-2">
                  <dt className="w-24 shrink-0 text-muted-foreground">Server</dt>
                  <dd className="min-w-0 truncate">{data.serverVersion}</dd>
                </div>
              )}
              {data.apiVersion && (
                <div className="flex gap-2">
                  <dt className="w-24 shrink-0 text-muted-foreground">API</dt>
                  <dd className="min-w-0 truncate">{data.apiVersion}</dd>
                </div>
              )}
            </dl>
          )}

          {data?.error && <p className="text-destructive">{data.error}</p>}

          {data?.securityNotice && (
            <div className="rounded-md border bg-muted/40 p-3 text-muted-foreground">
              {data.securityNotice}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
