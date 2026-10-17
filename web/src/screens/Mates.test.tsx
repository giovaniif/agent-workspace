import { readFileSync } from "node:fs";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { App, type AppEnv } from "../App";
import type { Frame, Mates, StreamState } from "../stream";
import { FakeServer, MemoryStorage } from "../test/fake-server";
import { FakeSockets } from "../test/fake-socket";

const golden = (name: string) => JSON.parse(readFileSync(new URL("../../../internal/serve/testdata/" + name, import.meta.url), "utf8"));
const goldenState = (): StreamState => (golden("stream-state.json") as Frame).state as StreamState;
const device = { id: "k3m9p2qx", name: "work phone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };

const board: Mates = {
  stopped: false,
  mates: [
    {
      name: "helper",
      state: "connected",
      work: ["s1"],
      threads: [
        {
          mate: "helper",
          team: "T1",
          channel: "C1",
          thread_ts: "1700.0001",
          session_id: "s1",
          url: "https://app.slack.com/client/T1/C1/thread/C1-1700.0001",
          session: "add login",
          state: "running",
          ended: false,
        },
      ],
    },
    { name: "webby", state: "paused", work: [], threads: [] },
    { name: "fresh", state: "no_tokens", work: [], threads: [] },
  ],
};

const defs = {
  mates: [
    { name: "helper", prompt: "Help.", agent: "claude", repos: ["api"], enabled: true },
    { name: "webby", prompt: "Web.", agent: "codex", enabled: false },
    { name: "fresh", prompt: "New.", agent: "claude", enabled: true },
  ],
  limits: { hop_limit: 5, turns_per_hour: 20, daily_turns: 100, daily_tokens: 0, stop: false },
};

function setup(hash = "#/mates", mates: Mates = board) {
  const sockets = new FakeSockets();
  const server = new FakeServer()
    .on("GET", "/api/v1/hello", { status: 200, body: { api: "v1", build: "v0.12.0+abc" } })
    .on("GET", "/api/v1/workspaces", { status: 200, body: { workspaces: [], last_used: "" } })
    .on("GET", "/api/v1/mates", { status: 200, body: defs })
    .on("GET", "/api/v1/approvals", { status: 200, body: [] })
    .on("POST", "/api/v1/mates/stop", { status: 200, body: defs.limits })
    .on("POST", "/api/v1/mates/helper/pause", { status: 200, body: defs.mates[0] })
    .on("POST", "/api/v1/mates/webby/resume", { status: 200, body: defs.mates[1] })
    .on("POST", "/api/v1/mates", { status: 200, body: { name: "scout", prompt: "Scout.", agent: "claude", enabled: true } })
    .on("GET", "/api/v1/mates/scout/setup", { status: 200, body: { manifest: "{}", url: "https://api.slack.com/apps?new_app=1" } })
    .on("POST", "/api/v1/mates/scout/tokens", { status: 200, body: { name: "scout", has_bot_token: true, has_app_token: true } })
    .on("POST", "/api/v1/mates/scout/claim", { status: 200, body: { name: "scout", code: "ABCD2345", expires_at: "2026-10-03T12:10:00Z" } });
  const storage = new MemoryStorage();
  storage.setItem("crew.auth", JSON.stringify({ token: "t0k", device }));
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash,
    host: "crew.example.ts.net",
    userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
    openStream: sockets.open,
    now: () => Date.parse("2026-10-03T12:00:00Z"),
    onHashChange: () => () => undefined,
  };
  render(<App env={env} />);
  sockets.last.open();
  sockets.last.push({ state: { ...goldenState(), mates } });
  return { server, sockets };
}

