import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { ChevronRight, FolderKanban, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { StatusBadge } from "@/components/StatusBadge";
import { DockerUnreachableAlert } from "@/components/DockerStatus";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { Timestamp } from "@/components/Timestamp";
import { usePageTitle } from "@/hooks/usePageTitle";
import { api, ApiError, type ProjectRecord } from "@/lib/api";

const registerSchema = z.object({
  name: z.string().min(1, "Required").max(128),
  path: z.string().min(1, "Required").refine((v) => v.startsWith("/"), {
    message: "Must be an absolute path (e.g. /srv/myproject)",
  }),
});
type RegisterFormValues = z.infer<typeof registerSchema>;

const columnHelper = createColumnHelper<ProjectRecord>();

const columns = [
  columnHelper.accessor("name", {
    header: "Name",
    cell: (info) => <span className="font-medium">{info.getValue()}</span>,
  }),
  columnHelper.accessor("status", {
    header: "Status",
    cell: (info) => <StatusBadge status={info.getValue()} />,
  }),
  columnHelper.accessor("source", {
    header: "Source",
    cell: (info) => (
      <span className="capitalize text-muted-foreground">{info.getValue()}</span>
    ),
  }),
  columnHelper.accessor("composePath", {
    header: "Compose path",
    cell: (info) => (
      // Paths are often long; truncate with the full value on hover so one
      // deep path cannot push the rest of the row out of view.
      <span
        title={info.getValue() || undefined}
        className="block max-w-[22rem] truncate font-mono text-xs text-muted-foreground"
      >
        {info.getValue() || "—"}
      </span>
    ),
  }),
  columnHelper.accessor("updatedAt", {
    header: "Updated",
    cell: (info) => (
      <Timestamp iso={info.getValue()} className="text-sm text-muted-foreground" />
    ),
  }),
  columnHelper.display({
    id: "go",
    header: "",
    cell: () => (
      <ChevronRight className="size-4 text-muted-foreground/50" aria-hidden="true" />
    ),
  }),
];

export function ProjectsPage() {
  usePageTitle("Projects");

  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [dialogOpen, setDialogOpen] = useState(false);

  const projectsQuery = useQuery({ queryKey: ["projects"], queryFn: api.listProjects });

  const scanMutation = useMutation({
    mutationFn: api.scanProjects,
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      if (result.warning) {
        toast.warning(result.warning);
      } else {
        toast.success(`Scan complete — ${result.projects.length} project(s) known.`);
      }
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Scan failed.");
    },
  });

  const registerForm = useForm<RegisterFormValues>({ resolver: zodResolver(registerSchema) });
  const registerMutation = useMutation({
    mutationFn: (values: RegisterFormValues) => api.registerProject(values.name, values.path),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      toast.success("Project registered.");
      setDialogOpen(false);
      registerForm.reset();
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Failed to register project.");
    },
  });

  const table = useReactTable({
    data: projectsQuery.data ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <div className="space-y-6">
      <PageHeader
        title="Projects"
        description="Docker Compose projects Back-Orbit has discovered or that were registered manually."
        actions={
          <>
            <Button
              variant="outline"
              onClick={() => scanMutation.mutate()}
              disabled={scanMutation.isPending}
            >
              <RefreshCw className={scanMutation.isPending ? "size-4 animate-spin" : "size-4"} />
              Scan for projects
            </Button>
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button />}>
              <Plus className="size-4" />
              Register project
            </DialogTrigger>
            <DialogContent>
              <form
                onSubmit={registerForm.handleSubmit((values) => registerMutation.mutate(values))}
                noValidate
              >
                <DialogHeader>
                  <DialogTitle>Register a project</DialogTitle>
                  <DialogDescription>
                    Track a Compose project by its directory on this host, even if it isn't
                    currently running.
                  </DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label htmlFor="project-name">Name</Label>
                    <Input id="project-name" {...registerForm.register("name")} />
                    {registerForm.formState.errors.name && (
                      <p className="text-sm text-destructive">
                        {registerForm.formState.errors.name.message}
                      </p>
                    )}
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="project-path">Project directory</Label>
                    <Input
                      id="project-path"
                      placeholder="/srv/myproject"
                      {...registerForm.register("path")}
                    />
                    {registerForm.formState.errors.path && (
                      <p className="text-sm text-destructive">
                        {registerForm.formState.errors.path.message}
                      </p>
                    )}
                  </div>
                </div>
                <DialogFooter>
                  <Button type="submit" disabled={registerMutation.isPending}>
                    {registerMutation.isPending ? "Registering…" : "Register"}
                  </Button>
                </DialogFooter>
              </form>
            </DialogContent>
          </Dialog>
          </>
        }
      />

      <DockerUnreachableAlert />

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id}>
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {projectsQuery.isLoading ? (
              Array.from({ length: 3 }).map((_, i) => (
                <TableRow key={i}>
                  {columns.map((_, j) => (
                    <TableCell key={j}>
                      <Skeleton className="h-5 w-full" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : table.getRowModel().rows.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <EmptyState
                    icon={FolderKanban}
                    title="No projects yet"
                    description="Scan to discover Compose projects running on this host, or register a project directory manually."
                    action={
                      <Button
                        variant="outline"
                        onClick={() => scanMutation.mutate()}
                        disabled={scanMutation.isPending}
                      >
                        <RefreshCw
                          className={scanMutation.isPending ? "size-4 animate-spin" : "size-4"}
                        />
                        Scan for projects
                      </Button>
                    }
                  />
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                // The whole row navigates, not just the name: a 5-column row
                // whose only target was one word made the table feel inert.
                <TableRow
                  key={row.id}
                  onClick={() => navigate(`/projects/${row.original.id}`)}
                  className="cursor-pointer"
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
