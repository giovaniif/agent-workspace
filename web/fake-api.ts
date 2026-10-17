import { createHash } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import type { Duplex } from "node:stream";
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

const minute = 60000;

function fakeSession(id: string, over: Record<string, unknown>) {
  return {
    ID: id,
    TaskID: "t-" + id,
    Harness: "claude",
    Pane: "",
    Model: "",
    Effort: "",
    State: "idle",
    Ended: false,
    Unread: false,
    Focused: false,
    Muted: false,
    WorktreeIDs: null,
    ResumeID: "",
    Transcript: "",
    Dir: "",
    Usage: { ContextLeftPercent: 0, HasContext: false, LimitUsedPercent: 0 },
    Limits: null,
    LimitsAt: "0001-01-01T00:00:00Z",
    Switches: null,
    SwitchWarning: false,
    name: "",
    where: "",
    banner: "",
    since: null,
    ...over,
  };
}

export function fakeStreamState(now = Date.now()) {
  const ago = (minutes: number) => new Date(now - minutes * minute).toISOString();
  const quota = (harness: string, window: string, label: string, left: number, resetsIn: number) => ({
    Harness: harness,
    Window: window,
    LeftPercent: left,
    ResetsAt: resetsIn ? Math.floor((now + resetsIn * minute) / 1000) : 0,
    ReportedAt: ago(1),
    label,
    low: left < 20,
    stale_at: new Date(now + 14 * minute).toISOString(),
  });
  return {
    seq: 1,
    workspaces: [],
    tasks: [],
    worktrees: [],
    sessions: [
      fakeSession("s1", { State: "permission", name: "fix the login redirect", where: "api@login", banner: "needs permission: Bash: make test", since: ago(3) }),
      fakeSession("s2", { Harness: "codex", State: "waiting", name: "pick a cache key", where: "web@cache", banner: "asks: Which TTL should the cache use?", since: ago(12) }),
      fakeSession("s3", { State: "running", name: "add retries", where: "api@retries", since: ago(8) }),
      fakeSession("s4", { State: "done", Unread: true, name: "update the docs", where: "docs@setup", banner: "Rewrote the setup section. (6m40s)", since: ago(20) }),
      fakeSession("s5", { Harness: "codex", State: "done", name: "bump deps", where: "web@deps", banner: "Bumped vite and vitest; the tests pass. (3m2s)", since: ago(125) }),
      fakeSession("s6", { State: "idle", name: "explore the auth flow", where: "api@main", since: ago(26 * 60) }),
    ],
    limits: [quota("claude", "five_hour", "5h", 38, 130), quota("claude", "seven_day", "7d", 19, 3 * 24 * 60), quota("codex", "five_hour", "5h", 77, 200)],
    sends: [
      { id: "m1", session: "s3", text: "then add a test for the backoff", queued_at: ago(2) },
      { id: "m2", session: "s3", text: "and update the README", queued_at: ago(1) },
    ],
    mates: fakeMates(process.env.CREW_FAKE_MATES === "stopped"),
  };
}

function fakeMates(stopped: boolean) {
  const held = stopped ? { guard: "stopped" } : {};
  return {
    stopped,
    mates: [
      {
        name: "helper",
        state: "connected",
        ...held,
        work: ["s3"],
        threads: [
          { mate: "helper", team: "T1", channel: "C1", thread_ts: "1700.0002", session_id: "s3", url: "https://app.slack.com/client/T1/C1/thread/C1-1700.0002", session: "add retries", state: "running", ended: false },
          { mate: "helper", team: "T1", channel: "C1", thread_ts: "1700.0001", session_id: "s5", url: "https://app.slack.com/client/T1/C1/thread/C1-1700.0001", session: "bump deps", state: "done", ended: false },
        ],
      },
      { name: "reviewer", state: "connected", guard: stopped ? "stopped" : "rate_limit", work: [], threads: [] },
      { name: "webby", state: "paused", ...held, work: [], threads: [] },
    ],
  };
}

function fakeMessage(id: string, cursor: number, role: string, text: string, at: string, tool?: { name: string; summary: string; status: string }) {
  return { id, cursor, turn: "p1", role, text, ...(tool ? { tool } : {}), at };
}

