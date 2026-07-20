import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { FolderKanban, ShieldOff, Activity as ActivityIcon } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { DockerStatusBanner } from "@/components/DockerStatusBanner";
import { api } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";

export function OverviewPage() {
  const projectsQuery = useQuery({ queryKey: ["projects"], queryFn: api.listProjects });
  const auditQuery = useQuery({
    queryKey: ["audit", 5],
    queryFn: () => api.listAudit(5),
  });

  const projects = projectsQuery.data ?? [];
  const unprotectedCount = projects.filter((p) => p.status === "unprotected").length;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
        <p className="text-sm text-muted-foreground">
          A quick look at what Back-Orbit knows about this host.
        </p>
      </div>

      <DockerStatusBanner />

      <div className="grid gap-4 sm:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Projects</CardTitle>
            <FolderKanban className="size-4 text-muted-foreground" aria-hidden="true" />
          </CardHeader>
          <CardContent>
            {projectsQuery.isLoading ? (
              <Skeleton className="h-8 w-12" />
            ) : (
              <div className="text-2xl font-semibold">{projects.length}</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Unprotected
            </CardTitle>
            <ShieldOff className="size-4 text-muted-foreground" aria-hidden="true" />
          </CardHeader>
          <CardContent>
            {projectsQuery.isLoading ? (
              <Skeleton className="h-8 w-12" />
            ) : (
              <div className="text-2xl font-semibold">{unprotectedCount}</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Recent activity
            </CardTitle>
            <ActivityIcon className="size-4 text-muted-foreground" aria-hidden="true" />
          </CardHeader>
          <CardContent>
            {auditQuery.isLoading ? (
              <Skeleton className="h-8 w-12" />
            ) : (
              <div className="text-2xl font-semibold">{auditQuery.data?.length ?? 0}</div>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Latest activity</CardTitle>
        </CardHeader>
        <CardContent>
          {auditQuery.isLoading ? (
            <div className="space-y-2">
              <Skeleton className="h-5 w-full" />
              <Skeleton className="h-5 w-full" />
              <Skeleton className="h-5 w-full" />
            </div>
          ) : auditQuery.data && auditQuery.data.length > 0 ? (
            <ul className="space-y-3">
              {auditQuery.data.map((event) => (
                <li key={event.id} className="flex items-center justify-between text-sm">
                  <span className="text-foreground">{event.action}</span>
                  <span className="text-muted-foreground">
                    {formatRelativeTime(event.createdAt)}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">No activity recorded yet.</p>
          )}
          <div className="mt-4">
            <Link to="/activity" className="text-sm font-medium text-primary hover:underline">
              View all activity →
            </Link>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
