import { readFileSync } from "node:fs";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { App, type AppEnv } from "../App";
import type { Frame, Message, StreamState } from "../stream";
import { FakeServer, MemoryStorage } from "../test/fake-server";
import { FakeSockets } from "../test/fake-socket";

const golden = (name: string) => JSON.parse(readFileSync(new URL("../../../internal/serve/testdata/" + name, import.meta.url), "utf8"));
const device = { id: "k3m9p2qx", name: "iPhone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };

function tool(id: string, cursor: number, summary: string): Message {
  return { id, cursor, turn: "p1", role: "tool", text: "", tool: { name: "Bash", summary, status: "done" }, at: "2026-10-03T12:00:00Z" };
}

function open(messages: Message[]) {
  window.location.hash = "#/sessions/s1";
  const sockets = new FakeSockets();
  const storage = new MemoryStorage();
  storage.setItem("crew.auth", JSON.stringify({ token: "t0k", device }));
  const server = new FakeServer().on("GET", "/api/v1/sessions/s1/messages?limit=50", { status: 200, body: { messages, before: 0 } });
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash: "#/sessions/s1",
    host: "box.example.ts.net",
    userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
    openStream: sockets.open,
    retryDelay: () => 5,
    now: () => 0,
    onHashChange: () => () => undefined,
  };
  render(<App env={env} />);
  sockets.last.open();
  sockets.last.push({ state: (golden("stream-state.json") as Frame).state as StreamState });
}

afterEach(() => {
  window.location.hash = "";
});

describe("a session that uses the browser of the Mac app", () => {
  it("tells that the phone cannot show the browser", async () => {
    open([tool("t1", 10, "crew browser snapshot")]);
    expect(await screen.findByRole("note")).toHaveTextContent("This session uses the browser of the Mac app");
  });

  it("also knows the agentws name of the command", async () => {
    open([tool("t1", 10, "agentws browser goto localhost:3000")]);
    expect(await screen.findByRole("note")).toHaveTextContent("Mac app");
  });

  it("shows no note for other tools", async () => {
    open([tool("t1", 10, "go test ./...")]);
    await screen.findByText(/go test/);
    expect(screen.queryByRole("note")).toBeNull();
  });
});
