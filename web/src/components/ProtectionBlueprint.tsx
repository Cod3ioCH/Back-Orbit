import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Check, Database, FileKey, FolderArchive, RefreshCw, ScanSearch } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError, api, type BlueprintFinding } from "@/lib/api";
import { cn } from "@/lib/utils";

const icons = { database: Database, storage: FolderArchive, secret: FileKey, configuration: FileKey };

export function ProtectionBlueprint({ projectId }: { projectId: string }) {
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["project-blueprint", projectId],
    queryFn: () => api.getProjectBlueprint(projectId),
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 2,
  });
  const analyze = useMutation({
    mutationFn: () => api.analyzeProject(projectId),
    onSuccess: (data) => client.setQueryData(["project-blueprint", projectId], data),
    onError: () => toast.error("Project analysis failed"),
  });
  const confirm = useMutation({
    mutationFn: () => api.confirmProjectBlueprint(projectId),
    onSuccess: (data) => client.setQueryData(["project-blueprint", projectId], data),
    onError: () => toast.error("Blueprint confirmation failed"),
  });

  if (query.isLoading) return <div className="space-y-3"><Skeleton className="h-28" /><Skeleton className="h-48" /></div>;
  const missing = query.error instanceof ApiError && query.error.status === 404;
  if (query.isError && !missing) return <Alert variant="destructive"><AlertDescription>Failed to load the protection blueprint.</AlertDescription></Alert>;
  const bp = query.data;

  if (!bp) return (
    <Card className="overflow-hidden">
      <CardContent className="flex min-h-72 flex-col items-center justify-center px-6 text-center">
        <span className="mb-5 rounded-2xl bg-primary/10 p-4"><ScanSearch className="size-8 text-primary" /></span>
        <h2 className="text-lg font-semibold">Understand this project before protecting it</h2>
        <p className="mt-2 max-w-xl text-sm leading-6 text-muted-foreground">Analyze Compose configuration and live Docker metadata to identify databases, persistent storage, configuration, and secret references. Secret values are never read.</p>
        <Button className="mt-6" onClick={() => analyze.mutate()} disabled={analyze.isPending}>{analyze.isPending ? "Analyzing…" : "Analyze project"}</Button>
      </CardContent>
    </Card>
  );

  const grouped = bp.findings.reduce<Record<string, BlueprintFinding[]>>((groups, finding) => {
    (groups[finding.kind] ??= []).push(finding);
    return groups;
  }, {});
  return <div className="space-y-4">
    {bp.drifted && <Alert><AlertTriangle className="size-4" /><AlertDescription>The project changed since this blueprint was confirmed. Review the findings before the next backup.</AlertDescription></Alert>}
    {bp.warnings.map((warning) => <Alert key={warning}><AlertTriangle className="size-4" /><AlertDescription>{warning}</AlertDescription></Alert>)}
    <Card>
      <CardContent className="flex flex-wrap items-center justify-between gap-4 py-5">
        <div><div className="flex items-center gap-2"><h2 className="font-semibold">Protection blueprint</h2>{bp.confirmedAt && !bp.drifted && <Badge variant="secondary"><Check className="mr-1 size-3" />Reviewed</Badge>}</div><p className="mt-1 text-sm text-muted-foreground">{bp.findings.length} findings · analyzed {new Date(bp.analyzedAt).toLocaleString()}</p></div>
        <div className="flex gap-2"><Button variant="outline" onClick={() => analyze.mutate()} disabled={analyze.isPending}><RefreshCw className={cn("size-4", analyze.isPending && "animate-spin")} />Re-analyze</Button><Button onClick={() => confirm.mutate()} disabled={confirm.isPending || (!!bp.confirmedAt && !bp.drifted)}>{confirm.isPending ? "Confirming…" : "Confirm blueprint"}</Button></div>
      </CardContent>
    </Card>
    {bp.findings.length === 0 && <Alert><AlertDescription>No persistent components were identified. This is not proof that the project is stateless; review its configuration manually.</AlertDescription></Alert>}
    {Object.entries(grouped).map(([kind, findings]) => findings && <FindingGroup key={kind} kind={kind} findings={findings} />)}
    <Card><CardHeader><CardTitle className="text-base">Recommended backup sequence</CardTitle></CardHeader><CardContent><ol className="space-y-4">{bp.steps.map((step) => <li key={step.action} className="flex gap-3"><span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">{step.order}</span><div><div className="font-medium capitalize">{step.action.replaceAll("_", " ")}</div><p className="mt-0.5 text-sm text-muted-foreground">{step.description}</p></div></li>)}</ol></CardContent></Card>
  </div>;
}

function FindingGroup({ kind, findings }: { kind: string; findings: BlueprintFinding[] }) {
  const Icon = icons[kind as keyof typeof icons] ?? ScanSearch;
  return <Card><CardHeader><CardTitle className="flex items-center gap-2 text-base"><Icon className="size-4 text-muted-foreground" />{kind[0].toUpperCase() + kind.slice(1)}<Badge variant="secondary">{findings.length}</Badge></CardTitle></CardHeader><CardContent className="grid gap-3 lg:grid-cols-2">{findings.map((finding) => <article key={finding.id} className="rounded-xl border bg-muted/20 p-4"><div className="flex flex-wrap items-start justify-between gap-2"><div><h3 className="font-medium capitalize">{finding.technology.replaceAll("-", " ")}</h3>{finding.service && <p className="font-mono text-xs text-muted-foreground">{finding.service}</p>}</div><Badge variant="outline" className={cn(finding.confidence === "confirmed" && "border-success/40 text-success", finding.confidence === "possible" && "border-warning/40 text-warning")}>{finding.confidence}</Badge></div><p className="mt-3 text-sm leading-6">{finding.recommendation}</p><div className="mt-3 flex flex-wrap gap-2"><Badge variant="secondary">{finding.consistency}</Badge></div><details className="mt-3 text-xs text-muted-foreground"><summary className="cursor-pointer select-none">Why was this detected?</summary><ul className="mt-2 space-y-1">{finding.evidence.map((evidence) => <li key={`${evidence.source}:${evidence.subject}:${evidence.detail}`}>{evidence.detail}</li>)}</ul></details>{finding.warnings?.map((warning) => <p key={warning} className="mt-3 text-xs text-warning">{warning}</p>)}</article>)}</CardContent></Card>;
}
