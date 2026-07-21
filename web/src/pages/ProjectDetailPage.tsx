import { useState } from "react";
import { useParams, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { BackupPanel } from "@/components/BackupPanel";
import { ProtectionBlueprint } from "@/components/ProtectionBlueprint";
import { ProtectionSummary } from "@/components/ProtectionSummary";
import { ConfigurationTab, DatabasesTab, ProjectActivityTab, ProjectSettingsTab, SnapshotsTab, VolumesTab } from "@/components/ProjectTabs";
import { StatusBadge } from "@/components/StatusBadge";
import { Timestamp } from "@/components/Timestamp";
import { api } from "@/lib/api";
import { usePageTitle } from "@/hooks/usePageTitle";
import { cn } from "@/lib/utils";

const PROJECT_TABS = [
  { value: "configuration", label: "Configuration" },
  { value: "volumes", label: "Volumes" },
  { value: "databases", label: "Databases" },
  { value: "snapshots", label: "Snapshots" },
  { value: "activity", label: "Activity" },
  { value: "settings", label: "Settings" },
] as const;

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [activeTab, setActiveTab] = useState("overview");
  const query = useQuery({
    queryKey: ["project", id],
    queryFn: () => api.getProject(id!),
    enabled: !!id,
  });

  usePageTitle(query.data?.name);

  if (query.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  if (query.isError || !query.data) {
    return (
      <Alert variant="destructive">
        <AlertDescription>Failed to load this project.</AlertDescription>
      </Alert>
    );
  }

  const project = query.data;

  return (
    <div className="space-y-6">
      <div>
        <Link
          to="/projects"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-3.5" />
          Back to projects
        </Link>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{project.name}</h1>
          <StatusBadge status={project.status} />
        </div>
        <p className="mt-1 font-mono text-xs text-muted-foreground">
          {project.composePath || "No compose path recorded"}
        </p>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="blueprint">Blueprint</TabsTrigger>
          {PROJECT_TABS.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          {project.dockerWarning && (
            <Alert>
              <AlertDescription>{project.dockerWarning}</AlertDescription>
            </Alert>
          )}

          <ProtectionSummary projectId={project.id} onOpenBlueprint={() => setActiveTab("blueprint")} />

          {/* First on the page: whether this project is backed up is the
              question someone opens it to answer. */}
          <BackupPanel project={project} />

          <Card>
            <CardHeader>
              <CardTitle className="text-base">
                Containers {project.dockerAvailable && `(${project.containers.length})`}
              </CardTitle>
            </CardHeader>
            <CardContent>
              {!project.dockerAvailable || project.containers.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No live container data available for this project right now.
                </p>
              ) : (
                <ul className="divide-y">
                  {project.containers.map((c) => (
                    <li
                      key={c.id}
                      className="flex flex-wrap items-center justify-between gap-2 py-2 text-sm"
                    >
                      <div className="min-w-0">
                        <div className="truncate font-medium">{c.name}</div>
                        <div className="truncate text-xs text-muted-foreground">
                          {c.service && `${c.service} · `}
                          {c.image}
                        </div>
                      </div>
                      {/* A coloured dot makes container health scannable at a
                          glance; previously running and stopped containers were
                          both just grey text. */}
                      <div className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
                        <span
                          className={cn(
                            "size-2 rounded-full",
                            c.state === "running" ? "bg-success" : "bg-muted-foreground/40",
                          )}
                          aria-hidden="true"
                        />
                        {c.status || c.state}
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>

          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Volumes</CardTitle>
              </CardHeader>
              <CardContent>
                {project.volumes.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No named volumes detected.</p>
                ) : (
                  <ul className="space-y-1 text-sm">
                    {project.volumes.map((v) => (
                      <li key={v.name} className="font-mono text-xs">
                        {v.name}
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-base">Networks</CardTitle>
              </CardHeader>
              <CardContent>
                {project.networks.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No networks detected.</p>
                ) : (
                  <ul className="space-y-1 text-sm">
                    {project.networks.map((n) => (
                      <li key={n.id} className="font-mono text-xs">
                        {n.name}
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Compose files</CardTitle>
            </CardHeader>
            <CardContent>
              {project.composeFiles.length === 0 ? (
                <p className="text-sm text-muted-foreground">No compose files recorded.</p>
              ) : (
                <ul className="space-y-1 text-sm">
                  {project.composeFiles.map((f) => (
                    <li key={f} className="font-mono text-xs">
                      {f}
                    </li>
                  ))}
                </ul>
              )}
              <p className="mt-3 flex flex-wrap gap-x-1 text-xs text-muted-foreground">
                <span>Registered</span>
                <Timestamp iso={project.createdAt} />
                {project.updatedAt !== project.createdAt && (
                  <>
                    <span>· updated</span>
                    <Timestamp iso={project.updatedAt} />
                  </>
                )}
              </p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="blueprint">
          <ProtectionBlueprint projectId={project.id} />
        </TabsContent>

        <TabsContent value="configuration">
          <ConfigurationTab project={project} />
        </TabsContent>
        <TabsContent value="volumes">
          <VolumesTab project={project} />
        </TabsContent>
        <TabsContent value="databases">
          <DatabasesTab projectId={project.id} />
        </TabsContent>
        <TabsContent value="snapshots">
          <SnapshotsTab projectId={project.id} />
        </TabsContent>
        <TabsContent value="activity">
          <ProjectActivityTab projectId={project.id} />
        </TabsContent>
        <TabsContent value="settings">
          <ProjectSettingsTab project={project} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
