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

export type BackupSourceKind = "volume" | "bind";

/**
 * Something in a project that holds data. Bind mounts are included alongside
 * named volumes, and sources that will not be backed up carry the reason
 * rather than being left out — an omission reads as "nothing to worry about".
 */
export interface BackupSource {
  kind: BackupSourceKind;
  name: string;
  mountedAt: string;
  services: string[];
  skipped?: string;
}

export interface ProjectDetail extends ProjectRecord {
  dockerAvailable: boolean;
  containers: DockerContainer[];
  volumes: DockerVolume[];
  networks: DockerNetwork[];
  dockerWarning?: string;
  sources: BackupSource[];
}

export type AnalyzerConfidence = "confirmed" | "probable" | "possible";

export interface BlueprintEvidence {
  source: string;
  subject: string;
  detail: string;
}

export interface BlueprintFinding {
  id: string;
  kind: "database" | "storage" | "secret" | "configuration" | string;
  technology: string;
  service?: string;
  confidence: AnalyzerConfidence;
  evidence: BlueprintEvidence[];
  recommendation: string;
  consistency: "crash-consistent" | "filesystem-consistent" | "application-consistent" | string;
  warnings?: string[];
}

export interface ProtectionBlueprint {
  schemaVersion: number;
  projectId: string;
  fingerprint: string;
  analyzedAt: string;
  confirmedAt?: string;
  drifted: boolean;
  findings: BlueprintFinding[];
  steps: { order: number; action: string; description: string }[];
  warnings: string[];
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

export interface SecretStoreStatus {
  initialized: boolean;
  unlocked: boolean;
  /** Whether a master key file is configured, i.e. whether the store unlocks
   *  itself after a restart. Without it, scheduled backups stop until someone
   *  unlocks it by hand. */
  unattendedUnlockConfigured: boolean;
}

/** Metadata only — the API has no shape that carries a secret's value. */
export interface SecretMetadata {
  id: string;
  kind: string;
  name: string;
  keyVersion: number;
  createdAt: string;
  updatedAt: string;
}

export type RepositoryKind = "local" | "sftp" | "s3";
export type RepositoryStatus = "unknown" | "uninitialized" | "ready" | "error";

export interface Repository {
  id: string;
  name: string;
  kind: RepositoryKind;
  location: string;
  status: RepositoryStatus;
  lastError?: string;
  lastCheckedAt?: string;
  createdAt: string;
  updatedAt: string;
}

/**
 * A directory this installation can store a local repository in. `writable`
 * is probed by the server rather than assumed, so the UI can offer a path that
 * is known to work instead of one that merely looks plausible.
 */
export interface RepositoryLocation {
  path: string;
  label: string;
  description: string;
  writable: boolean;
  detail?: string;
  recommended: boolean;
}


export type BackupRunStatus =
  | "running"
  | "completed"
  | "completed_with_warnings"
  | "failed"
  | "cancelled";

export type BackupPhase = "preparing" | "staging" | "snapshotting" | "verifying" | "finished";

/**
 * What was actually checked after a snapshot was written. Kept as a list
 * rather than a boolean so the UI can say what "verified" covered instead of
 * implying more was checked than was.
 */
export interface BackupVerification {
  ok: boolean;
  checks?: string[];
  errors?: string[];
  filesListed: number;
  bytesInSnapshot: number;
  durationMs: number;
}

export interface OwnershipEntry {
  path: string;
  uid: number;
  gid: number;
  mode: number;
}

export interface SQLiteCapture {
  path: string;
  method: string;
  bytes: number;
}

export interface VolumeManifest {
  name: string;
  kind: string;
  mountedAt?: string;
  sqliteDatabases?: SQLiteCapture[];
  pathInSnapshot: string;
  files: number;
  bytes: number;
  ownership: OwnershipEntry[];
  ownershipPreserved: boolean;
  warnings?: string[];
}

export interface BackupManifest {
  schemaVersion: number;
  project: string;
  createdAt: string;
  volumes: VolumeManifest[];
}

export interface BackupSnapshot {
  id: string;
  runId: string;
  repositoryId?: string;
  resticSnapshotId: string;
  manifest: BackupManifest;
  sizeBytes: number;
  filesCount: number;
  verifiedAt?: string;
  verification: BackupVerification;
  createdAt: string;
}

export interface BackupRun {
  id: string;
  projectId?: string;
  projectName: string;
  repositoryId?: string;
  repositoryName: string;
  trigger: string;
  status: BackupRunStatus;
  phase: BackupPhase;
  volumes: string[];
  filesTotal: number;
  bytesTotal: number;
  bytesAdded: number;
  warnings: string[];
  error?: string;
  startedAt: string;
  endedAt?: string;
  createdAt: string;
  /** Only present when the run produced a snapshot that verified. */
  snapshot?: BackupSnapshot;
}

export interface RepositoryCheckResult {
  status: RepositoryStatus;
  snapshotCount: number;
  error?: string;
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
  getProjectBlueprint: (id: string) =>
    request<ProtectionBlueprint>(`/api/v1/projects/${id}/blueprint`),
  analyzeProject: (id: string) =>
    request<ProtectionBlueprint>(`/api/v1/projects/${id}/analyze`, { method: "POST" }),
  confirmProjectBlueprint: (id: string) =>
    request<ProtectionBlueprint>(`/api/v1/projects/${id}/blueprint/confirm`, { method: "POST" }),
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

  secretStoreStatus: () => request<SecretStoreStatus>("/api/v1/secrets/status"),
  initializeSecretStore: (passphrase: string) =>
    request<void>("/api/v1/secrets/initialize", {
      method: "POST",
      body: JSON.stringify({ passphrase }),
    }),
  unlockSecretStore: (passphrase: string) =>
    request<void>("/api/v1/secrets/unlock", {
      method: "POST",
      body: JSON.stringify({ passphrase }),
    }),
  lockSecretStore: () => request<void>("/api/v1/secrets/lock", { method: "POST" }),

  startBackup: (projectId: string, repositoryId: string) =>
    request<BackupRun>(`/api/v1/projects/${projectId}/backup`, {
      method: "POST",
      body: JSON.stringify({ repositoryId }),
    }),
  listBackupRuns: (limit = 25) => request<BackupRun[]>(`/api/v1/backups?limit=${limit}`),
  getBackupRun: (id: string) => request<BackupRun>(`/api/v1/backups/${id}`),
  cancelBackupRun: (id: string) =>
    request<void>(`/api/v1/backups/${id}/cancel`, { method: "POST" }),

  listRepositories: () => request<Repository[]>("/api/v1/repositories"),
  repositoryLocations: () =>
    request<RepositoryLocation[]>("/api/v1/repositories/locations"),
  createRepository: (input: {
    name: string;
    kind: RepositoryKind;
    location: string;
    password: string;
  }) =>
    request<Repository>("/api/v1/repositories", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  // deleteData erases the snapshots as well. It is only ever sent when the
  // operator asked for it in the confirmation dialog; the server treats any
  // other value as "keep the data".
  deleteRepository: (id: string, deleteData = false) =>
    request<void>(
      `/api/v1/repositories/${id}${deleteData ? "?deleteData=true" : ""}`,
      { method: "DELETE" },
    ),
  checkRepository: (id: string) =>
    request<RepositoryCheckResult>(`/api/v1/repositories/${id}/check`, { method: "POST" }),
  initializeRepository: (id: string) =>
    request<void>(`/api/v1/repositories/${id}/initialize`, { method: "POST" }),
};