export function fakeMessages(older: boolean, now = Date.now(), session = "") {
  const at = new Date(now - 5 * minute).toISOString();
  if (older) {
    return {
      messages: [
        fakeMessage("o1", 400, "user", "the client gives up on the first 503; add retries", at),
        fakeMessage("o2", 900, "assistant", "I'll start by finding where the client sends requests.", at),
        fakeMessage("o3", 1400, "tool", "", at, { name: "Grep", summary: "Grep http.NewRequest", status: "done" }),
        fakeMessage("o4", 2200, "tool", "", at, { name: "Read", summary: "Read internal/client/client.go", status: "done" }),
        fakeMessage("o5", 3000, "assistant", "`Do` sends once and returns the first error. I'll wrap it in a retry loop.", at),
      ],
      before: 0,
    };
  }
  return {
    messages: [
      fakeMessage("n1", 4300, "tool", "", at, { name: "Edit", summary: "Edit internal/client/client.go", status: "done" }),
      fakeMessage("n2", 5100, "tool", "ok  \tgithub.com/acme/api/internal/client\t0.41s\nok  \tgithub.com/acme/api/internal/server\t0.22s", at, { name: "Bash", summary: "Bash go test ./...", status: "done" }),
      fakeMessage("n3", 5600, "tool", "golangci-lint: errcheck: Error return value of `resp.Body.Close` is not checked", at, { name: "Bash", summary: "Bash make lint", status: "failed" }),
      fakeMessage("n4", 6400, "assistant", "The tests pass. Lint flagged one unchecked `Close`; fixing it now.\n\n- retries **3 times** on 502, 503 and 504\n- backs off 200 ms, doubling\n- honours `Retry-After`", at),
      fakeMessage("n5", 7000, "tool", "", at, { name: "Edit", summary: "Edit internal/client/client.go", status: "done" }),
      fakeMessage("n6", 7600, "tool", "", at, { name: "Bash", summary: "Bash make lint && go test ./...", status: "running" }),
      ...(session === "s4" ? [fakeMessage("n7", 8200, "tool", "", at, { name: "Bash", summary: "Bash crew browser screenshot", status: "done" })] : []),
    ],
    before: 4000,
  };
}

function wsFrame(text: string): Buffer {
  const payload = Buffer.from(text);
  const len = payload.length;
  const head = len < 126 ? Buffer.from([0x81, len]) : Buffer.from([0x81, 126, len >> 8, len & 255]);
  return Buffer.concat([head, payload]);
}

function wsClose(code: number): Buffer {
  return Buffer.from([0x88, 2, code >> 8, code & 255]);
}

let offlineServed = false;
let refusedServed = false;

function refusing(): boolean {
  const mode = process.env.CREW_FAKE_STREAM;
  return mode === "revoked" || mode === "late";
}

type FakeSend = { id: string; session: string; text: string; queued_at: string };

let fakeSends: FakeSend[] | null = null;
let fakeSeq = 1;
const liveSockets = new Set<Duplex>();

function publishSends() {
  fakeSeq++;
  const frame = wsFrame(JSON.stringify({ diff: { seq: fakeSeq, sends: fakeSends ?? [] } }));
  for (const socket of liveSockets) {
    socket.write(frame);
  }
}

