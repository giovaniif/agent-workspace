import { readFileSync } from "node:fs";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { App, type AppEnv } from "../App";
import type { Frame, Message, Session, StreamState } from "../stream";
import { Deferred, FakeServer, MemoryStorage, type FakeRoute } from "../test/fake-server";
import { FakeSockets } from "../test/fake-socket";

const golden = (name: string) => JSON.parse(readFileSync(new URL("../../../internal/serve/testdata/" + name, import.meta.url), "utf8"));
const goldenState = (): StreamState => (golden("stream-state.json") as Frame).state as StreamState;
const goldenFrames = (): Frame[] => golden("stream-transcript.json") as Frame[];
const device = { id: "k3m9p2qx", name: "iPhone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };
const halfPastNoon = new Date("2026-10-03T12:30:00Z").getTime();
const newest = "/api/v1/sessions/s1/messages?limit=50";

function msg(id: string, cursor: number, over: Partial<Message> = {}): Message {
  return { id, cursor, turn: "p1", role: "assistant", text: "message " + id, at: "2026-10-03T12:00:00Z", ...over };
}

function withSession(state: StreamState, id: string, over: Partial<Session>): StreamState {
  return { ...state, sessions: state.sessions.map((s) => (s.ID === id ? { ...s, ...over } : s)) };
}

function setup(server: FakeServer, hash = "#/sessions/s1") {
  window.location.hash = hash;
  const sockets = new FakeSockets();
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
    retryDelay: () => 5,
    now: () => halfPastNoon,
    onHashChange: (cb) => {
      const listener = () => cb(window.location.hash);
      window.addEventListener("hashchange", listener);
      return () => window.removeEventListener("hashchange", listener);
    },
  };
  render(<App env={env} />);
  return sockets;
}

function live(sockets: FakeSockets, state: StreamState = goldenState()) {
  sockets.last.open();
  sockets.last.push({ state });
}

function watches(sockets: FakeSockets) {
  return sockets.last.sent.filter((f) => typeof f === "object" && f !== null && ("watch" in f || "unwatch" in f));
}

function page(messages: Message[], before = 0): FakeRoute {
  return { status: 200, body: { messages, before } };
}

beforeEach(() => {
  window.location.hash = "";
});

afterEach(() => {
  window.location.hash = "";
});

describe("the chat header", () => {
  it("shows the name, repo@branch, model, context left and the state", async () => {
    const server = new FakeServer().on("GET", newest, page([]));
    const sockets = setup(server);
    live(sockets, withSession(goldenState(), "s1", { Usage: { ContextLeftPercent: 62, HasContext: true, LimitUsedPercent: 0 } }));
    const header = within(screen.getByRole("banner"));
    expect(header.getByRole("heading", { name: "fix the login redirect" })).toBeInTheDocument();
    expect(header.getByText(/api@login/)).toBeInTheDocument();
    expect(header.getByText(/opus/)).toBeInTheDocument();
    expect(header.getByText("62% context left")).toBeInTheDocument();
    expect(header.getByText("working")).toBeInTheDocument();
  });

  it("follows the session's state live", async () => {
    const server = new FakeServer().on("GET", newest, page([]));
    const sockets = setup(server);
    live(sockets);
    sockets.last.push((golden("stream-diffs.json") as Frame[])[0]);
    expect(within(screen.getByRole("banner")).getByText("needs permission")).toBeInTheDocument();
  });
});

