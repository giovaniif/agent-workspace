export const cachePrefix = "agentws-shell-";

export function cacheName(build: string): string {
  return cachePrefix + build;
}

export function buildFromWorkerLocation(href: string): string {
  return new URL(href).searchParams.get("build") || "dev";
}

export function staleCaches(names: string[], current: string): string[] {
  return names.filter((name) => name.startsWith(cachePrefix) && name !== current);
}

export type RequestKind = "shell" | "file" | "network";

export function requestKind(method: string, mode: string, url: string, origin: string): RequestKind {
  const u = new URL(url);
  if (method !== "GET" || u.origin !== origin || u.pathname === "/api" || u.pathname.startsWith("/api/")) {
    return "network";
  }
  if (mode === "navigate") {
    return "shell";
  }
  if (u.pathname === "/sw.js") {
    return "network";
  }
  return "file";
}

export function precacheList(files: string[]): string[] {
  return ["/", ...files.filter((f) => f !== "/")];
}
