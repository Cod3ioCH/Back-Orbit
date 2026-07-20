const CSRF_COOKIE_NAME = "backorbit_csrf";
const CSRF_HEADER_NAME = "X-CSRF-Token";

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

function readCookie(name: string): string | undefined {
  const match = document.cookie
    .split("; ")
    .find((row) => row.startsWith(`${name}=`));
  return match?.split("=")[1];
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);

  if (init.body !== undefined) {
    headers.set("Content-Type", "application/json");
  }
  if (method !== "GET" && method !== "HEAD") {
    const csrfToken = readCookie(CSRF_COOKIE_NAME);
    if (csrfToken) {
      headers.set(CSRF_HEADER_NAME, csrfToken);
    }
  }

  const response = await fetch(path, { ...init, headers });

  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get("Content-Type") ?? "";
  const payload = contentType.includes("application/json")
    ? await response.json()
    : undefined;

  if (!response.ok) {
    const message =
      (payload && typeof payload === "object" && "error" in payload
        ? String((payload as { error: unknown }).error)
        : undefined) ?? `request failed with status ${response.status}`;
    throw new ApiError(response.status, message);
  }

  return payload as T;
}

export interface User {
  id: string;
  username: string;
  sessionExpiresAt: string;
}

export interface SetupStatus {
  setupComplete: boolean;
}

export interface DockerStatus {
  connected: boolean;
  host?: string;
  apiVersion?: string;
  serverVersion?: string;
  error?: string;
  securityNotice: string;
}

export interface DockerMount {
  type: string;
  name?: string;
  source: string;
  destination: string;
  readOnly: boolean;
}

export interface DockerContainer {
  id: string;
  name: string;
  service?: string;
  image: string;
  imageId: string;
  state: string;
  status: string;
  createdAt: string;
  labels?: Record<string, string>;
  mounts: DockerMount[];
}

export interface DockerVolume {
  name: string;
  driver: string;
  mountpoint: string;
  labels?: Record<string, string>;
}

export interface DockerNetwork {
  id: string;
  name: string;
  driver: string;
  labels?: Record<string, string>;
}

export type ProjectSource = "discovered" | "registered";
export type ProjectStatus =
  | "healthy"
  | "warning"
  | "failed"
  | "running"
  | "paused"
  | "unprotected";

export interface ProjectRecord {
  id: string;
  name: string;
  composePath: string;
  composeFiles: string[];
  source: ProjectSource;
  status: ProjectStatus;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectDetail extends ProjectRecord {
  dockerAvailable: boolean;
  containers: DockerContainer[];
  volumes: DockerVolume[];
  networks: DockerNetwork[];
  dockerWarning?: string;
}

export interface AuditEvent {
  id: string;
  action: string;
  actorUserId?: string;
  targetType?: string;
  targetId?: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
}

export const api = {
  setupStatus: () => request<SetupStatus>("/api/v1/setup/status"),
  setupAdmin: (username: string, password: string) =>
    request<User>("/api/v1/setup/admin", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),

  login: (username: string, password: string) =>
    request<User>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<void>("/api/v1/auth/logout", { method: "POST" }),
  session: () => request<User>("/api/v1/auth/session"),

  dockerStatus: () => request<DockerStatus>("/api/v1/docker/status"),

  listProjects: () => request<ProjectRecord[]>("/api/v1/projects"),
  getProject: (id: string) => request<ProjectDetail>(`/api/v1/projects/${id}`),
  registerProject: (name: string, path: string) =>
    request<ProjectRecord>("/api/v1/projects", {
      method: "POST",
      body: JSON.stringify({ name, path }),
    }),
  scanProjects: () =>
    request<{ projects: ProjectRecord[]; warning?: string }>(
      "/api/v1/projects/scan",
      { method: "POST" },
    ),

  listAudit: (limit = 50) =>
    request<AuditEvent[]>(`/api/v1/audit?limit=${limit}`),
};
