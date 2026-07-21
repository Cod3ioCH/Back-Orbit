import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArchiveRestore, Boxes, CheckCircle2, Copy, FolderOutput, Loader2, ShieldAlert, XCircle } from "lucide-react";
import { toast } from "sonner";
import { PageHeader } from "@/components/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, type RestoreMode } from "@/lib/api";
import { usePageTitle } from "@/hooks/usePageTitle";

const modes: {id:RestoreMode;title:string;description:string;icon:typeof FolderOutput}[] = [
  {id:"extract",title:"Extract safely",description:"Restore into a new protected folder without touching the running project.",icon:FolderOutput},
  {id:"in_place",title:"Restore in place",description:"Replace the current project's data and restart its services.",icon:ArchiveRestore},
  {id:"clone",title:"Deploy as new",description:"Create an independent Compose project with remapped resources.",icon:Copy},
];
const fmt=(n:number)=>new Intl.NumberFormat("en",{style:"unit",unit:"byte",notation:"compact",unitDisplay:"narrow"}).format(n);

export function RestorePage(){
  usePageTitle("Restore"); const qc=useQueryClient(); const [snapshotId,setSnapshotId]=useState("");const [mode,setMode]=useState<RestoreMode>("extract");const [name,setName]=useState("");
  const backups=useQuery({queryKey:["backups",100],queryFn:()=>api.listBackupRuns(100)});const snapshots=useMemo(()=>backups.data?.flatMap(r=>r.snapshot?[{...r.snapshot,projectName:r.projectName,repositoryName:r.repositoryName}]:[])??[],[backups.data]);
  const preview=useMutation({mutationFn:()=>api.previewRestore(snapshotId,mode,name)});
  const start=useMutation({mutationFn:()=>api.startRestore(snapshotId,mode,name),onSuccess:()=>{toast.success("Restore started in an isolated directory");qc.invalidateQueries({queryKey:["restores"]})},onError:(e)=>toast.error(e.message)});
  const runs=useQuery({queryKey:["restores"],queryFn:()=>api.listRestoreRuns(),refetchInterval:q=>q.state.data?.some(r=>r.status==="running")?1500:false});
  return <div className="space-y-6"><PageHeader title="Restore" description="Preview impact first. Back-Orbit only enables actions that the selected snapshot can perform safely."/>
    <div className="grid gap-6 xl:grid-cols-[minmax(0,1.4fr)_minmax(320px,.6fr)]"><div className="space-y-5">
      <Card><CardHeader><CardTitle>1. Choose a recovery point</CardTitle><CardDescription>Only verified snapshots are available.</CardDescription></CardHeader><CardContent><Label htmlFor="snapshot">Snapshot</Label><select id="snapshot" value={snapshotId} onChange={e=>{setSnapshotId(e.target.value);preview.reset()}} className="mt-2 h-10 w-full rounded-lg border bg-background px-3 text-sm"><option value="">Select a snapshot…</option>{snapshots.map(s=><option key={s.id} value={s.id}>{s.projectName} · {new Date(s.createdAt).toLocaleString()} · {s.repositoryName}</option>)}</select></CardContent></Card>
      <Card><CardHeader><CardTitle>2. Choose the outcome</CardTitle><CardDescription>Extraction is non-destructive. The other modes require a complete, portable project bundle.</CardDescription></CardHeader><CardContent className="grid gap-3 md:grid-cols-3">{modes.map(m=>{const Icon=m.icon;return <button key={m.id} onClick={()=>{setMode(m.id);preview.reset()}} className={`rounded-xl border p-4 text-left transition ${mode===m.id?"border-primary bg-primary/5 ring-2 ring-primary/15":"hover:bg-muted/50"}`}><Icon className="mb-3 size-5"/><div className="font-medium">{m.title}</div><p className="mt-1 text-xs leading-5 text-muted-foreground">{m.description}</p></button>})}</CardContent></Card>
      {mode==="clone"&&<Card><CardContent><Label htmlFor="clone-name">New Compose project name</Label><Input id="clone-name" className="mt-2" value={name} onChange={e=>{setName(e.target.value);preview.reset()}} placeholder="my-project-recovered"/></CardContent></Card>}
      <Button disabled={!snapshotId||preview.isPending} onClick={()=>preview.mutate()}>{preview.isPending?<Loader2 className="animate-spin"/>:<ShieldAlert/>}Run dry run</Button>
      {preview.data&&<Card className={preview.data.supported?"ring-success/30":"ring-destructive/30"}><CardHeader><CardTitle className="flex items-center gap-2">{preview.data.supported?<CheckCircle2 className="size-5 text-success"/>:<XCircle className="size-5 text-destructive"/>}{preview.data.supported?"Ready to restore":"Action blocked safely"}</CardTitle><CardDescription>{preview.data.files.toLocaleString()} files · {fmt(preview.data.estimatedBytes)} · {preview.data.items.length} data sources</CardDescription></CardHeader><CardContent className="space-y-4">{preview.data.blockers.map(i=><div key={i.code} className="rounded-lg border border-destructive/25 bg-destructive/5 p-3 text-sm text-destructive">{i.message}</div>)}{preview.data.warnings.map(i=><div key={i.code} className="rounded-lg border bg-muted/40 p-3 text-sm">{i.message}</div>)}<div className="divide-y rounded-lg border">{preview.data.items.map(i=><div key={`${i.kind}-${i.name}`} className="flex items-center justify-between gap-3 p-3"><div><div className="font-medium">{i.name}</div><div className="text-xs text-muted-foreground">{i.kind} · {i.files.toLocaleString()} files</div></div><span className="text-sm text-muted-foreground">{fmt(i.bytes)}</span></div>)}</div>{preview.data.supported&&<Button disabled={start.isPending} onClick={()=>start.mutate()}>{start.isPending?<Loader2 className="animate-spin"/>:<ArchiveRestore/>}Restore to isolated directory</Button>}</CardContent></Card>}
    </div><Card><CardHeader><CardTitle>Restore activity</CardTitle><CardDescription>Long-running restores continue when this page is closed.</CardDescription></CardHeader><CardContent className="space-y-3">{(runs.data??[]).length===0?<div className="py-8 text-center text-sm text-muted-foreground">No restores have been started.</div>:runs.data?.map(r=><div key={r.id} className="rounded-lg border p-3"><div className="flex items-center justify-between"><div className="flex items-center gap-2"><Boxes className="size-4"/><span className="font-medium">{r.projectName}</span></div><Badge variant="outline">{r.status}</Badge></div><div className="mt-2 text-xs text-muted-foreground">{r.status==="completed"?r.targetPath:new Date(r.startedAt).toLocaleString()}</div>{r.error&&<p className="mt-2 text-xs text-destructive">{r.error}</p>}{r.status==="running"&&<Button className="mt-3" size="sm" variant="outline" onClick={()=>api.cancelRestore(r.id).then(()=>qc.invalidateQueries({queryKey:["restores"]}))}>Cancel</Button>}</div>)}</CardContent></Card></div>
  </div>
}
