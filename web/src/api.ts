import type { Message } from "./stream";

export type Hello = {
  api: string;
  build: string;
};

export type Device = {
  id: string;
  name: string;
  created_at: string;
  last_seen: string;
};

export type Paired = {
  device: Device;
  token: string;
};

export type Fetch = (input: string, init?: RequestInit) => Promise<Response>;

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

const codeByStatus: Record<number, string> = {
  400: "bad_request",
  401: "unauthorized",
  404: "not_found",
  429: "rate_limited",
};

function errorFields(body: unknown): { code?: string; message?: string } {
  if (typeof body !== "object" || body === null) {
    return {};
  }
  const record = body as Record<string, unknown>;
  const nested = record.error;
  if (typeof nested === "object" && nested !== null) {
    return errorFields(nested);
  }
  return {
    code: typeof record.code === "string" ? record.code : undefined,
    message: typeof record.message === "string" ? record.message : typeof nested === "string" ? nested : undefined,
  };
}

async function request<T>(fetchFn: Fetch, path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetchFn(path, { cache: "no-store", ...init });
  } catch (err) {
    throw new ApiError(0, "network", err instanceof Error ? err.message : String(err));
  }
  const text = await res.text();
  let body: unknown = undefined;
  try {
    body = text ? JSON.parse(text) : undefined;
  } catch {
    body = undefined;
  }
  if (!res.ok) {
    const fields = errorFields(body);
    const code = fields.code ?? codeByStatus[res.status] ?? "failed";
    throw new ApiError(res.status, code, fields.message ?? (text || res.statusText));
  }
  return body as T;
}

export function hello(fetchFn: Fetch): Promise<Hello> {
  return request<Hello>(fetchFn, "/api/v1/hello");
}

export function pair(fetchFn: Fetch, code: string, name: string): Promise<Paired> {
  return request<Paired>(fetchFn, "/api/v1/pair", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ code, name }),
  });
}

export type Authed = { fetch: Fetch; token: string };

export type MessagesPage = { messages: Message[]; before: number };

export type Sent = { id: string; queued: boolean };

function sessionPath(session: string, rest: string): string {
  return "/api/v1/sessions/" + encodeURIComponent(session) + rest;
}

function authedRequest<T>(a: Authed, path: string, method = "GET", body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Authorization: "Bearer " + a.token };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  return request<T>(a.fetch, path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
}

export function messagesPage(a: Authed, session: string, before?: number, limit = 50): Promise<MessagesPage> {
  const query = (before ? "before=" + before + "&" : "") + "limit=" + limit;
  return authedRequest<MessagesPage>(a, sessionPath(session, "/messages?" + query));
}

export function sendMessage(a: Authed, session: string, text: string): Promise<Sent> {
  return authedRequest<Sent>(a, sessionPath(session, "/messages"), "POST", { text });
}

export async function unsend(a: Authed, session: string, id: string): Promise<void> {
  await authedRequest<unknown>(a, sessionPath(session, "/sends/" + encodeURIComponent(id)), "DELETE");
}

export async function interrupt(a: Authed, session: string): Promise<void> {
  await authedRequest<unknown>(a, sessionPath(session, "/interrupt"), "POST");
}

export type PromptChoice = {
  id: string;
  label: string;
};

export type Prompt = {
  id?: string;
  text: string;
  choices: PromptChoice[];
  raw?: string;
};

export function getPrompt(a: Authed, session: string): Promise<Prompt> {
  return authedRequest<Prompt>(a, sessionPath(session, "/prompt"));
}

export async function answerPrompt(a: Authed, session: string, choice: string, prompt?: string): Promise<void> {
  await authedRequest<unknown>(a, sessionPath(session, "/answer"), "POST", prompt ? { choice, prompt } : { choice });
}

export type PushKey = {
  public_key: string;
};

export type PushSubscriptionBody = {
  endpoint?: string;
  expirationTime?: number | null;
  keys?: Record<string, string>;
};

function authorized(token: string, init: RequestInit = {}): RequestInit {
  return { ...init, headers: { ...(init.headers as Record<string, string> | undefined), Authorization: "Bearer " + token } };
}

export function pushKey(fetchFn: Fetch, token: string): Promise<PushKey> {
  return request<PushKey>(fetchFn, "/api/v1/push/key", authorized(token));
}

export function pushSubscribe(fetchFn: Fetch, token: string, subscription: PushSubscriptionBody): Promise<unknown> {
  return request<unknown>(
    fetchFn,
    "/api/v1/push/subscribe",
    authorized(token, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(subscription),
    }),
  );
}

export function pushUnsubscribe(fetchFn: Fetch, token: string): Promise<unknown> {
  return request<unknown>(fetchFn, "/api/v1/push/unsubscribe", authorized(token, { method: "POST" }));
}

export function apiVersion(h: Hello): string {
  const v = String(h.api);
  return v.startsWith("v") ? v : "v" + v;
}

export type WorkspaceInfo = {
  Root: string;
  Kind: string;
  LastUsed: string;
};

export type WorkspaceList = {
  workspaces: WorkspaceInfo[];
  last_used: string;
};

export type WorkItemResolved = {
  source: string;
  ref?: string;
  title?: string;
  worktree: string;
  workspace: string;
};

export type NewSessionRequest = {
  workspace?: string;
  work_item: string;
  harness: string;
  model?: string;
  effort?: string;
  prompt?: string;
};

function authed(token: string, init: RequestInit = {}): RequestInit {
  const headers: Record<string, string> = { Authorization: "Bearer " + token };
  if (init.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  return { ...init, headers };
}

export function listWorkspaces(fetchFn: Fetch, token: string): Promise<WorkspaceList> {
  return request<WorkspaceList>(fetchFn, "/api/v1/workspaces", authed(token));
}

export function resolveWorkItem(fetchFn: Fetch, token: string, workspace: string, item: string): Promise<WorkItemResolved> {
  const query = new URLSearchParams({ workspace, item }).toString();
  return request<WorkItemResolved>(fetchFn, "/api/v1/work-items/resolve?" + query, authed(token));
}

export function startSession(fetchFn: Fetch, token: string, body: NewSessionRequest): Promise<{ ID: string }> {
  return request<{ ID: string }>(fetchFn, "/api/v1/sessions", authed(token, { method: "POST", body: JSON.stringify(body) }));
}

export async function tokenRefused(fetchFn: Fetch, token: string): Promise<boolean> {
  try {
    await listWorkspaces(fetchFn, token);
    return false;
  } catch (err) {
    return err instanceof ApiError && err.status === 401 && err.code === "unauthorized";
  }
}
