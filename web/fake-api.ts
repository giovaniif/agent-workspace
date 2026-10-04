import type { IncomingMessage, ServerResponse } from "node:http";
import type { Plugin } from "vite";

export const fakePairCode = "ABCD2345";
export const fakeRateLimitedCode = "ZZZZZZZZ";

function send(res: ServerResponse, status: number, body: unknown) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(body));
}

function readJSON(req: IncomingMessage): Promise<Record<string, unknown>> {
  return new Promise((resolve) => {
    let raw = "";
    req.on("data", (chunk) => {
      raw += chunk;
    });
    req.on("end", () => {
      try {
        resolve(JSON.parse(raw || "{}"));
      } catch {
        resolve({});
      }
    });
  });
}

export function fakeApi(build = "v0.12.0+demo"): Plugin {
  return {
    name: "agentws-fake-api",
    configureServer(server) {
      server.middlewares.use(async (req, res, next) => {
        const path = (req.url ?? "").split("?")[0];
        if (req.method === "GET" && path === "/api/v1/hello") {
          send(res, 200, { api: "v1", build });
          return;
        }
        if (req.method === "POST" && path === "/api/v1/pair") {
          const body = await readJSON(req);
          const code = String(body.code ?? "");
          if (code === fakeRateLimitedCode) {
            send(res, 429, { error: { code: "rate_limited", message: "too many failed pairing tries; wait a minute" } });
            return;
          }
          if (code !== fakePairCode) {
            send(res, 401, { error: { code: "unauthorized", message: "the pairing code is wrong or has expired" } });
            return;
          }
          const now = new Date().toISOString();
          send(res, 200, {
            device: { id: "k3m9p2qx", name: String(body.name ?? "phone"), created_at: now, last_seen: now },
            token: "demo-token",
          });
          return;
        }
        if (path.startsWith("/api/")) {
          send(res, 404, { error: { code: "not_found", message: "not in the fake API" } });
          return;
        }
        next();
      });
    },
  };
}
