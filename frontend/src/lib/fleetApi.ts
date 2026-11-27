// Client for /api/fleet/*.

import type {
  FleetAuditEntry,
  FleetCosts,
  FleetDiff,
  FleetDigest,
  FleetDispatchRequest,
  FleetErrorBody,
  FleetEventList,
  FleetEpicReply,
  FleetEpicRequest,
  FleetGitHubStatus,
  FleetIssue,
  FleetMilestone,
  FleetProject,
  FleetProjectRequest,
  FleetTask,
  FleetTaskAction,
  FleetDelivery,
  FleetVerification,
  FleetWorktree,
} from "@/types/fleet";

const TOKEN_KEY = "alice-api-token";

/** The API token for commands, kept in this browser only. */
export function getApiToken(): string {
  if (typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem(TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

export function setApiToken(token: string): void {
  if (typeof window === "undefined") return;
  try {
    if (token) window.localStorage.setItem(TOKEN_KEY, token);
    else window.localStorage.removeItem(TOKEN_KEY);
  } catch {
    // Storage is unavailable; the token lasts for this page only.
  }
}

/** A refused or failed fleet request, with the server's code and message. */
export class FleetApiError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "FleetApiError";
    this.status = status;
    this.code = code;
  }

  /** The server wants an API token, or the one sent is wrong. */
  get unauthorized(): boolean {
    return this.status === 401;
  }

  /** The fleet is switched off in alice's configuration. */
  get disabled(): boolean {
    return this.code === "disabled";
  }
}

async function request<T>(method: string, url: string, body?: unknown, isCommand = false): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (isCommand) {
    const token = getApiToken();
    if (token) headers.Authorization = `Bearer ${token}`;
  }
  let res: Response;
  try {
    res = await fetch(url, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  } catch (err) {
    throw new FleetApiError(0, "network", err instanceof Error ? err.message : "network error");
  }
  let parsed: unknown = null;
  try {
    parsed = await res.json();
  } catch {
    // A proxy or a crash answered with something that is not JSON.
  }
  if (!res.ok) {
    const e = (parsed ?? {}) as Partial<FleetErrorBody>;
    throw new FleetApiError(res.status, e.code || "error", e.error || `HTTP ${res.status}`);
  }
  return parsed as T;
}

function query(params: Record<string, string | number | undefined>): string {
  const qs = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") qs.set(key, String(value));
  }
  const s = qs.toString();
  return s ? `?${s}` : "";
}

const enc = encodeURIComponent;

export interface FleetBackendUsage {
  backend: string;
  input_tokens: string;
  output_tokens: string;
  cost_usd: number;
  tasks: string;
}

export interface FleetBackendQuota {
  backend: string;
  blocked: boolean;
  estimated_remaining?: number;
  provider_remaining?: number;
  resets_at?: string;
}