describe("the transcript", () => {
  it("loads the newest page, then watches after the newest cursor it holds", async () => {
    const server = new FakeServer().on("GET", newest, { status: 200, body: golden("messages.json") });
    const sockets = setup(server);
    live(sockets);
    const log = within(screen.getByRole("log", { name: "Messages" }));
    expect(await log.findByText("fix the login redirect")).toBeInTheDocument();
    expect(server.headers[server.calls.findIndex((c) => c.path === newest)].authorization).toBe("Bearer t0k");
    await waitFor(() => expect(watches(sockets)).toEqual([{ watch: "s1", after: 210 }]));
  });

  it("shows new messages live, with a running tool marked live and a finished one with its test result", async () => {
    const server = new FakeServer().on("GET", newest, page([msg("u1", 40, { role: "user", text: "fix the login redirect" })]));
    const sockets = setup(server);
    live(sockets);
    const log = within(screen.getByRole("log", { name: "Messages" }));
    await log.findByText("fix the login redirect");
    const [first, second] = goldenFrames();
    sockets.last.push(first);
    sockets.last.push(second);
    expect(log.getByText("Looking at the redirect.")).toBeInTheDocument();
    const running = log.getByRole("group", { name: /Bash/ });
    expect(running).toHaveTextContent("go test ./...");
    expect(within(running).getByText("running")).toBeInTheDocument();
    sockets.last.push({
      transcript: {
        session: "s1",
        messages: [{ ...second.transcript!.messages[0], text: "ok  \tgithub.com/x/api\t0.2s", tool: { name: "Bash", summary: "go test ./...", status: "done" } }],
      },
    });
    const done = log.getByRole("group", { name: /Bash/ });
    expect(within(done).queryByText("running")).not.toBeInTheDocument();
    expect(done).toHaveTextContent("1 package ok");
    expect(log.getAllByRole("group", { name: /Bash/ })).toHaveLength(1);
  });

  it("ignores transcript frames of other sessions", async () => {
    const server = new FakeServer().on("GET", newest, page([]));
    const sockets = setup(server);
    live(sockets);
    await waitFor(() => expect(watches(sockets)).toHaveLength(1));
    sockets.last.push({ transcript: { session: "s2", messages: [msg("x1", 5, { text: "not mine" })] } });
    expect(screen.queryByText("not mine")).not.toBeInTheDocument();
  });

  it("renders assistant text as markdown without raw HTML", async () => {
    const server = new FakeServer().on("GET", newest, page([msg("a1", 10, { text: "Use **retries** and `backoff`.\n\n<img src=x alt=injected>" })]));
    const sockets = setup(server);
    live(sockets);
    const log = within(screen.getByRole("log", { name: "Messages" }));
    expect(await log.findByText("retries")).toHaveProperty("tagName", "STRONG");
    expect(log.getByText("backoff")).toHaveProperty("tagName", "CODE");
    expect(screen.queryByRole("img", { name: "injected" })).not.toBeInTheDocument();
  });

  it("watches again after the newest cursor when the stream comes back", async () => {
    const server = new FakeServer().on("GET", newest, page([msg("a1", 80)]));
    const sockets = setup(server);
    live(sockets);
    await waitFor(() => expect(watches(sockets)).toEqual([{ watch: "s1", after: 80 }]));
    sockets.last.push({ transcript: { session: "s1", messages: [msg("a2", 130)] } });
    sockets.last.drop(1013);
    await waitFor(() => expect(sockets.opened).toHaveLength(2));
    live(sockets);
    await waitFor(() => expect(watches(sockets)).toEqual([{ watch: "s1", after: 130 }]));
  });

  it("stops watching when the screen closes", async () => {
    const server = new FakeServer().on("GET", newest, page([msg("a1", 80)]));
    const sockets = setup(server);
    live(sockets);
    await waitFor(() => expect(watches(sockets)).toHaveLength(1));
    await userEvent.click(screen.getByRole("link", { name: "Sessions" }));
    await screen.findByRole("region", { name: "Working" });
    expect(watches(sockets)).toEqual([{ watch: "s1", after: 80 }, { unwatch: "s1" }]);
  });

  it("loads the older page when scrolled to the top and keeps the reading position", async () => {
    const later = Array.from({ length: 10 }, (_, i) => msg("b" + i, 1000 + i * 10));
    const earlier = Array.from({ length: 10 }, (_, i) => msg("a" + i, 100 + i * 10));
    const older = new Deferred<FakeRoute>();
    const server = new FakeServer()
      .on("GET", newest, page(later, 90))
      .on("GET", "/api/v1/sessions/s1/messages?before=90&limit=50", () => older.promise);
    const sockets = setup(server);
    live(sockets);
    const log = screen.getByRole("log", { name: "Messages" });
    Object.defineProperty(log, "scrollHeight", { configurable: true, get: () => log.querySelectorAll("[data-message]").length * 100 });
    Object.defineProperty(log, "clientHeight", { configurable: true, get: () => 500 });
    await within(log).findByText("message b9");
    expect(log.scrollTop).toBe(1000);
    log.scrollTop = 0;
    fireEvent.scroll(log);
    expect(await screen.findByText("Loading earlier messages…")).toBeInTheDocument();
    await act(async () => older.resolve(page(earlier, 0)));
    expect(within(log).getByText("message a0")).toBeInTheDocument();
    expect(log.scrollTop).toBe(1000);
    expect(screen.queryByText("Loading earlier messages…")).not.toBeInTheDocument();
    expect(screen.getByText("Start of the conversation")).toBeInTheDocument();
  });

  it("drops an older page that lands after the session moved to another transcript file", async () => {
    const older = new Deferred<FakeRoute>();
    let calls = 0;
    const server = new FakeServer()
      .on("GET", newest, () => {
        calls++;
        return calls === 1 ? page([msg("b1", 1000)], 90) : page([msg("n1", 50)], 30);
      })
      .on("GET", "/api/v1/sessions/s1/messages?before=90&limit=50", () => older.promise);
    const sockets = setup(server);
    live(sockets);
    const log = within(screen.getByRole("log", { name: "Messages" }));
    await log.findByText("message b1");
    await userEvent.click(screen.getByRole("button", { name: "Load earlier messages" }));
    sockets.last.push({ transcript: { session: "s1", messages: [msg("n1", 50)], reset: true } });
    await act(async () => older.resolve(page([msg("a1", 10)], 0)));
    expect(log.getByText("message n1")).toBeInTheDocument();
    expect(log.queryByText("message a1")).not.toBeInTheDocument();
    expect(log.queryByText("message b1")).not.toBeInTheDocument();
    expect(screen.queryByText("Start of the conversation")).not.toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Load earlier messages" })).toBeInTheDocument();
  });

  it("asks for the newest page of the new file after a reset, so older pages load from there", async () => {
    let calls = 0;
    const server = new FakeServer().on("GET", newest, () => {
      calls++;
      return calls === 1 ? page([msg("b1", 1000)], 90) : page([msg("n0", 20), msg("n1", 50)], 0);
    });
    const sockets = setup(server);
    live(sockets);
    const log = within(screen.getByRole("log", { name: "Messages" }));
    await log.findByText("message b1");
    sockets.last.push({ transcript: { session: "s1", messages: [msg("n1", 50)], reset: true } });
    expect(await log.findByText("message n0")).toBeInTheDocument();
    expect(screen.getByText("Start of the conversation")).toBeInTheDocument();
    expect(log.queryByText("message b1")).not.toBeInTheDocument();
    expect(watches(sockets)).toEqual([{ watch: "s1", after: 1000 }]);
  });

  it("keeps the view at the bottom as messages arrive while the reader is there", async () => {
    const server = new FakeServer().on("GET", newest, page([msg("a1", 10), msg("a2", 20)]));
    const sockets = setup(server);
    live(sockets);
    const log = screen.getByRole("log", { name: "Messages" });
    Object.defineProperty(log, "scrollHeight", { configurable: true, get: () => log.querySelectorAll("[data-message]").length * 400 });
    Object.defineProperty(log, "clientHeight", { configurable: true, get: () => 500 });
    await within(log).findByText("message a2");
    sockets.last.push({ transcript: { session: "s1", messages: [msg("a3", 30)] } });
    expect(log.scrollTop).toBe(1200);
    log.scrollTop = 100;
    fireEvent.scroll(log);
    sockets.last.push({ transcript: { session: "s1", messages: [msg("a4", 40)] } });
    expect(log.scrollTop).toBe(100);
  });
});

