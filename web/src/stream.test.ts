import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";
import { applyDiff, defaultRetryDelay, StreamClient, type Frame, type StreamState } from "./stream";
import { FakeSockets } from "./test/fake-socket";

const golden = (name: string) => JSON.parse(readFileSync(new URL("../../internal/serve/testdata/" + name, import.meta.url), "utf8"));
const goldenState = (): StreamState => (golden("stream-state.json") as Frame).state as StreamState;
const goldenDiffs = (): Frame[] => golden("stream-diffs.json") as Frame[];

function started(retryDelay: (attempt: number) => number = () => 5) {
  const sockets = new FakeSockets();
  const client = new StreamClient({ open: sockets.open, token: "t0k", retryDelay });
  client.start();
  return { sockets, client };
}

describe("applyDiff", () => {
  it("turns the golden state and diffs into the state the server holds", () => {
    let state = goldenState();
    for (const frame of goldenDiffs()) {
      state = applyDiff(state, frame.diff!);
    }
    expect(state.seq).toBe(48);
    expect(state.sessions.map((s) => [s.ID, s.State, s.name, s.where, s.banner])).toEqual([
      ["s1", "permission", "login fix", "api@login-v2", "needs permission: Bash: make test"],
    ]);
    expect(state.worktrees.map((w) => w.Branch)).toEqual(["login-v2"]);
    expect(state.tasks.find((t) => t.ID === "t1")?.PinnedName).toBe("login fix");
    expect(state.sends).toEqual([]);
    expect(state.limits.map((q) => [q.Harness, q.label, q.LeftPercent, q.low])).toEqual([["claude", "5h", 19, true]]);
  });

  it("does not change the state it was given", () => {
    const state = goldenState();
    const before = JSON.stringify(state);
    applyDiff(state, { seq: 99, removed_session: "s1" });
    expect(JSON.stringify(state)).toBe(before);
  });

  it("adds what it has not seen and removes workspaces and worktrees by key", () => {
    const state = goldenState();
    const added = applyDiff(state, { seq: 50, session: { ...state.sessions[0], ID: "s9" } });
    expect(added.sessions.map((s) => s.ID)).toEqual(["s1", "s2", "s9"]);
    const gone = applyDiff(applyDiff(state, { seq: 51, removed_worktree: "w1" }), { seq: 52, removed_workspace: "/home/me/api" });
    expect(gone.worktrees).toEqual([]);
    expect(gone.workspaces).toEqual([]);
  });
});

describe("defaultRetryDelay", () => {
  it("doubles from a second up to thirty", () => {
    expect([0, 1, 2, 3, 4, 5, 9].map(defaultRetryDelay)).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000]);
  });
});