export const fleetApi = {
  getDigest: () => request<FleetDigest>("GET", "/api/fleet/digest"),

  listProjects: async () =>
    (await request<{ projects?: FleetProject[] }>("GET", "/api/fleet/projects")).projects ?? [],
  getProject: (id: string) => request<FleetProject>("GET", `/api/fleet/projects/${enc(id)}`),
  registerProject: (req: FleetProjectRequest) =>
    request<FleetProject>("POST", "/api/fleet/projects", req, true),
  updateProject: (id: string, req: FleetProjectRequest) =>
    request<FleetProject>("PUT", `/api/fleet/projects/${enc(id)}`, req, true),

  listTasks: async (params: { project?: string; status?: string[]; limit?: number } = {}) =>
    (
      await request<{ tasks?: FleetTask[] }>(
        "GET",
        `/api/fleet/tasks${query({ project: params.project, status: params.status?.join(","), limit: params.limit })}`,
      )
    ).tasks ?? [],
  getTask: (id: string) => request<FleetTask>("GET", `/api/fleet/tasks/${enc(id)}`),
  dispatch: (req: FleetDispatchRequest) => request<FleetTask>("POST", "/api/fleet/tasks", req, true),
  /** Creates an epic issue on GitHub. No task is dispatched. */
  createEpic: (req: FleetEpicRequest) => request<FleetEpicReply>("POST", "/api/fleet/epics", req, true),
  getProjectGitHub: (id: string) => request<FleetGitHubStatus>("GET", `/api/fleet/projects/${enc(id)}/github`),
  act: (id: string, action: FleetTaskAction, body?: { text?: string; ignore_budget?: boolean; reason?: string }) =>
    request<FleetTask>("POST", `/api/fleet/tasks/${enc(id)}/${action}`, body ?? {}, true),

  getTaskEvents: (id: string, params: { afterId?: number; limit?: number; types?: string[] } = {}) =>
    request<FleetEventList>(
      "GET",
      `/api/fleet/tasks/${enc(id)}/events${query({ after_id: params.afterId, limit: params.limit, types: params.types?.join(",") })}`,
    ),
  /** The task's most recent event of one type, e.g. the last thing its agent said. */
  getLastTaskEvent: async (id: string, type: string) =>
    (await request<FleetEventList>("GET", `/api/fleet/tasks/${enc(id)}/events${query({ last_of: type })}`)).events?.[0] ?? null,
  getEvents: (params: { afterId?: number; limit?: number; types?: string[] } = {}) =>
    request<FleetEventList>(
      "GET",
      `/api/fleet/events${query({ after_id: params.afterId, limit: params.limit, types: params.types?.join(",") })}`,
    ),
  getTaskDiff: (id: string) => request<FleetDiff>("GET", `/api/fleet/tasks/${enc(id)}/diff`),
  getTaskDelivery: (id: string) => request<FleetDelivery>("GET", `/api/fleet/tasks/${enc(id)}/delivery`),
  /** Per-item acceptance commands; each returns the refreshed delivery summary. */
  decideAcceptance: (id: string, item_id: string, decision: string, note: string) =>
    request<FleetDelivery>("POST", `/api/fleet/tasks/${enc(id)}/acceptance/decide`, { item_id, decision, note }, true),
  enterAcceptance: (
    id: string,
    body: {
      criteria?: { id: string; text: string }[];
      items: { id: string; status: string; evidence?: { type: string; ref: string; note?: string }[]; blocker?: { missing: string; owner: string }; note?: string }[];
    },
  ) => request<FleetDelivery>("POST", `/api/fleet/tasks/${enc(id)}/acceptance/entry`, body, true),
  acceptanceCriteria: (id: string, action: "refresh" | "apply" | "dismiss") =>
    request<FleetDelivery>("POST", `/api/fleet/tasks/${enc(id)}/acceptance/criteria`, { action }, true),
  requestEvidence: (id: string) =>
    request<FleetDelivery>("POST", `/api/fleet/tasks/${enc(id)}/acceptance/request-evidence`, {}, true),
  getTaskVerifications: async (id: string) =>
    (await request<{ verifications?: FleetVerification[] }>("GET", `/api/fleet/tasks/${enc(id)}/verifications`))
      .verifications ?? [],

  listProjectIssues: async (id: string) =>
    (await request<{ issues?: FleetIssue[] }>("GET", `/api/fleet/projects/${enc(id)}/issues`)).issues ?? [],
  listProjectMilestones: async (id: string) =>
    (await request<{ milestones?: FleetMilestone[] }>("GET", `/api/fleet/projects/${enc(id)}/milestones`)).milestones ?? [],
  listProjectWorktrees: async (id: string) =>
    (await request<{ worktrees?: FleetWorktree[] }>("GET", `/api/fleet/projects/${enc(id)}/worktrees`)).worktrees ?? [],
  /** Runs the project's saved verify commands once and returns each outcome. */
  tryVerify: async (id: string) =>
    (await request<{ verifications?: FleetVerification[] }>("POST", `/api/fleet/projects/${enc(id)}/verify`, {}, true))
      .verifications ?? [],
  getBackendUsage: (since: string, until: string) =>
    request<{ backends: FleetBackendUsage[]; quota?: FleetBackendQuota[] }>("GET", `/api/fleet/usage/backends${query({ since, until })}`),
  getCosts: (days = 7) => request<FleetCosts>("GET", `/api/costs/fleet${query({ days })}`),

  getAudit: async (params: { project?: string; task?: string; actor?: string; limit?: number; beforeId?: number } = {}) =>
    (
      await request<{ entries?: FleetAuditEntry[] }>(
        "GET",
        `/api/fleet/audit${query({ project: params.project, task: params.task, actor: params.actor, limit: params.limit, before_id: params.beforeId })}`,
      )
    ).entries ?? [],
} as const;