describe("the composer", () => {
  it("sends at once when the agent is free and clears the text", async () => {
    const server = new FakeServer()
      .on("GET", "/api/v1/sessions/s2/messages?limit=50", page([]))
      .on("POST", "/api/v1/sessions/s2/messages", { status: 200, body: { id: "q1", queued: false } });
    const sockets = setup(server, "#/sessions/s2");
    live(sockets);
    const box = screen.getByRole("textbox", { name: "Message" });
    await userEvent.type(box, "now add a test");
    await userEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(box).toHaveValue(""));
    const post = server.calls.findIndex((c) => c.method === "POST");
    expect(server.calls[post]?.body).toEqual({ text: "now add a test" });
    expect(server.headers[post].authorization).toBe("Bearer t0k");
  });

  it("keeps what was typed while a send was on its way", async () => {
    const posted = new Deferred<FakeRoute>();
    const server = new FakeServer()
      .on("GET", "/api/v1/sessions/s2/messages?limit=50", page([]))
      .on("POST", "/api/v1/sessions/s2/messages", () => posted.promise);
    const sockets = setup(server, "#/sessions/s2");
    live(sockets);
    const box = screen.getByRole("textbox", { name: "Message" });
    await userEvent.type(box, "first");
    await userEvent.click(screen.getByRole("button", { name: "Send" }));
    await userEvent.type(box, " and more");
    await act(async () => posted.resolve({ status: 200, body: { id: "q1", queued: false } }));
    expect(box).toHaveValue("first and more");
  });

  it("does not send blank text", async () => {
    const server = new FakeServer().on("GET", "/api/v1/sessions/s2/messages?limit=50", page([]));
    const sockets = setup(server, "#/sessions/s2");
    live(sockets);
    await userEvent.type(screen.getByRole("textbox", { name: "Message" }), "   ");
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  });

  it("queues while the agent works, and the send becomes a user message once pasted", async () => {
    const server = new FakeServer()
      .on("GET", newest, page([]))
      .on("POST", "/api/v1/sessions/s1/messages", { status: 200, body: { id: "m2", queued: true } });
    const sockets = setup(server);
    const state = goldenState();
    live(sockets, state);
    const queued = within(screen.getByRole("list", { name: "Queued" }));
    expect(queued.getByText("and add a test")).toBeInTheDocument();
    await userEvent.type(screen.getByRole("textbox", { name: "Message" }), "then update the docs");
    await userEvent.click(screen.getByRole("button", { name: "Queue" }));
    const second = { id: "m2", session: "s1", text: "then update the docs", queued_at: "2026-10-03T12:31:00Z" };
    sockets.last.push({ diff: { seq: 50, sends: [...state.sends, second] } });
    expect(queued.getByText("then update the docs")).toBeInTheDocument();
    sockets.last.push({ diff: { seq: 51, sends: [second] } });
    sockets.last.push({ transcript: { session: "s1", messages: [msg("u9", 300, { role: "user", text: "and add a test" })] } });
    expect(queued.queryByText("and add a test")).not.toBeInTheDocument();
    expect(within(screen.getByRole("log", { name: "Messages" })).getByText("and add a test")).toBeInTheDocument();
  });

  it("only lists this session's queued sends", async () => {
    const server = new FakeServer().on("GET", newest, page([]));
    const sockets = setup(server);
    const state = goldenState();
    live(sockets, { ...state, sends: [...state.sends, { id: "m5", session: "s2", text: "elsewhere", queued_at: "2026-10-03T12:00:00Z" }] });
    expect(screen.queryByText("elsewhere")).not.toBeInTheDocument();
  });

  it("drops a queued send", async () => {
    const server = new FakeServer().on("GET", newest, page([])).on("DELETE", "/api/v1/sessions/s1/sends/m1", { status: 200, body: {} });
    const sockets = setup(server);
    live(sockets);
    await userEvent.click(screen.getByRole("button", { name: "Drop: and add a test" }));
    await waitFor(() => expect(server.calls.some((c) => c.method === "DELETE")).toBe(true));
  });

  it("edits a queued send by taking it back into the composer", async () => {
    const server = new FakeServer().on("GET", newest, page([])).on("DELETE", "/api/v1/sessions/s1/sends/m1", { status: 200, body: {} });
    const sockets = setup(server);
    live(sockets);
    await userEvent.click(screen.getByRole("button", { name: "Edit: and add a test" }));
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue("and add a test"));
    expect(server.calls.filter((c) => c.method === "DELETE").map((c) => c.path)).toEqual(["/api/v1/sessions/s1/sends/m1"]);
  });

  it("says so when a send went out before it could be edited", async () => {
    const server = new FakeServer()
      .on("GET", newest, page([]))
      .on("DELETE", "/api/v1/sessions/s1/sends/m1", { status: 404, body: { error: { code: "not_found", message: "no queued send m1" } } });
    const sockets = setup(server);
    live(sockets);
    await userEvent.click(screen.getByRole("button", { name: "Edit: and add a test" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("already sent");
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue("");
  });

  it("shows why a send failed and keeps the text", async () => {
    const server = new FakeServer()
      .on("GET", "/api/v1/sessions/s2/messages?limit=50", page([]))
      .on("POST", "/api/v1/sessions/s2/messages", { status: 404, body: { error: { code: "not_found", message: "no session s2" } } });
    const sockets = setup(server, "#/sessions/s2");
    live(sockets);
    const box = screen.getByRole("textbox", { name: "Message" });
    await userEvent.type(box, "hello");
    await userEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("no session s2");
    expect(box).toHaveValue("hello");
  });
});

describe("interrupt", () => {
  it("asks first and does nothing when cancelled", async () => {
    const server = new FakeServer().on("GET", newest, page([]));
    const sockets = setup(server);
    live(sockets);
    await userEvent.click(screen.getByRole("button", { name: "Interrupt" }));
    const dialog = within(screen.getByRole("alertdialog", { name: "Interrupt this session?" }));
    await userEvent.click(dialog.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(server.calls.some((c) => c.path.endsWith("/interrupt"))).toBe(false);
  });

  it("stops a working session once confirmed", async () => {
    const server = new FakeServer().on("GET", newest, page([])).on("POST", "/api/v1/sessions/s1/interrupt", { status: 200, body: {} });
    const sockets = setup(server);
    live(sockets);
    await userEvent.click(screen.getByRole("button", { name: "Interrupt" }));
    const dialog = within(screen.getByRole("alertdialog", { name: "Interrupt this session?" }));
    await userEvent.click(dialog.getByRole("button", { name: "Interrupt" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
    const call = server.calls.findIndex((c) => c.path === "/api/v1/sessions/s1/interrupt");
    expect(server.calls[call]?.method).toBe("POST");
    expect(server.headers[call].authorization).toBe("Bearer t0k");
  });

  it("is offered only while the agent works", async () => {
    const server = new FakeServer().on("GET", "/api/v1/sessions/s2/messages?limit=50", page([]));
    const sockets = setup(server, "#/sessions/s2");
    live(sockets);
    expect(screen.queryByRole("button", { name: "Interrupt" })).not.toBeInTheDocument();
  });
});