describe("StreamClient", () => {
  it("sends the token first and is live once the state arrives", () => {
    const { sockets, client } = started();
    expect(client.snapshot().status).toBe("connecting");
    sockets.last.open();
    expect(sockets.last.sent).toEqual([{ token: "t0k" }]);
    sockets.last.push({ state: goldenState() });
    expect(client.snapshot().status).toBe("live");
    expect(client.snapshot().state?.sessions).toHaveLength(2);
  });

  it("applies diffs and tells subscribers", () => {
    const { sockets, client } = started();
    const seen = vi.fn();
    client.subscribe(seen);
    sockets.last.open();
    sockets.last.push({ state: goldenState() });
    sockets.last.push(goldenDiffs()[0]);
    expect(client.snapshot().state?.sessions.find((s) => s.ID === "s1")?.State).toBe("permission");
    expect(seen).toHaveBeenCalled();
  });

  it("goes offline when the connection drops, keeps the last state, and reconnects with the token", async () => {
    const { sockets, client } = started();
    sockets.last.open();
    sockets.last.push({ state: goldenState() });
    sockets.last.drop(1013, "the agentws daemon went away");
    expect(client.snapshot().status).toBe("offline");
    expect(client.snapshot().state?.sessions).toHaveLength(2);
    await vi.waitFor(() => expect(sockets.opened).toHaveLength(2));
    sockets.last.open();
    expect(sockets.last.sent).toEqual([{ token: "t0k" }]);
    sockets.last.push({ state: { ...goldenState(), sessions: [] } });
    expect(client.snapshot().status).toBe("live");
    expect(client.snapshot().state?.sessions).toEqual([]);
  });

  it("backs off longer after each failed try and starts over once live", async () => {
    const delays: number[] = [];
    const { sockets } = started((attempt) => {
      delays.push(attempt);
      return 1;
    });
    sockets.last.drop(1006);
    await vi.waitFor(() => expect(sockets.opened).toHaveLength(2));
    sockets.last.drop(1006);
    await vi.waitFor(() => expect(sockets.opened).toHaveLength(3));
    sockets.last.open();
    sockets.last.push({ state: goldenState() });
    sockets.last.drop(1011);
    await vi.waitFor(() => expect(sockets.opened).toHaveLength(4));
    expect(delays).toEqual([0, 1, 0]);
  });

  it("stops for good on 4401 once the server confirms it refuses the token", async () => {
    const sockets = new FakeSockets();
    const confirmRefused = vi.fn(async () => true);
    const client = new StreamClient({ open: sockets.open, token: "t0k", retryDelay: () => 5, confirmRefused });
    client.start();
    sockets.last.open();
    sockets.last.drop(4401, "device revoked");
    await vi.waitFor(() => expect(client.snapshot().status).toBe("unauthorized"));
    expect(confirmRefused).toHaveBeenCalledOnce();
    await new Promise((r) => setTimeout(r, 30));
    expect(sockets.opened).toHaveLength(1);
  });

  it("keeps retrying with its token when a 4401 is not confirmed", async () => {
    const sockets = new FakeSockets();
    const client = new StreamClient({ open: sockets.open, token: "t0k", retryDelay: () => 5, confirmRefused: async () => false });
    client.start();
    sockets.last.open();
    sockets.last.drop(4401, "no token in time");
    expect(client.snapshot().status).not.toBe("unauthorized");
    await vi.waitFor(() => expect(sockets.opened).toHaveLength(2));
    sockets.last.open();
    expect(sockets.last.sent).toEqual([{ token: "t0k" }]);
    expect(client.snapshot().status).toBe("connecting");
  });

  it("does nothing with a confirmation that arrives after it was stopped", async () => {
    const sockets = new FakeSockets();
    let answer: (refused: boolean) => void = () => undefined;
    const confirmRefused = () => new Promise<boolean>((resolve) => (answer = resolve));
    const client = new StreamClient({ open: sockets.open, token: "t0k", retryDelay: () => 5, confirmRefused });
    client.start();
    sockets.last.drop(4401);
    client.stop();
    answer(true);
    answer(false);
    await new Promise((r) => setTimeout(r, 30));
    expect(client.snapshot().status).not.toBe("unauthorized");
    expect(sockets.opened).toHaveLength(1);
  });

  it("passes every frame to frame listeners and sends frames only while live", () => {
    const { sockets, client } = started();
    const frames: Frame[] = [];
    client.onFrame((f) => frames.push(f));
    expect(client.send({ watch: "s1", after: 0 })).toBe(false);
    sockets.last.open();
    sockets.last.push({ state: goldenState() });
    expect(client.send({ watch: "s1", after: 0 })).toBe(true);
    sockets.last.push({ transcript: { session: "s1", messages: [] } });
    expect(sockets.last.sent).toEqual([{ token: "t0k" }, { watch: "s1", after: 0 }]);
    expect(frames.map((f) => Object.keys(f)[0])).toEqual(["state", "transcript"]);
  });

  it("retries at once when asked, and stop closes the socket without retrying", async () => {
    const { sockets, client } = started(() => 60000);
    sockets.last.drop(1006);
    client.retryNow();
    expect(sockets.opened).toHaveLength(2);
    client.stop();
    expect(sockets.last.closed).not.toBeNull();
    sockets.last.drop(1000);
    await new Promise((r) => setTimeout(r, 20));
    expect(sockets.opened).toHaveLength(2);
  });
});
