import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ArrowRight, Database, FileText, FolderArchive, KeyRound, Leaf, ScanSearch, ShieldCheck, Zap } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError, api, type BlueprintFinding } from "@/lib/api";
import { cn } from "@/lib/utils";

const technologyStyle: Record<string, { icon: typeof Database; className: string; label: string }> = {
  mongodb: { icon: Leaf, className: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400", label: "MongoDB" },
  postgresql: { icon: Database, className: "bg-sky-500/10 text-sky-600 dark:text-sky-400", label: "PostgreSQL" },
  mysql: { icon: Database, className: "bg-blue-500/10 text-blue-600 dark:text-blue-400", label: "MySQL" },
  mariadb: { icon: Database, className: "bg-cyan-500/10 text-cyan-600 dark:text-cyan-400", label: "MariaDB" },
  sqlite: { icon: FileText, className: "bg-indigo-500/10 text-indigo-600 dark:text-indigo-400", label: "SQLite" },
  redis: { icon: Zap, className: "bg-red-500/10 text-red-600 dark:text-red-400", label: "Redis" },
  valkey: { icon: Zap, className: "bg-rose-500/10 text-rose-600 dark:text-rose-400", label: "Valkey" },
};

export function ProtectionSummary({ projectId, onOpenBlueprint }: { projectId: string; onOpenBlueprint: () => void }) {
  const query = useQuery({
    queryKey: ["project-blueprint", projectId],
    queryFn: () => api.getProjectBlueprint(projectId),
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 2,
  });

  if (query.isLoading) return <Skeleton className="h-52 w-full" />;
  const missing = query.error instanceof ApiError && query.error.status === 404;
  if (!query.data) return (
    <Card>
      <CardContent className="flex flex-wrap items-center justify-between gap-4 py-6">
        <div className="flex items-center gap-4"><span className="rounded-xl bg-primary/10 p-3"><ScanSearch className="size-5 text-primary" /></span><div><h2 className="font-semibold">Project analysis</h2><p className="mt-1 text-sm text-muted-foreground">{missing ? "Analyze this project to identify databases and storage." : "Analysis information is currently unavailable."}</p></div></div>
        <Button variant="outline" onClick={onOpenBlueprint}>Open blueprint <ArrowRight className="size-4" /></Button>
      </CardContent>
    </Card>
  );

  const bp = query.data;
  const databases = bp.findings.filter((finding) => finding.kind === "database");
  const storage = bp.findings.filter((finding) => finding.kind === "storage");
  const secrets = bp.findings.filter((finding) => finding.kind === "secret");
  const configs = bp.findings.filter((finding) => finding.kind === "configuration");
  const warningCount = bp.warnings.length + bp.findings.reduce((total, finding) => total + (finding.warnings?.length ?? 0), 0);
  const uniqueDatabases = Array.from(new Map(databases.map((finding) => [finding.id, finding])).values());

  return <Card className="overflow-hidden">
    <CardHeader className="border-b bg-muted/20">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div><CardTitle className="flex items-center gap-2 text-base"><ShieldCheck className="size-4 text-primary" />Project protection summary</CardTitle><p className="mt-1 text-xs text-muted-foreground">Last analyzed {new Date(bp.analyzedAt).toLocaleString()}</p></div>
        <div className="flex items-center gap-2">{bp.drifted && <Badge variant="destructive"><AlertTriangle />Changed</Badge>}{bp.confirmedAt && !bp.drifted && <Badge variant="secondary">Reviewed</Badge>}<Button variant="ghost" size="sm" onClick={onOpenBlueprint}>View blueprint <ArrowRight className="size-4" /></Button></div>
      </div>
    </CardHeader>
    <CardContent className="space-y-5 py-5">
      <div>
        <p className="mb-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">Detected data stores</p>
        {uniqueDatabases.length === 0 ? <p className="text-sm text-muted-foreground">No databases confidently identified.</p> : <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">{uniqueDatabases.map((finding) => <Technology key={finding.id} finding={finding} />)}</div>}
      </div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <Metric icon={FolderArchive} label="Storage sources" value={storage.length} />
        <Metric icon={KeyRound} label="Secret references" value={secrets.length} />
        <Metric icon={FileText} label="Configuration" value={configs.length} />
        <Metric icon={warningCount > 0 ? AlertTriangle : ShieldCheck} label="Warnings" value={warningCount} warning={warningCount > 0} />
      </div>
    </CardContent>
  </Card>;
}

function Technology({ finding }: { finding: BlueprintFinding }) {
  const style = technologyStyle[finding.technology] ?? { icon: Database, className: "bg-primary/10 text-primary", label: finding.technology };
  const Icon = style.icon;
  return <div className="flex items-center gap-3 rounded-xl border bg-background p-3"><span className={cn("rounded-lg p-2", style.className)}><Icon className="size-5" /></span><div className="min-w-0"><div className="truncate font-medium">{style.label}</div><div className="truncate text-xs text-muted-foreground">{finding.service || finding.evidence[0]?.subject || finding.confidence}</div></div></div>;
}

function Metric({ icon: Icon, label, value, warning = false }: { icon: typeof Database; label: string; value: number; warning?: boolean }) {
  return <div className="rounded-lg bg-muted/40 p-3"><div className={cn("flex items-center gap-1.5 text-xs text-muted-foreground", warning && "text-warning")}><Icon className="size-3.5" />{label}</div><div className="mt-1 text-xl font-semibold tabular-nums">{value}</div></div>;
}
