import { useState } from "react";
import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { AlertTriangle, Camera, CheckCircle2, Database, FileCode2, FolderArchive, HardDrive, Network, Settings2, ShieldAlert, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Timestamp } from "@/components/Timestamp";
import { ApiError, api, type AuditEvent, type BlueprintFinding, type ProjectDetail } from "@/lib/api";
import { describeEvent, eventDetail, TONE_CLASSES } from "@/lib/events";
import { cn } from "@/lib/utils";

function useBlueprint(projectId: string) {
  return useQuery({ queryKey:["project-blueprint",projectId],queryFn:()=>api.getProjectBlueprint(projectId),retry:(count,error)=>!(error instanceof ApiError&&error.status===404)&&count<2 });
}

export function ConfigurationTab({ project }: { project: ProjectDetail }) {
  const blueprint=useBlueprint(project.id);const configs=blueprint.data?.findings.filter(f=>f.kind==="configuration")??[];
  return <div className="grid gap-4 lg:grid-cols-2"><InfoCard icon={FileCode2} title="Compose files">{project.composeFiles.length?<ItemList items={project.composeFiles}/>:<Muted>No Compose files recorded.</Muted>}</InfoCard><InfoCard icon={Network} title="Runtime topology"><div className="grid grid-cols-3 gap-3"><Stat label="Services" value={project.containers.length}/><Stat label="Networks" value={project.networks.length}/><Stat label="Images" value={new Set(project.containers.map(c=>c.image)).size}/></div></InfoCard><InfoCard icon={FileCode2} title="Configuration references" className="lg:col-span-2">{blueprint.isLoading?<Skeleton className="h-20"/>:configs.length?<FindingList findings={configs}/>:<Muted>No Compose configs or env files detected.</Muted>}</InfoCard></div>;
}

export function VolumesTab({ project }: { project: ProjectDetail }) {
  return <div className="space-y-4"><div className="grid gap-3 sm:grid-cols-3"><Summary icon={HardDrive} label="Named volumes" value={project.sources.filter(s=>s.kind==="volume").length}/><Summary icon={FolderArchive} label="Bind mounts" value={project.sources.filter(s=>s.kind==="bind").length}/><Summary icon={ShieldAlert} label="Excluded" value={project.sources.filter(s=>!!s.skipped).length}/></div><div className="grid gap-3 lg:grid-cols-2">{project.sources.map(source=><Card key={`${source.kind}:${source.name}`}><CardContent className="flex gap-3 py-4"><span className="rounded-lg bg-muted p-2">{source.kind==="volume"?<HardDrive className="size-4"/>:<FolderArchive className="size-4"/>}</span><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><span className="font-medium">{source.kind==="volume"?"Named volume":"Bind mount"}</span>{source.skipped?<Badge variant="destructive">Excluded</Badge>:<Badge variant="secondary">Included</Badge>}</div><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{source.name}</p><p className="mt-1 text-xs text-muted-foreground">Mounted at {source.mountedAt||"unknown"}{source.services.length?` · ${source.services.join(", ")}`:""}</p>{source.skipped&&<p className="mt-2 text-xs text-destructive">{source.skipped}</p>}</div></CardContent></Card>)}</div>{project.sources.length===0&&<Alert><AlertDescription>No persistent storage sources were detected.</AlertDescription></Alert>}</div>;
}

