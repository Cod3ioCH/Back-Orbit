import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ShieldAlert } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { api } from "@/lib/api";

// DockerStatusBanner always surfaces Back-Orbit's security notice about
// Docker socket access, and additionally warns if the daemon is currently
// unreachable — see docs/threat-model.md.
export function DockerStatusBanner() {
  const { data } = useQuery({ queryKey: ["docker-status"], queryFn: api.dockerStatus });

  if (!data) {
    return null;
  }

  if (!data.connected) {
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

  return (
    <Alert>
      <ShieldAlert className="size-4" />
      <AlertTitle>Docker access notice</AlertTitle>
      <AlertDescription>{data.securityNotice}</AlertDescription>
    </Alert>
  );
}