describe("Mates", () => {
  it("is a tab", () => {
    setup("");
    expect(screen.getByRole("link", { name: "Mates" })).toHaveAttribute("href", "#/mates");
  });

  it("shows each mate with its state, current work and threads that link to Slack and the session", async () => {
    setup();
    const helper = await screen.findByRole("listitem", { name: "helper" });
    expect(helper).toHaveTextContent("connected");
    expect(helper).toHaveTextContent("Works on add login");
    const threads = within(helper).getByRole("list", { name: "Threads of helper" });
    expect(within(threads).getByRole("link", { name: "add login" })).toHaveAttribute("href", "#/sessions/s1");
    expect(within(threads).getByRole("link", { name: "Slack" })).toHaveAttribute("href", "https://app.slack.com/client/T1/C1/thread/C1-1700.0001");
    expect(screen.getByRole("listitem", { name: "webby" })).toHaveTextContent("paused");
    expect(await screen.findByText("No approvals wait for you.")).toBeInTheDocument();
  });

  it("pauses a mate and resumes a paused one", async () => {
    const { server } = setup();
    const user = userEvent.setup();
    await user.click(within(await screen.findByRole("listitem", { name: "helper" })).getByRole("button", { name: "Pause" }));
    await user.click(within(screen.getByRole("listitem", { name: "webby" })).getByRole("button", { name: "Resume" }));
    await waitFor(() => expect(server.calls.map((c) => c.method + " " + c.path)).toContain("POST /api/v1/mates/webby/resume"));
    expect(server.calls.map((c) => c.method + " " + c.path)).toContain("POST /api/v1/mates/helper/pause");
  });

  it("stops all mates only after a confirm", async () => {
    const { server } = setup();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Stop all mates" }));
    expect(server.calls.some((c) => c.path === "/api/v1/mates/stop")).toBe(false);
    await user.click(screen.getByRole("button", { name: "Stop all" }));
    await waitFor(() => expect(server.calls.find((c) => c.path === "/api/v1/mates/stop")?.body).toEqual({ stop: true }));
  });

  it("shows the stop from the stream on the Mates page and on the session list, and starts the mates again", async () => {
    const { server, sockets } = setup();
    sockets.last.push({ diff: { seq: 99, mates: { ...board, stopped: true } } });
    const user = userEvent.setup();
    expect(await screen.findByText("All mates are stopped.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Start all mates" }));
    await waitFor(() => expect(server.calls.find((c) => c.path === "/api/v1/mates/stop")?.body).toEqual({ stop: false }));
  });

  it("shows a stopped banner over the session list", async () => {
    setup("", { ...board, stopped: true });
    expect(await screen.findByRole("status")).toHaveTextContent("All mates are stopped");
  });

  it("edits the settings of a mate", async () => {
    const { server } = setup();
    const user = userEvent.setup();
    const helper = await screen.findByRole("listitem", { name: "helper" });
    await user.click(within(helper).getByRole("button", { name: "Settings" }));
    const form = within(helper).getByRole("form", { name: "Settings of helper" });
    const model = within(form).getByLabelText("Model");
    await user.type(model, "opus");
    await user.click(within(form).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(server.calls.find((c) => c.method === "POST" && c.path === "/api/v1/mates")?.body).toEqual({
        mate: { name: "helper", prompt: "Help.", agent: "claude", model: "opus", repos: ["api"], enabled: true },
      }),
    );
  });

  it("adds a mate, links the Slack manifest, saves the tokens and shows the claim code", async () => {
    const { server } = setup();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Add a mate" }));
    const form = screen.getByRole("form", { name: "New mate" });
    await user.type(within(form).getByLabelText("Name"), "scout");
    await user.type(within(form).getByLabelText("Prompt"), "Scout.");
    await user.click(within(form).getByRole("button", { name: "Add mate" }));
    const link = await screen.findByRole("link", { name: "Make the Slack app from the manifest" });
    expect(link).toHaveAttribute("href", "https://api.slack.com/apps?new_app=1");
    await user.type(screen.getByLabelText("Bot token"), "xoxb-1");
    await user.type(screen.getByLabelText("App token"), "xapp-1");
    await user.click(screen.getByRole("button", { name: "Save tokens" }));
    expect(await screen.findByText("ABCD2345")).toBeInTheDocument();
    expect(server.calls.find((c) => c.path === "/api/v1/mates/scout/tokens")?.body).toEqual({ bot_token: "xoxb-1", app_token: "xapp-1" });
  });
});
