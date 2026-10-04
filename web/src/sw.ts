import { buildFromWorkerLocation, cacheName, precacheList, requestKind, staleCaches } from "./sw-cache";

declare const self: ServiceWorkerGlobalScope;

const shellFiles = "__AGENTWS_SHELL_FILES__" as unknown as string[];
const current = cacheName(buildFromWorkerLocation(self.location.href));

self.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(current);
      await cache.addAll(precacheList(shellFiles).map((path) => new Request(path, { cache: "reload" })));
      await self.skipWaiting();
    })(),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys();
      await Promise.all(staleCaches(names, current).map((name) => caches.delete(name)));
      await self.clients.claim();
    })(),
  );
});

async function fromCache(request: Request, key: RequestInfo): Promise<Response> {
  const cache = await caches.open(current);
  const hit = await cache.match(key);
  if (hit) {
    return hit;
  }
  const res = await fetch(request);
  if (res.ok && res.type === "basic") {
    await cache.put(key, res.clone());
  }
  return res;
}

self.addEventListener("fetch", (event) => {
  const { request } = event;
  const kind = requestKind(request.method, request.mode, request.url, self.location.origin);
  if (kind === "network") {
    return;
  }
  event.respondWith(fromCache(request, kind === "shell" ? "/" : request));
});