export function DatabasesTab({ projectId }: { projectId: string }) {
  const query=useBlueprint(projectId);if(query.isLoading)return <Skeleton className="h-52"/>;const findings=query.data?.findings.filter(f=>f.kind==="database")??[];
  if(!findings.length)return <Alert><Database className="size-4"/><AlertDescription>No active databases were identified. Re-analyze the project after storage changes.</AlertDescription></Alert>;
  return <div className="grid gap-3 lg:grid-cols-2">{findings.map(f=><Card key={f.id}><CardHeader><div className="flex items-start justify-between gap-2"><CardTitle className="capitalize">{f.technology}</CardTitle><Badge variant="outline">{f.confidence}</Badge></div>{f.service&&<p className="font-mono text-xs text-muted-foreground">{f.service}</p>}</CardHeader><CardContent className="space-y-3"><p className="text-sm leading-6">{f.recommendation}</p><Badge variant="secondary">{f.consistency}</Badge><details className="text-xs text-muted-foreground"><summary className="cursor-pointer">Detection evidence</summary><ul className="mt-2 space-y-1">{f.evidence.map(e=><li key={`${e.source}:${e.subject}:${e.detail}`}>{e.detail}</li>)}</ul></details>{f.warnings?.map(w=><p key={w} className="text-xs text-warning">{w}</p>)}</CardContent></Card>)}</div>;
}

export function SnapshotsTab({ projectId }: { projectId: string }) {
  const query=useQuery({queryKey:["backups",100],queryFn:()=>api.listBackupRuns(100)});if(query.isLoading)return <Skeleton className="h-52"/>;const runs=(query.data??[]).filter(r=>r.projectId===projectId);const snapshots=runs.filter(r=>r.snapshot).map(r=>({run:r,snapshot:r.snapshot!}));
  return <div className="space-y-4"><div className="grid gap-3 sm:grid-cols-3"><Summary icon={Camera} label="Verified snapshots" value={snapshots.length}/><Summary icon={CheckCircle2} label="Successful runs" value={runs.filter(r=>r.status==="completed"||r.status==="completed_with_warnings").length}/><Summary icon={AlertTriangle} label="Failed runs" value={runs.filter(r=>r.status==="failed").length}/></div>{snapshots.length?<div className="space-y-2">{snapshots.map(({run,snapshot})=><Card key={snapshot.id}><CardContent className="flex flex-wrap items-center justify-between gap-3 py-4"><div><div className="flex items-center gap-2"><span className="font-medium"><Timestamp iso={snapshot.createdAt}/></span><Badge variant={run.status==="completed"?"secondary":"outline"}>{run.status.replaceAll("_"," ")}</Badge></div><p className="mt-1 text-xs text-muted-foreground">{run.repositoryName} · {snapshot.filesCount} files · {formatBytes(snapshot.sizeBytes)}</p></div><code className="text-xs text-muted-foreground">{snapshot.resticSnapshotId.slice(0,12)}</code></CardContent></Card>)}</div>:<Alert><AlertDescription>No verified snapshots exist for this project yet.</AlertDescription></Alert>}</div>;
}

export function ProjectActivityTab({ projectId }: { projectId: string }) {
  const query=useQuery({queryKey:["audit",200],queryFn:()=>api.listAudit(200)});if(query.isLoading)return <Skeleton className="h-52"/>;const events=(query.data??[]).filter(e=>belongsToProject(e,projectId));
  return <Card><CardContent className="p-0">{events.length?<ul className="divide-y">{events.map(event=>{const d=describeEvent(event),detail=eventDetail(event),Icon=d.icon;return <li key={event.id} className="flex items-center gap-3 px-4 py-3"><Icon className={cn("size-4",TONE_CLASSES[d.tone])}/><div className="min-w-0 flex-1"><div className="font-medium">{d.label}</div>{detail&&<div className="truncate text-xs text-muted-foreground">{detail}</div>}</div><Timestamp iso={event.createdAt} className="text-xs text-muted-foreground"/></li>})}</ul>:<div className="p-8 text-center text-sm text-muted-foreground">No project-specific activity recorded yet.</div>}</CardContent></Card>;
}

