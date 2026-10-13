import { readFileSync } from "node:fs";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { App, type AppEnv } from "../App";
import { FakeServer, MemoryStorage } from "../test/fake-server";
import { FakeSockets } from "../test/fake-socket";

const golden = (name: string) => JSON.parse(readFileSync(new URL("../../../internal/serve/testdata/" + name, import.meta.url), "utf8"));
const device = { id: "k3m9p2qx", name: "iPhone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };
const workspaces = {
  workspaces: [
    { Root: "/home/me/api", Kind: "single", Repos: [], LastUsed: "2026-10-03T12:00:00Z" },
    { Root: "/home/me/web", Kind: "single", Repos: [], LastUsed: "2026-10-01T12:00:00Z" },
  ],
  last_used: "/home/me/api",
};
const resolveURL = (workspace: string, item: string) => "/api/v1/work-items/resolve?" + new URLSearchParams({ workspace, item }).toString();

function setup(options: { remembered?: unknown; workspacesDown?: boolean } = {}) {
  window.location.hash = "#/new";
  const sockets = new FakeSockets();
  const storage = new MemoryStorage();
  storage.setItem("agentws.auth", JSON.stringify({ token: "t0k", device }));
  if (options.remembered) {
    storage.setItem("agentws.new", JSON.stringify(options.remembered));
  }
  const server = new FakeServer()
    .on("GET", "/api/v1/hello", { status: 200, body: { api: "v1", build: "v0.12.0+abc" } })
    .on("GET", "/api/v1/workspaces", options.workspacesDown ? { status: 503, body: { error: { code: "unavailable", message: "daemon is down" } } } : { status: 200, body: workspaces });
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash: "#/new",
    host: "agentws.example.ts.net",
    userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
    openStream: sockets.open,
    retryDelay: () => 5,
    now: () => 0,
    onHashChange: (cb) => {
      const listener = () => cb(window.location.hash);
      window.addEventListener("hashchange", listener);
      return () => window.removeEventListener("hashchange", listener);
    },
  };
  render(<App env={env} />);
  return { server, storage, sockets };
}

beforeEach(() => {
  window.location.hash = "";
});

afterEach(() => {
  window.location.hash = "";
});

