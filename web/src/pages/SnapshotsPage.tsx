import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArchiveRestore, Camera, CheckCircle2, Database, FileCheck2, FolderArchive, HardDrive, Search, ShieldCheck } from "lucide-react";
import { Link } from "react-router-dom";

import { EmptyState } from "@/components/EmptyState";
import { PageHeader } from "@/components/PageHeader";
import { Timestamp } from "@/components/Timestamp";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api, type BackupRun, type BackupSnapshot } from "@/lib/api";
import { usePageTitle } from "@/hooks/usePageTitle";
import { formatBytes } from "@/lib/format";

interface SnapshotRecord { snapshot: BackupSnapshot; run: BackupRun }

export function SnapshotsPage() {
  usePageTitle("Snapshots");
  const [search, setSearch] = useState("");
  const [project, setProject] = useState("all");
  const [selected, setSelected] = useState<SnapshotRecord>();
  const query = useQuery({ queryKey: ["backups", 100], queryFn: () => api.listBackupRuns(100) });
  const records = useMemo(() => (query.data ?? []).flatMap((run) => run.snapshot ? [{ run, snapshot: run.snapshot }] : []), [query.data]);
  const projects = useMemo(() => [...new Set(records.map(({ run }) => run.projectName))].sort(), [records]);
  const filtered = useMemo(() => {
    const term = search.trim().toLowerCase();
    return records.filter(({ run, snapshot }) => (project === "all" || run.projectName === project) && (!term || [run.projectName, run.repositoryName, snapshot.id, snapshot.resticSnapshotId].some((value) => value.toLowerCase().includes(term))));
  }, [project, records, search]);
  const totalBytes = records.reduce((sum, item) => sum + item.snapshot.sizeBytes, 0);
  const latest = records[0]?.snapshot.createdAt;

  return <div className="space-y-6">
    <PageHeader title="Snapshots" description="Verified recovery points and the exact data captured in each backup." actions={<Button render={<Link to="/restore"/>}><ArchiveRestore/>Open restore</Button>}/>
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <Metric icon={Camera} label="Recovery points" value={records.length.toLocaleString()}/>
      <Metric icon={HardDrive} label="Protected data" value={formatBytes(totalBytes)}/>
      <Metric icon={ShieldCheck} label="Verified" value={records.filter(({snapshot})=>snapshot.verification.ok).length.toLocaleString()}/>
      <Metric icon={CheckCircle2} label="Latest snapshot" value={latest?<Timestamp iso={latest}/>:"None"}/>
    </div>
    <Card><CardContent className="flex flex-col gap-3 py-4 sm:flex-row">
      <label className="relative flex-1"><span className="sr-only">Search snapshots</span><Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"/><Input value={search} onChange={(event)=>setSearch(event.target.value)} className="pl-9" placeholder="Search project, repository, or snapshot ID…"/></label>
      <Select<string> value={project} onValueChange={(value)=>setProject(value??"all")}><SelectTrigger size="sm" className="w-full sm:w-56" aria-label="Filter by project"><SelectValue>{(value)=>value==="all"?"All projects":value}</SelectValue></SelectTrigger><SelectContent><SelectItem value="all">All projects</SelectItem>{projects.map(name=><SelectItem key={name} value={name}>{name}</SelectItem>)}</SelectContent></Select>
    </CardContent></Card>
    {query.isLoading?<LoadingList/>:query.isError?<Card><EmptyState icon={Camera} title="Snapshots could not be loaded" description="Check the Back-Orbit API and try again."/></Card>:filtered.length===0?<Card><EmptyState icon={Camera} title={records.length?"No snapshots match":"No recovery points yet"} description={records.length?"Adjust the search or project filter.":"Run a project backup. Verified snapshots will appear here automatically."}/></Card>:<div className="space-y-3">{filtered.map(record=><SnapshotCard key={record.snapshot.id} record={record} onDetails={()=>setSelected(record)}/>)}</div>}
    <SnapshotDetails record={selected} onOpenChange={(open)=>{if(!open)setSelected(undefined)}}/>
  </div>;
}