function fakeStream(req: IncomingMessage, socket: Duplex) {
  const offline = process.env.CREW_FAKE_STREAM === "offline";
  if (offline && offlineServed) {
    socket.destroy();
    return;
  }
  const key = String(req.headers["sec-websocket-key"] ?? "");
  const accept = createHash("sha1").update(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest("base64");
  socket.write(["HTTP/1.1 101 Switching Protocols", "Upgrade: websocket", "Connection: Upgrade", "Sec-WebSocket-Accept: " + accept, "", ""].join("\r\n"));
  let authed = false;
  socket.on("data", (chunk: Buffer) => {
    if (authed || (chunk[0] & 0x0f) !== 1) {
      return;
    }
    authed = true;
    const state = fakeStreamState();
    fakeSends ??= state.sends;
    socket.write(wsFrame(JSON.stringify({ state: { ...state, seq: fakeSeq, sends: fakeSends } })));
    liveSockets.add(socket);
    if (offline) {
      offlineServed = true;
      setTimeout(() => socket.end(wsClose(1013)), 300);
    }
    if (refusing() && !refusedServed) {
      refusedServed = true;
      setTimeout(() => socket.end(wsClose(4401)), 300);
    }
  });
  socket.on("close", () => liveSockets.delete(socket));
  socket.on("error", () => undefined);
}

export function fakeApi(build = "v0.12.0+demo"): Plugin {
  return {
    name: "crew-fake-api",
    configureServer(server) {
      server.httpServer?.on("upgrade", (req: IncomingMessage, socket: Duplex) => {
        if ((req.url ?? "").split("?")[0] === "/api/v1/stream") {
          fakeStream(req, socket);
        }
      });
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
        const session = path.match(/^\/api\/v1\/sessions\/([^/]+)\/(messages|interrupt|prompt|answer|sends\/[^/]+)$/);
        if (session && req.method === "GET" && session[2] === "messages") {
          const older = new URL(req.url ?? "", "http://fake").searchParams.has("before");
          setTimeout(() => send(res, 200, fakeMessages(older, Date.now(), session[1])), older ? 5000 : 0);
          return;
        }
        if (session && req.method === "POST" && session[2] === "messages") {
          const body = await readJSON(req);
          const queued = { id: "m" + Date.now(), session: decodeURIComponent(session[1]), text: String(body.text ?? ""), queued_at: new Date().toISOString() };
          fakeSends = [...(fakeSends ?? []), queued];
          publishSends();
          send(res, 200, { id: queued.id, queued: true });
          return;
        }
        if (session && req.method === "DELETE" && session[2].startsWith("sends/")) {
          const id = decodeURIComponent(session[2].slice("sends/".length));
          const before = fakeSends ?? [];
          fakeSends = before.filter((q) => q.id !== id);
          if (fakeSends.length === before.length) {
            send(res, 404, { error: { code: "not_found", message: "no queued send " + id } });
            return;
          }
          publishSends();
          send(res, 200, {});
          return;
        }
        if (session && req.method === "GET" && session[2] === "prompt") {
          if (process.env.CREW_FAKE_PROMPT === "raw") {
            send(res, 200, { text: "", choices: [], raw: "Allow this unusual request?\n  [a] allow  [d] deny" });
            return;
          }
          send(res, 200, {
            text: "Bash command\n\nmake test\n\nDo you want to proceed?",
            choices: [
              { id: "1", label: "Yes" },
              { id: "2", label: "Yes, and don't ask again for make commands" },
              { id: "3", label: "No, and tell Claude what to do differently" },
            ],
          });
          return;
        }
        if (session && req.method === "POST" && session[2] === "answer") {
          const body = await readJSON(req);
          if (body.choice === "3") {
            send(res, 409, { error: { code: "stale", message: "session is no longer waiting for a permission" } });
            return;
          }
          send(res, 200, {});
          return;
        }
        if (session && req.method === "POST" && session[2] === "interrupt") {
          send(res, 200, {});
          return;
        }
        if (req.method === "GET" && path === "/api/v1/workspaces" && process.env.CREW_FAKE_STREAM === "revoked") {
          send(res, 401, { error: { code: "unauthorized", message: "unknown or revoked device token" } });
          return;
        }
        if (req.method === "GET" && path === "/api/v1/automations") {
          send(res, 200, {
            automations: [
              { name: "fix-ci", agent: "claude", prompt: "Fix the CI of {{work_item}}.", on: ["pr.ci_failed"], enabled: true, source: "/home/me/.crew/config.toml" },
              { name: "deps", agent: "codex", prompt: "Update the deps.", schedule: "weekly mon 09:00", enabled: false, source: "/home/me/.crew/config.toml" },
              { name: "audit", agent: "claude", prompt: "Audit the api.", enabled: true, source: "/home/me/api/.crew.toml" },
            ],
          });
          return;
        }
        if (req.method === "GET" && path === "/api/v1/mates") {
          send(res, 200, {
            mates: [
              { name: "helper", prompt: "You help with the api.", agent: "claude", repos: ["api"], enabled: true },
              { name: "reviewer", prompt: "You review PRs.", agent: "claude", enabled: true },
              { name: "webby", prompt: "You help with the web.", agent: "codex", enabled: false },
            ],
            limits: { hop_limit: 5, turns_per_hour: 20, daily_turns: 100, daily_tokens: 0, stop: false },
          });
          return;
        }
        if (req.method === "GET" && path === "/api/v1/approvals") {
          send(res, 200, [
            {
              ID: "approval-2",
              SessionID: "s3",
              Mate: "helper",
              Requester: "U0GUEST01",
              Action: { Kind: "create_pr", Tool: "Bash", Input: '{"command":"gh pr create --fill --base main"}' },
              At: new Date(Date.now() - 2 * 3600 * 1000).toISOString(),
              State: "pending",
            },
            {
              ID: "approval-1",
              SessionID: "s3",
              Mate: "helper",
              Requester: "U0GUEST01",
              Action: { Kind: "push", Tool: "Bash", Input: '{"command":"git push origin fix-login"}' },
              At: new Date(Date.now() - 5 * 3600 * 1000).toISOString(),
              State: "pending",
            },
          ]);
          return;
        }
        if (req.method === "POST" && /^\/api\/v1\/approvals\/[^/]+\/(approve|deny)$/.test(path)) {
          send(res, 200, { ID: path.split("/")[4], State: path.endsWith("approve") ? "granted" : "denied" });
          return;
        }
        if (req.method === "GET" && path === "/api/v1/automations/runs") {
          send(res, 200, {
            runs: [
              { id: "r3", automation: "fix-ci", trigger: "pr.ci_failed", outcome: "succeeded", session_id: "s3", start: "2026-10-03T11:40:00Z" },
              { id: "r2", automation: "audit", trigger: "manual", outcome: "failed", reason: "agent exited with status 1", start: "2026-10-03T09:10:00Z" },
              { id: "r1", automation: "fix-ci", trigger: "pr.ci_failed", outcome: "skipped", reason: "precheck failed", start: "2026-10-02T16:00:00Z" },
            ],
          });
          return;
        }
        if (req.method === "POST" && path === "/api/v1/workspaces") {
          const body = await readJSON(req);
          send(res, 200, { Root: String(body.path ?? ""), Kind: "single", Repos: [], LastUsed: "0001-01-01T00:00:00Z" });
          return;
        }
        if (req.method === "POST" && path === "/api/v1/workspaces/remove") {
          send(res, 200, {});
          return;
        }
        if (req.method === "POST" && path.startsWith("/api/v1/automations/")) {
          send(res, 200, { run: { id: "r9", automation: "fix-ci", outcome: "skipped", reason: "precheck failed" }, session: {} });
          return;
        }
        if (req.method === "GET" && path === "/api/v1/workspaces") {
          send(res, 200, {
            workspaces: [
              { Root: "/home/me/api", Kind: "single", Repos: [], LastUsed: "2026-10-03T12:00:00Z" },
              { Root: "/home/me/web", Kind: "single", Repos: [], LastUsed: "2026-10-01T12:00:00Z" },
            ],
            last_used: "/home/me/api",
          });
          return;
        }
        if (req.method === "GET" && path === "/api/v1/session-options") {
          send(res, 200, {
            harnesses: [
              { harness: "claude", default_model: "opus", default_effort: "high" },
              { harness: "codex", default_model: "gpt-6.1-sol", default_effort: "medium" },
            ],
            home: "/home/me",
          });
          return;
        }
        if (req.method === "GET" && path === "/api/v1/work-items/resolve") {
          const query = new URL(req.url ?? "", "http://fake").searchParams;
          const item = query.get("item") ?? "";
          const workspace = query.get("workspace") ?? "";
          const pr = item.match(/^https:\/\/github\.com\/[^/]+\/([^/]+)\/pull\/(\d+)/);
          if (pr) {
            send(res, 200, { source: "pr", ref: pr[1] + "#" + pr[2], title: "Fix the login redirect", worktree: pr[1] + "-" + pr[2], workspace });
            return;
          }
          if (item.startsWith("http")) {
            send(res, 400, { error: { code: "bad_request", message: "unsupported link: use a GitHub pull request URL" } });
            return;
          }
          send(res, 200, { source: "text", title: item, worktree: item.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""), workspace });
          return;
        }
        if (req.method === "POST" && path === "/api/v1/sessions") {
          const body = await readJSON(req);
          if (String(body.work_item ?? "").includes("broken")) {
            send(res, 500, {
              error: { code: "failed", message: "setup /home/me/.crew/worktrees/api/broken: npm ci: exit status 1\nnpm ERR! code EUSAGE\nnpm ERR! missing lockfile" },
            });
            return;
          }
          send(res, 200, fakeSession("s9", { name: String(body.work_item ?? ""), State: "running" }));
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