describe("starting a session from the phone", () => {
  it("opens from a New session link on the session list", async () => {
    window.location.hash = "";
    const sockets = new FakeSockets();
    const storage = new MemoryStorage();
    storage.setItem("agentws.auth", JSON.stringify({ token: "t0k", device }));
    const server = new FakeServer().on("GET", "/api/v1/workspaces", { status: 200, body: workspaces });
    render(
      <App
        env={{
          fetch: server.fetch,
          storage,
          installed: true,
          hash: "",
          host: "h",
          userAgent: "",
          openStream: sockets.open,
          now: () => 0,
          onHashChange: (cb) => {
            const listener = () => cb(window.location.hash);
            window.addEventListener("hashchange", listener);
            return () => window.removeEventListener("hashchange", listener);
          },
        }}
      />,
    );
    const link = screen.getByRole("link", { name: "New session" });
    expect(link).toHaveAttribute("href", "#/new");
    await userEvent.click(link);
    expect(await screen.findByRole("heading", { name: "New session" })).toBeInTheDocument();
  });

  it("defaults to the daemon's last used workspace, Claude, and no model or effort", async () => {
    const { server } = setup();
    const workspace = await screen.findByLabelText("Workspace");
    await waitFor(() => expect(workspace).toHaveValue("/home/me/api"));
    expect(screen.getByRole("radio", { name: "Claude" })).toBeChecked();
    expect(screen.getByLabelText("Model")).toHaveValue("");
    expect(screen.getByLabelText("Effort")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Start session" })).toBeDisabled();
    expect(new Set(server.headers.map((h) => h.authorization))).toEqual(new Set(["Bearer t0k"]));
  });

  it("defaults to what this phone used last", async () => {
    setup({ remembered: { workspace: "/home/me/web", harness: "codex", model: "gpt-5.5", effort: "high" } });
    const workspace = await screen.findByLabelText("Workspace");
    await waitFor(() => expect(workspace).toHaveValue("/home/me/web"));
    expect(screen.getByRole("radio", { name: "Codex" })).toBeChecked();
    expect(screen.getByLabelText("Model")).toHaveValue("gpt-5.5");
    expect(screen.getByLabelText("Effort")).toHaveValue("high");
  });

  it("falls back to the last used workspace when the remembered one is gone", async () => {
    setup({ remembered: { workspace: "/home/me/gone", harness: "claude", model: "", effort: "" } });
    const workspace = await screen.findByLabelText("Workspace");
    await waitFor(() => expect(workspace).toHaveValue("/home/me/api"));
  });

  it("asks for a PR URL or text as the work item", async () => {
    setup();
    expect(await screen.findByLabelText("Work item")).toHaveAttribute("placeholder", "PR URL, or what to do");
  });

  it("shows the resolved title and worktree name of a work item", async () => {
    const { server } = setup();
    server.on("GET", resolveURL("/home/me/api", "ENG-12"), { status: 200, body: golden("work-item-resolved.json") });
    await userEvent.type(await screen.findByLabelText("Work item"), "ENG-12");
    expect(await screen.findByText("Fix the login redirect")).toBeInTheDocument();
    expect(screen.getByText("api-12")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start session" })).toBeEnabled();
  });

  it("reports a bad work item in the form and does not allow the launch", async () => {
    const { server } = setup();
    const url = "https://example.com/x";
    server.on("GET", resolveURL("/home/me/api", url), { status: 400, body: golden("work-item-bad.json") });
    await userEvent.type(await screen.findByLabelText("Work item"), url);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("unsupported link: use a GitHub pull request URL");
    expect(screen.getByRole("button", { name: "Start session" })).toBeDisabled();
    expect(server.calls.some((c) => c.method === "POST")).toBe(false);
  });

  it("starts the session with the form's choices, remembers them and opens the new session", async () => {
    const { server, storage } = setup();
    server.on("GET", resolveURL("/home/me/web", "ENG-12"), { status: 200, body: golden("work-item-resolved.json") });
    server.on("POST", "/api/v1/sessions", { status: 200, body: golden("session-new.json") });
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByLabelText("Workspace")).toHaveValue("/home/me/api"));
    await user.selectOptions(screen.getByLabelText("Workspace"), "/home/me/web");
    await user.type(screen.getByLabelText("Work item"), "ENG-12");
    await user.click(screen.getByRole("radio", { name: "Codex" }));
    await user.type(screen.getByLabelText("Model"), "gpt-5.5");
    await user.selectOptions(screen.getByLabelText("Effort"), "high");
    await user.type(screen.getByLabelText("First prompt"), "start with the tests");
    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() => expect(window.location.hash).toBe("#/sessions/s9"));
    const post = server.calls.find((c) => c.method === "POST");
    expect(post?.body).toEqual({
      workspace: "/home/me/web",
      work_item: "ENG-12",
      harness: "codex",
      model: "gpt-5.5",
      effort: "high",
      prompt: "start with the tests",
    });
    expect(JSON.parse(storage.getItem("agentws.new") ?? "{}")).toEqual({
      workspace: "/home/me/web",
      harness: "codex",
      model: "gpt-5.5",
      effort: "high",
    });
  });

  it("leaves the prompt out when it is blank", async () => {
    const { server } = setup();
    server.on("GET", resolveURL("/home/me/api", "tidy"), { status: 200, body: { source: "text", title: "tidy", worktree: "tidy", workspace: "/home/me/api" } });
    server.on("POST", "/api/v1/sessions", { status: 200, body: golden("session-new.json") });
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByLabelText("Workspace")).toHaveValue("/home/me/api"));
    await user.type(screen.getByLabelText("Work item"), "tidy");
    await user.type(screen.getByLabelText("First prompt"), "   ");
    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() => expect(window.location.hash).toBe("#/sessions/s9"));
    expect(server.calls.find((c) => c.method === "POST")?.body).toEqual({ workspace: "/home/me/api", work_item: "tidy", harness: "claude" });
  });

  it("shows a setup recipe's output in the form when the launch fails", async () => {
    const { server } = setup();
    server.on("GET", resolveURL("/home/me/api", "tidy"), { status: 200, body: { source: "text", title: "tidy", worktree: "tidy", workspace: "/home/me/api" } });
    server.on("POST", "/api/v1/sessions", { status: 500, body: golden("session-new-failed.json") });
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByLabelText("Workspace")).toHaveValue("/home/me/api"));
    await user.type(screen.getByLabelText("Work item"), "tidy");
    await user.click(screen.getByRole("button", { name: "Start session" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("setup /w/api: npm ci: exit status 1");
    expect(alert).toHaveTextContent("npm ERR! missing lockfile");
    expect(window.location.hash).toBe("#/new");
    expect(screen.getByRole("button", { name: "Start session" })).toBeEnabled();
  });

  it("reloads the workspaces when Retry now is pressed after they failed to load", async () => {
    const { server } = setup({ workspacesDown: true });
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load workspaces: daemon is down");
    server.on("GET", "/api/v1/workspaces", { status: 200, body: workspaces });
    await userEvent.click(screen.getByRole("button", { name: "Retry now" }));
    await waitFor(() => expect(screen.getByLabelText("Workspace")).toHaveValue("/home/me/api"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
