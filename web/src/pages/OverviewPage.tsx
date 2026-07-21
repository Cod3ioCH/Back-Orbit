import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { FolderKanban, ShieldCheck, ShieldOff, ArrowRight } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { DockerUnreachableAlert } from "@/components/DockerStatus";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { Timestamp } from "@/components/Timestamp";
import { api, type ProjectRecord } from "@/lib/api";
import { describeEvent, eventDetail, TONE_CLASSES } from "@/lib/events";
import { usePageTitle } from "@/hooks/usePageTitle";
import { cn } from "@/lib/utils";

export function OverviewPage() {
  usePageTitle("Overview");

  const projectsQuery = useQuery({ queryKey: ["projects"], queryFn: api.listProjects });
  const auditQuery = useQuery({ queryKey: ["audit", 6], queryFn: () => api.listAudit(6) });

  const projects = projectsQuery.data ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Overview"
        description="Whether the Docker Compose projects on this host are protected."
      />

      <DockerUnreachableAlert />

      <ProtectionSummary projects={projects} isLoading={projectsQuery.isLoading} />

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <CardTitle className="text-base">Latest activity</CardTitle>
          <Link
            to="/activity"
            className="text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
          >
            View all
          </Link>
        </CardHeader>
        <CardContent>
          {auditQuery.isLoading ? (
            <div className="space-y-3">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-6 w-full" />
              ))}
            </div>
          ) : !auditQuery.data || auditQuery.data.length === 0 ? (
            <p className="py-4 text-sm text-muted-foreground">No activity recorded yet.</p>
          ) : (
            <ul className="space-y-3">
              {auditQuery.data.map((event) => {
                const { label, icon: Icon, tone } = describeEvent(event);
                const detail = eventDetail(event);

                return (
                  <li key={event.id} className="flex items-center gap-3">
                    <Icon
                      className={cn("size-4 shrink-0", TONE_CLASSES[tone])}
                      aria-hidden="true"
                    />
                    <span className="min-w-0 flex-1 truncate text-sm">
                      {label}
                      {detail && (
                        <span className="text-muted-foreground"> · {detail}</span>
                      )}
                    </span>
                    <Timestamp
                      iso={event.createdAt}
                      className="shrink-0 text-xs text-muted-foreground"
                    />
                  </li>
                );
              })}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

/**
 * ProtectionSummary answers the one question a backup tool exists to answer:
 * is my data safe? The previous dashboard showed three equally-weighted
 * numbers, which meant "every project is unprotected" — the most important
 * state this product can report — looked exactly like ordinary information.
 */
function ProtectionSummary({
  projects,
  isLoading,
}: {
  projects: ProjectRecord[];
  isLoading: boolean;
}) {
  if (isLoading) {
    return <Skeleton className="h-36 w-full" />;
  }

  if (projects.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={FolderKanban}
          title="No projects yet"
          description="Back-Orbit discovers Docker Compose projects running on this host. Scan to find them, or register a project directory manually."
          action={
            <Button render={<Link to="/projects" />}>
              Go to projects
              <ArrowRight className="size-4" />
            </Button>
          }
        />
      </Card>
    );
  }

  const protectedCount = projects.filter((p) => p.status !== "unprotected").length;
  const unprotectedCount = projects.length - protectedCount;
  const allUnprotected = protectedCount === 0;

  return (
    <div className="grid gap-4 md:grid-cols-3">
      <Card
        className={cn(
          "md:col-span-2",
          allUnprotected && "border-warning/40 bg-warning/5",
        )}
      >
        <CardContent className="flex items-start gap-4 p-6">
          <div
            className={cn(
              "flex size-10 shrink-0 items-center justify-center rounded-full",
              allUnprotected ? "bg-warning/15 text-warning" : "bg-success/15 text-success",
            )}
          >
            {allUnprotected ? (
              <ShieldOff className="size-5" aria-hidden="true" />
            ) : (
              <ShieldCheck className="size-5" aria-hidden="true" />
            )}
          </div>
          <div className="min-w-0">
            <p className="font-medium">
              {allUnprotected
                ? `No project is backed up yet`
                : `${protectedCount} of ${projects.length} projects protected`}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              {allUnprotected
                ? `Back-Orbit is tracking ${projects.length} ${projects.length === 1 ? "project" : "projects"} but nothing is being backed up. Backup plans arrive in a later phase.`
                : `${unprotectedCount} ${unprotectedCount === 1 ? "project has" : "projects have"} no backup plan yet.`}
            </p>
            <Button
              render={<Link to="/projects" />}
              variant="outline"
              size="sm"
              className="mt-3"
            >
              Review projects
              <ArrowRight className="size-4" />
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 sm:grid-cols-2 md:grid-cols-1">
        <StatCard label="Projects" value={projects.length} icon={FolderKanban} />
        <StatCard
          label="Unprotected"
          value={unprotectedCount}
          icon={ShieldOff}
          emphasis={unprotectedCount > 0}
        />
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  icon: Icon,
  emphasis,
}: {
  label: string;
  value: number;
  icon: typeof FolderKanban;
  emphasis?: boolean;
}) {
  return (
    <Card>
      <CardContent className="flex items-center justify-between p-4">
        <div>
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className={cn("text-2xl font-semibold", emphasis && "text-warning")}>{value}</p>
        </div>
        <Icon
          className={cn("size-4", emphasis ? "text-warning" : "text-muted-foreground")}
          aria-hidden="true"
        />
      </CardContent>
    </Card>
  );
}