function SnapshotCard({record,onDetails}:{record:SnapshotRecord;onDetails:()=>void}) { const {run,snapshot}=record; const databases=snapshot.manifest.volumes.reduce((sum,volume)=>sum+(volume.sqliteDatabases?.length??0),0); return <Card><CardContent className="grid gap-4 py-4 lg:grid-cols-[minmax(0,1fr)_auto_auto] lg:items-center"><div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><Link to={`/projects/${run.projectId}`} className="font-medium hover:underline">{run.projectName}</Link><Badge variant={snapshot.verification.ok?"secondary":"destructive"}><ShieldCheck/>{snapshot.verification.ok?"Verified":"Check failed"}</Badge></div><div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground"><span><Timestamp iso={snapshot.createdAt}/></span><span>{run.repositoryName}</span><code title={snapshot.resticSnapshotId}>{snapshot.resticSnapshotId.slice(0,12)}</code></div></div><div className="grid grid-cols-3 gap-5 text-sm"><SmallStat label="Data" value={formatBytes(snapshot.sizeBytes)}/><SmallStat label="Files" value={snapshot.filesCount.toLocaleString()}/><SmallStat label="Sources" value={snapshot.manifest.volumes.length.toLocaleString()} detail={databases?`${databases} SQLite`:undefined}/></div><div className="flex gap-2"><Button variant="outline" onClick={onDetails}>Details</Button><Button render={<Link to={`/restore?snapshot=${encodeURIComponent(snapshot.id)}`}/>}><ArchiveRestore/>Restore</Button></div></CardContent></Card> }

function SnapshotDetails({record,onOpenChange}:{record?:SnapshotRecord;onOpenChange:(open:boolean)=>void}) { if(!record)return null;const {run,snapshot}=record;return <Dialog open onOpenChange={onOpenChange}><DialogContent className="sm:max-w-2xl"><DialogHeader><DialogTitle>{run.projectName} recovery point</DialogTitle><DialogDescription>Created <Timestamp iso={snapshot.createdAt}/> in {run.repositoryName}. Snapshot IDs are shown for disaster-recovery use.</DialogDescription></DialogHeader><div className="grid grid-cols-2 gap-3 sm:grid-cols-4"><SmallStat label="Data" value={formatBytes(snapshot.sizeBytes)}/><SmallStat label="Files" value={snapshot.filesCount.toLocaleString()}/><SmallStat label="Sources" value={snapshot.manifest.volumes.length.toLocaleString()}/><SmallStat label="Schema" value={`v${snapshot.manifest.schemaVersion}`}/></div><div className="max-h-72 space-y-2 overflow-y-auto">{snapshot.manifest.volumes.map(volume=><div key={`${volume.kind}:${volume.name}`} className="flex gap-3 rounded-lg border p-3">{volume.kind==="volume"?<HardDrive className="mt-0.5 size-4 shrink-0 text-muted-foreground"/>:<FolderArchive className="mt-0.5 size-4 shrink-0 text-muted-foreground"/>}<div className="min-w-0 flex-1"><div className="break-all text-sm font-medium">{volume.name}</div><div className="mt-1 text-xs text-muted-foreground">{volume.kind} · {volume.files.toLocaleString()} files · {formatBytes(volume.bytes)}</div>{volume.sqliteDatabases?.map(db=><div key={db.path} className="mt-2 flex items-center gap-2 rounded bg-muted/50 px-2 py-1 text-xs"><Database className="size-3"/><span className="truncate">{db.path}</span><span className="ml-auto shrink-0">{formatBytes(db.bytes)}</span></div>)}</div></div>)}</div><div className="rounded-lg bg-muted/50 p-3 text-xs"><div className="flex items-center gap-2 font-medium"><FileCheck2 className="size-4"/>Verification</div><p className="mt-1 text-muted-foreground">{snapshot.verification.checks?.join(" · ")||"Repository snapshot and manifest checked"}</p></div><div className="grid gap-1 font-mono text-[11px] text-muted-foreground"><span>Back-Orbit ID: {snapshot.id}</span><span>restic ID: {snapshot.resticSnapshotId}</span></div><DialogFooter><Button variant="outline" onClick={()=>onOpenChange(false)}>Close</Button><Button render={<Link to={`/restore?snapshot=${encodeURIComponent(snapshot.id)}`}/>}><ArchiveRestore/>Restore this snapshot</Button></DialogFooter></DialogContent></Dialog> }

function Metric({icon:Icon,label,value}:{icon:typeof Camera;label:string;value:ReactNode}) { return <Card><CardContent className="flex items-center gap-3 py-4"><span className="rounded-lg bg-muted p-2"><Icon className="size-4 text-muted-foreground"/></span><div><div className="text-xl font-semibold">{value}</div><div className="text-xs text-muted-foreground">{label}</div></div></CardContent></Card> }
function SmallStat({label,value,detail}:{label:string;value:string;detail?:string}) { return <div><div className="font-medium tabular-nums">{value}</div><div className="text-xs text-muted-foreground">{detail??label}</div></div> }
function LoadingList(){return <div className="space-y-3">{Array.from({length:4},(_,index)=><Skeleton key={index} className="h-24 w-full"/>)}</div>}
