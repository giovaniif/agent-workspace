import { readFileSync } from "node:fs";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { App, type AppEnv } from "../App";
import type { Frame, Message, Session, StreamState } from "../stream";
import { Deferred, FakeServer, MemoryStorage, type FakeHandler, type FakeRoute } from "../test/fake-server";
import { FakeSockets } from "../test/fake-socket";

const golden = (name: string) => JSON.parse(readFileSync(new URL("../../../internal/serve/testdata/" + name, import.meta.url), "utf8"));
const goldenState = (): StreamState => (golden("stream-state.json") as Frame).state as StreamState;
const device = { id: "k3m9p2qx", name: "iPhone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };
const halfPastNoon = new Date("2026-10-03T12:30:00Z").getTime();
const newest = "/api/v1/sessions/s1/messages?limit=50";
const promptPath = "/api/v1/sessions/s1/prompt";
const answerPath = "/api/v1/sessions/s1/answer";

const bashPrompt = golden("prompt.json");
const rawPrompt = golden("prompt-raw.json");

function page(messages: Message[]): FakeRoute {
  return { status: 200, body: { messages, before: 0 } };
}

function withSession(state: StreamState, over: Partial<Session>): StreamState {
  return { ...state, sessions: state.sessions.map((s) => (s.ID === "s1" ? { ...s, ...over } : s)) };
}

function setup(server: FakeServer) {
  window.location.hash = "#/sessions/s1";
  const sockets = new FakeSockets();
  const storage = new MemoryStorage();
  storage.setItem("crew.auth", JSON.stringify({ token: "t0k", device }));
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash: "#/sessions/s1",
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

function live(sockets: FakeSockets, over: Partial<Session>) {
  sockets.last.open();
  sockets.last.push({ state: withSession(goldenState(), over) });
}

let seq = 1000;

function move(sockets: FakeSockets, over: Partial<Session>) {
  const current = withSession(goldenState(), over).sessions.find((s) => s.ID === "s1") as Session;
  act(() => sockets.last.push({ diff: { seq: ++seq, session: current } }));
}

const asking: Partial<Session> = { State: "permission", since: "2026-10-03T12:01:00Z" };

function serverWith(prompt: FakeRoute | FakeHandler = { status: 200, body: bashPrompt }) {
  return new FakeServer().on("GET", newest, page([])).on("GET", promptPath, prompt).on("POST", answerPath, { status: 200, body: {} });
}

function posts(server: FakeServer, path: string) {
  return server.calls.filter((c) => c.method === "POST" && c.path === path);
}

beforeEach(() => {
  window.location.hash = "";
});

afterEach(() => {
  window.location.hash = "";
});

describe("the permission card", () => {
  it("shows the request and one button per choice while the session needs permission", async () => {
    const server = serverWith();
    live(setup(server), asking);
    expect(await screen.findByText(/touch notes\.txt/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Yes" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Yes, and don't ask again" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "No" })).toBeInTheDocument();
    expect(server.calls.filter((c) => c.path === promptPath)).toHaveLength(1);
  });

  it("is absent and fetches nothing while the session is running", async () => {
    const server = serverWith();
    live(setup(server), {});
    await waitFor(() => expect(server.calls.some((c) => c.path === newest)).toBe(true));
    expect(screen.queryByRole("button", { name: "Yes" })).not.toBeInTheDocument();
    expect(server.calls.some((c) => c.path === promptPath)).toBe(false);
  });

  it("disables the composer until the prompt is answered", async () => {
    const server = serverWith();
    live(setup(server), asking);
    await screen.findByRole("button", { name: "Yes" });
    expect(screen.getByRole("textbox", { name: "Message" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Queue" })).toBeDisabled();
  });

  it("posts the chosen id and says it was answered", async () => {
    const server = serverWith();
    const sockets = setup(server);
    live(sockets, asking);
    await userEvent.click(await screen.findByRole("button", { name: "Yes, and don't ask again" }));
    await waitFor(() => expect(posts(server, answerPath)).toHaveLength(1));
    expect(posts(server, answerPath)[0].body).toEqual({ choice: "2", prompt: "9f2c4a1b7d3e" });
    expect(await screen.findByText("Answered: Yes, and don't ask again")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Yes" })).not.toBeInTheDocument();
  });

  it("enables the composer again once the stream says the session moved on", async () => {
    const server = serverWith();
    const sockets = setup(server);
    live(sockets, asking);
    await screen.findByRole("button", { name: "Yes" });
    move(sockets, {});
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Message" })).toBeEnabled());
  });

  it("does not let a second tap send a second answer while one is in flight", async () => {
    const gate = new Deferred<FakeRoute>();
    const server = serverWith().on("POST", answerPath, () => gate.promise);
    live(setup(server), asking);
    await userEvent.click(await screen.findByRole("button", { name: "Yes" }));
    expect(screen.getByRole("button", { name: "No" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "No" }));
    gate.resolve({ status: 200, body: {} });
    await screen.findByText("Answered: Yes");
    expect(posts(server, answerPath)).toHaveLength(1);
  });

  it("says the prompt is gone on a 409 and offers no buttons", async () => {
    const server = serverWith().on("POST", answerPath, { status: 409, body: { error: { code: "stale", message: "session is no longer waiting for a permission" } } });
    live(setup(server), asking);
    await userEvent.click(await screen.findByRole("button", { name: "Yes" }));
    expect(await screen.findByText(/already answered/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Yes" })).not.toBeInTheDocument();
    expect(posts(server, answerPath)).toHaveLength(1);
  });

  it("says the prompt is gone and sends nothing when the stream moved on before the tap", async () => {
    const server = serverWith();
    const sockets = setup(server);
    live(sockets, asking);
    await screen.findByRole("button", { name: "Yes" });
    move(sockets, {});
    expect(await screen.findByText(/already answered/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Yes" })).not.toBeInTheDocument();
    expect(posts(server, answerPath)).toHaveLength(0);
  });

  it("says the prompt is gone when no dialog is showing any more", async () => {
    const server = serverWith({ status: 404, body: { error: { code: "not_found", message: "no permission dialog is showing" } } });
    live(setup(server), asking);
    expect(await screen.findByText(/already answered/i)).toBeInTheDocument();
  });

  it("shows a note and the raw pane text, with no buttons, when the dialog is not recognized", async () => {
    const server = serverWith({ status: 200, body: rawPrompt });
    live(setup(server), asking);
    expect(await screen.findByText("Couldn’t read this dialog")).toBeInTheDocument();
    const raw = screen.getByText(/Allow this unusual request\?/);
    expect(raw.tagName).toBe("PRE");
    expect(raw).toHaveTextContent("[a] allow [d] deny");
    expect(screen.queryByRole("button", { name: /allow|deny|Yes/ })).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Message" })).toBeDisabled();
  });

  it("offers Retry when the prompt cannot be loaded", async () => {
    let calls = 0;
    const server = serverWith(() => {
      calls++;
      return calls === 1 ? { status: 500, body: { error: { code: "failed", message: "capture failed" } } } : { status: 200, body: bashPrompt };
    });
    live(setup(server), asking);
    expect(await screen.findByText(/Couldn’t load the prompt: capture failed/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("button", { name: "Yes" })).toBeInTheDocument();
  });

  it("fetches the prompt again when the session re-enters permission", async () => {
    let calls = 0;
    const server = serverWith(() => {
      calls++;
      return { status: 200, body: calls === 1 ? bashPrompt : { text: "Edit file\n\nDo you want to proceed?", choices: [{ id: "1", label: "Yes, apply" }] } };
    });
    const sockets = setup(server);
    live(sockets, asking);
    await screen.findByRole("button", { name: "Yes" });
    move(sockets, {});
    await screen.findByText(/already answered/i);
    move(sockets, { State: "permission", since: "2026-10-03T12:09:00Z" });
    expect(await screen.findByRole("button", { name: "Yes, apply" })).toBeInTheDocument();
    expect(screen.queryByText(/already answered/i)).not.toBeInTheDocument();
  });

  it("ignores the outcome of an answer sent for an earlier prompt once a new one is showing", async () => {
    const gate = new Deferred<FakeRoute>();
    let calls = 0;
    const server = serverWith(() => {
      calls++;
      return { status: 200, body: calls === 1 ? bashPrompt : { id: "second", text: "Edit file\n\nDo you want to proceed?", choices: [{ id: "1", label: "Yes, apply" }] } };
    }).on("POST", answerPath, () => gate.promise);
    const sockets = setup(server);
    live(sockets, asking);
    await userEvent.click(await screen.findByRole("button", { name: "Yes" }));
    move(sockets, {});
    move(sockets, { State: "permission", since: "2026-10-03T12:09:00Z" });
    await screen.findByRole("button", { name: "Yes, apply" });
    await act(async () => {
      gate.resolve({ status: 200, body: {} });
    });
    expect(screen.getByRole("button", { name: "Yes, apply" })).toBeInTheDocument();
    expect(screen.queryByText(/^Answered:/)).not.toBeInTheDocument();
  });
});