export function ProjectSettingsTab({ project }: { project: ProjectDetail }) {
  const [open,setOpen]=useState(false),[confirmation,setConfirmation]=useState("");const navigate=useNavigate(),client=useQueryClient();const remove=useMutation({mutationFn:()=>api.removeProject(project.id),onSuccess:()=>{client.invalidateQueries({queryKey:["projects"]});navigate("/projects");toast.success("Project removed from Back-Orbit.")},onError:()=>toast.error("Project could not be removed")});
  return <div className="space-y-4"><InfoCard icon={Settings2} title="Registration"><dl className="grid gap-4 sm:grid-cols-2"><Definition label="Compose project" value={project.name}/><Definition label="Source" value={project.source}/><Definition label="Project path" value={project.composePath||"Not recorded"}/><Definition label="Project ID" value={project.id}/></dl></InfoCard><Card className="border-destructive/30"><CardHeader><CardTitle className="text-base text-destructive">Remove project</CardTitle></CardHeader><CardContent><p className="max-w-2xl text-sm text-muted-foreground">Remove this registration and its analysis from Back-Orbit. Containers, volumes, source files, repositories, backup history, and snapshots are not deleted.</p><Button variant="destructive" className="mt-4" onClick={()=>setOpen(true)}><Trash2 className="size-4"/>Remove from Back-Orbit</Button></CardContent></Card><Dialog open={open} onOpenChange={setOpen}><DialogContent><DialogHeader><DialogTitle>Remove {project.name}?</DialogTitle><DialogDescription>This only removes the project registration. Type the project name to confirm.</DialogDescription></DialogHeader><div className="space-y-2"><Label htmlFor="confirm-project-name">Project name</Label><Input id="confirm-project-name" value={confirmation} onChange={e=>setConfirmation(e.target.value)} autoComplete="off"/></div><DialogFooter><DialogClose render={<Button variant="outline"/>}>Cancel</DialogClose><Button variant="destructive" disabled={confirmation!==project.name||remove.isPending} onClick={()=>remove.mutate()}>{remove.isPending?"Removing…":"Remove project"}</Button></DialogFooter></DialogContent></Dialog></div>;
}

function FindingList({findings}:{findings:BlueprintFinding[]}){return <ul className="divide-y">{findings.map(f=><li key={f.id} className="flex items-center gap-3 py-3"><FileCode2 className="size-4 text-muted-foreground"/><div><div className="font-medium capitalize">{f.technology.replaceAll("-"," ")}</div><div className="text-xs text-muted-foreground">{f.evidence[0]?.subject}</div></div></li>)}</ul>}
function InfoCard({icon:Icon,title,children,className}:{icon:typeof Database;title:string;children:ReactNode;className?:string}){return <Card className={className}><CardHeader><CardTitle className="flex items-center gap-2 text-base"><Icon className="size-4 text-muted-foreground"/>{title}</CardTitle></CardHeader><CardContent>{children}</CardContent></Card>}
function ItemList({items}:{items:string[]}){return <ul className="space-y-2">{items.map(item=><li key={item} className="break-all rounded-md bg-muted/50 px-3 py-2 font-mono text-xs">{item}</li>)}</ul>}
function Muted({children}:{children:ReactNode}){return <p className="text-sm text-muted-foreground">{children}</p>}
function Stat({label,value}:{label:string;value:number}){return <div><div className="text-2xl font-semibold">{value}</div><div className="text-xs text-muted-foreground">{label}</div></div>}
function Summary({icon:Icon,label,value}:{icon:typeof Database;label:string;value:number}){return <Card><CardContent className="flex items-center gap-3 py-4"><Icon className="size-4 text-muted-foreground"/><div><div className="text-xl font-semibold">{value}</div><div className="text-xs text-muted-foreground">{label}</div></div></CardContent></Card>}
function Definition({label,value}:{label:string;value:string}){return <div><dt className="text-xs text-muted-foreground">{label}</dt><dd className="mt-1 break-all font-mono text-xs">{value}</dd></div>}
function belongsToProject(event:AuditEvent,id:string){return event.targetType==="project"&&event.targetId===id||event.metadata?.projectId===id}
function formatBytes(bytes:number){if(bytes===0)return"0 B";const units=["B","KB","MB","GB","TB"],i=Math.min(Math.floor(Math.log(bytes)/Math.log(1024)),units.length-1);return`${(bytes/1024**i).toFixed(i?1:0)} ${units[i]}`}
