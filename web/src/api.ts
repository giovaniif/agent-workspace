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

export function apiVersion(h: Hello): string {
  const v = String(h.api);
  return v.startsWith("v") ? v : "v" + v;
}
