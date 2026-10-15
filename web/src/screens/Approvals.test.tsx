import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { App, type AppEnv } from "../App";
import { FakeServer, MemoryStorage } from "../test/fake-server";
import { FakeSockets } from "../test/fake-socket";

const device = { id: "k3m9p2qx", name: "work phone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };

const pending = [
  {
    ID: "a1",
    SessionID: "s1",
    Mate: "helper",
    Requester: "U0GUEST01",
    Action: { Kind: "create_pr", Tool: "Bash", Input: '{"command":"gh pr create --fill"}' },
    At: "2026-10-03T11:00:00Z",
    State: "pending",
  },
];

function setup(list: unknown[] = pending) {
  const server = new FakeServer()
    .on("GET", "/api/v1/hello", { status: 200, body: { api: "v1", build: "v0.12.0+abc" } })
    .on("GET", "/api/v1/approvals", { status: 200, body: list });
  const storage = new MemoryStorage();
  storage.setItem("crew.auth", JSON.stringify({ token: "t0k", device }));
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash: "#/approvals",
    host: "crew.example.ts.net",
    userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
    openStream: new FakeSockets().open,
    now: () => Date.parse("2026-10-03T12:00:00Z"),
    onHashChange: () => () => undefined,
  };
  return { server, env };
}

describe("Approvals", () => {
  it("is a tab next to the session list", () => {
    const { env } = setup();
    render(<App env={{ ...env, hash: "" }} />);
    expect(screen.getByRole("link", { name: "Approvals" })).toHaveAttribute("href", "#/approvals");
  });

  it("shows a card for each pending approval with the mate, the action, who asked and the input", async () => {
    const { env } = setup();
    render(<App env={env} />);
    const card = await screen.findByRole("listitem", { name: "helper: create pr" });
    expect(card).toHaveTextContent("U0GUEST01");
    expect(card).toHaveTextContent("gh pr create --fill");
    expect(card).toHaveTextContent("expires in 23h");
    expect(within(card).getByRole("link", { name: "Open session" })).toHaveAttribute("href", "#/sessions/s1");
  });

  it("approves an approval and shows the result", async () => {
    const { server, env } = setup();
    server.on("POST", "/api/v1/approvals/a1/approve", { status: 200, body: { ...pending[0], State: "granted" } });
    render(<App env={env} />);
    const card = await screen.findByRole("listitem", { name: "helper: create pr" });
    await userEvent.click(within(card).getByRole("button", { name: "Approve" }));
    expect(server.calls.some((c) => c.method === "POST" && c.path === "/api/v1/approvals/a1/approve")).toBe(true);
    expect(await within(card).findByRole("status")).toHaveTextContent("Approved. The session tries it again.");
    expect(within(card).queryByRole("button", { name: "Approve" })).toBeNull();
  });

  it("denies an approval", async () => {
    const { server, env } = setup();
    server.on("POST", "/api/v1/approvals/a1/deny", { status: 200, body: { ...pending[0], State: "denied" } });
    render(<App env={env} />);
    const card = await screen.findByRole("listitem", { name: "helper: create pr" });
    await userEvent.click(within(card).getByRole("button", { name: "Deny" }));
    expect(await within(card).findByRole("status")).toHaveTextContent("Denied.");
  });

  it("tells you when an approval is gone", async () => {
    const { server, env } = setup();
    server.on("POST", "/api/v1/approvals/a1/approve", { status: 409, body: { error: { code: "stale", message: "the approval expired" } } });
    render(<App env={env} />);
    const card = await screen.findByRole("listitem", { name: "helper: create pr" });
    await userEvent.click(within(card).getByRole("button", { name: "Approve" }));
    expect(await within(card).findByRole("status")).toHaveTextContent("This approval is gone: the approval expired");
  });

  it("says when nothing waits", async () => {
    const { env } = setup([]);
    render(<App env={env} />);
    expect(await screen.findByText("No approvals wait for you.")).toBeInTheDocument();
  });
});
