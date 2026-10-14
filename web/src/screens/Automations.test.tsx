import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { App, type AppEnv } from "../App";
import { FakeServer, MemoryStorage } from "../test/fake-server";
import { FakeSockets } from "../test/fake-socket";

const device = { id: "k3m9p2qx", name: "work phone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };

const automations = {
  automations: [
    { name: "fix-ci", agent: "claude", target: "new-worktree", prompt: "Fix the CI of {{work_item}}.", on: ["pr.ci_failed"], enabled: true, source: "/h/config.toml" },
    { name: "deps", agent: "codex", prompt: "Update the deps.", schedule: "weekly mon 09:00", enabled: false, source: "/h/config.toml" },
  ],
};

const runs = {
  runs: [
    { id: "r2", automation: "fix-ci", trigger: "pr.ci_failed", outcome: "failed", reason: "agent exited", start: "2026-10-03T11:00:00Z", end: "2026-10-03T11:05:00Z" },
    { id: "r1", automation: "fix-ci", trigger: "manual", outcome: "succeeded", session_id: "s1", start: "2026-10-02T09:00:00Z", end: "2026-10-02T09:30:00Z" },
  ],
};

function setup(hash: string) {
  const server = new FakeServer()
    .on("GET", "/api/v1/hello", { status: 200, body: { api: "v1", build: "v0.12.0+abc" } })
    .on("GET", "/api/v1/automations", { status: 200, body: automations })
    .on("GET", "/api/v1/automations/runs?limit=100", { status: 200, body: runs });
  const storage = new MemoryStorage();
  storage.setItem("agentws.auth", JSON.stringify({ token: "t0k", device }));
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash,
    host: "agentws.example.ts.net",
    userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
    openStream: new FakeSockets().open,
    now: () => Date.parse("2026-10-03T12:00:00Z"),
    onHashChange: () => () => undefined,
  };
  return { server, env };
}

describe("Automations", () => {
  it("is a tab next to the session list", () => {
    const { env } = setup("");
    render(<App env={env} />);
    expect(screen.getByRole("link", { name: "Automations" })).toHaveAttribute("href", "#/automations");
  });

  it("lists each automation with its agent, trigger and state", async () => {
    const { env } = setup("#/automations");
    render(<App env={env} />);
    const fix = await screen.findByRole("listitem", { name: "fix-ci" });
    expect(fix).toHaveTextContent("claude");
    expect(fix).toHaveTextContent("on pr.ci_failed");
    const deps = screen.getByRole("listitem", { name: "deps" });
    expect(deps).toHaveTextContent("weekly mon 09:00");
    expect(deps).toHaveTextContent("paused");
  });

  it("shows the outcome of the last run of each automation", async () => {
    const { env } = setup("#/automations");
    render(<App env={env} />);
    const fix = await screen.findByRole("listitem", { name: "fix-ci" });
    expect(await within(fix).findByText("last run failed: agent exited")).toBeInTheDocument();
    expect(within(screen.getByRole("listitem", { name: "deps" })).getByText("never run")).toBeInTheDocument();
  });

  it("shows the run history with a link to the session of a run", async () => {
    const { env } = setup("#/automations");
    render(<App env={env} />);
    await screen.findByText("last run failed: agent exited");
    await userEvent.click(screen.getByRole("button", { name: "History of fix-ci" }));
    const history = screen.getByRole("list", { name: "Runs of fix-ci" });
    expect(within(history).getAllByRole("listitem")).toHaveLength(2);
    expect(within(history).getByRole("link", { name: "Open session" })).toHaveAttribute("href", "#/sessions/s1");
  });

  it("names the run button Run with context", async () => {
    const { env } = setup("#/automations");
    render(<App env={env} />);
    await screen.findByRole("listitem", { name: "fix-ci" });
    expect(screen.getByRole("button", { name: "Run fix-ci" })).toHaveTextContent("Run with context");
  });

  it("runs an automation with context and opens its session", async () => {
    const { server, env } = setup("#/automations");
    server.on("POST", "/api/v1/automations/fix-ci/run", {
      status: 200,
      body: { run: { id: "r1", automation: "fix-ci", outcome: "succeeded", session_id: "s9" }, session: { ID: "s9" } },
    });
    render(<App env={env} />);
    await screen.findByRole("listitem", { name: "fix-ci" });
    await userEvent.click(screen.getByRole("button", { name: "Run fix-ci" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Context" }), "only the lint job");
    await userEvent.type(screen.getByRole("textbox", { name: "Work item" }), "#42");
    await userEvent.click(screen.getByRole("button", { name: "Run" }));
    const post = server.calls.find((c) => c.method === "POST");
    expect(post?.body).toEqual({ context: "only the lint job", work_item: "#42" });
    expect(window.location.hash).toBe("#/sessions/s9");
  });

  it("shows a skipped run instead of opening a session", async () => {
    const { server, env } = setup("#/automations");
    server.on("POST", "/api/v1/automations/fix-ci/run", {
      status: 200,
      body: { run: { id: "r1", automation: "fix-ci", outcome: "skipped", reason: "precheck failed" }, session: { ID: "" } },
    });
    render(<App env={env} />);
    await screen.findByRole("listitem", { name: "fix-ci" });
    await userEvent.click(screen.getByRole("button", { name: "Run fix-ci" }));
    await userEvent.click(screen.getByRole("button", { name: "Run" }));
    expect(await screen.findByRole("status")).toHaveTextContent("skipped: precheck failed");
  });
});
