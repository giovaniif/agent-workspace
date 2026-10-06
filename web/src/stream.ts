import { useEffect, useMemo, useSyncExternalStore } from "react";

export type Harness = "claude" | "codex" | (string & {});

export type AgentState = "idle" | "running" | "waiting" | "permission" | "done";

export type Repo = {
  Name: string;
  Path: string;
  DefaultBranch: string;
  Branch: string;
  ChangedFiles: number;
};

export type Workspace = {
  Root: string;
  Kind: "single" | "orchestration" | (string & {});
  Repos: Repo[] | null;
  LastUsed: string;
};

export type Task = {
  ID: string;
  Source: string;
  Ref: string;
  Text: string;
  IssueTitle: string;
  PRTitle: string;
  PinnedName: string;
  URL: string;
};

export type PullRequest = {
  Number: number;
  Title: string;
  URL: string;
  Head: string;
  State: string;
  Checks: string;
  [field: string]: unknown;
};

export type Worktree = {
  ID: string;
  Repo: string;
  Path: string;
  Branch: string;
  PR: PullRequest | null;
  SubtaskSlug: string;
  SessionID: string;
  Ports: null;
};

export type RateLimit = {
  Window: string;
  UsedPercent: number;
  ResetsAt: number;
};

export type Switch = {
  Kind: string;
  Value: string;
  SentAt: string;
};

export type Session = {
  ID: string;
  TaskID: string;
  Harness: Harness;
  Pane: string;
  Model: string;
  Effort: string;
  State: AgentState;
  Ended: boolean;
  Unread: boolean;
  Focused: boolean;
  Muted: boolean;
  WorktreeIDs: string[] | null;
  ResumeID: string;
  Transcript: string;
  Dir: string;
  Usage: { ContextLeftPercent: number; HasContext: boolean; LimitUsedPercent: number };
  Limits: RateLimit[] | null;
  LimitsAt: string;
  Switches: Switch[] | null;
  SwitchWarning: boolean;
  name: string;
  where: string;
  banner: string;
  since: string | null;
};

export type Quota = {
  Harness: Harness;
  Window: string;
  LeftPercent: number;
  ResetsAt: number;
  ReportedAt: string;
  label: string;
  low: boolean;
  stale_at: string;
};

export type LaunchItem = {
  ID: string;
  Ref: string;
  URL: string;
  Workspace: string;
  Request: { Harness: string; Model: string; Effort: string };
  Starting: boolean;
  Err: string;
};

export type QueuedSend = {
  id: string;
  session: string;
  text: string;
  queued_at: string;
};

export type StreamState = {
  seq: number;
  workspaces: Workspace[];
  tasks: Task[];
  worktrees: Worktree[];
  sessions: Session[];
  limits: Quota[];
  queue: LaunchItem[];
  sends: QueuedSend[];
};

export type StreamDiff = {
  seq: number;
  removed_workspace?: string;
  removed_worktree?: string;
  removed_session?: string;
  workspace?: Workspace;
  task?: Task;
  worktree?: Worktree;
  session?: Session;
  limits?: Quota[];
  queue?: LaunchItem[];
  sends?: QueuedSend[];
};

export type Message = {
  id: string;
  cursor: number;
  turn?: string;
  role: "user" | "assistant" | "tool" | "system" | (string & {});
  text?: string;
  tool?: { name: string; summary?: string; status: "running" | "done" | "failed" | (string & {}) };
  at: string;
};

export type TranscriptFrame = {
  session: string;
  messages: Message[];
  reset?: boolean;
  closed?: boolean;
};

export type Frame = {
  state?: StreamState;
  diff?: StreamDiff;
  transcript?: TranscriptFrame;
  error?: { code: string; message: string };
  watch?: string;
};

export type ClientFrame = { watch: string; after: number } | { unwatch: string };

export type SocketLike = {
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: ((ev: Event) => void) | null;
  onmessage: ((ev: MessageEvent) => void) | null;
  onclose: ((ev: CloseEvent) => void) | null;
  onerror: ((ev: Event) => void) | null;
};

export type OpenSocket = () => SocketLike;

export type StreamStatus = "connecting" | "live" | "offline" | "unauthorized";

export type StreamSnapshot = {
  status: StreamStatus;
  state: StreamState | null;
  retryAt: number | null;
  reason: string;
};

export const closeUnauthorized = 4401;

export function defaultRetryDelay(attempt: number): number {
  return Math.min(30000, 1000 * 2 ** attempt);
}

function upsert<T>(list: T[], item: T, key: (x: T) => string): T[] {
  const id = key(item);
  const at = list.findIndex((x) => key(x) === id);
  if (at < 0) {
    return [...list, item];
  }
  const next = list.slice();
  next[at] = item;
  return next;
}

function remove<T>(list: T[], id: string | undefined, key: (x: T) => string): T[] {
  return id ? list.filter((x) => key(x) !== id) : list;
}

export function applyDiff(state: StreamState, diff: StreamDiff): StreamState {
  let { workspaces, tasks, worktrees, sessions } = state;
  if (diff.workspace) {
    workspaces = upsert(workspaces, diff.workspace, (w) => w.Root);
  }
  workspaces = remove(workspaces, diff.removed_workspace, (w) => w.Root);
  if (diff.task) {
    tasks = upsert(tasks, diff.task, (t) => t.ID);
  }
  if (diff.worktree) {
    worktrees = upsert(worktrees, diff.worktree, (w) => w.ID);
  }
  worktrees = remove(worktrees, diff.removed_worktree, (w) => w.ID);
  if (diff.session) {
    sessions = upsert(sessions, diff.session, (s) => s.ID);
  }
  sessions = remove(sessions, diff.removed_session, (s) => s.ID);
  return {
    seq: diff.seq,
    workspaces,
    tasks,
    worktrees,
    sessions,
    limits: diff.limits ?? state.limits,
    queue: diff.queue ?? state.queue,
    sends: diff.sends ?? state.sends,
  };
}

export type StreamOptions = {
  open: OpenSocket;
  token: string;
  retryDelay?: (attempt: number) => number;
  now?: () => number;
  confirmRefused?: () => Promise<boolean>;
};

export class StreamClient {
  private readonly opts: StreamOptions;
  private socket: SocketLike | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private attempt = 0;
  private running = false;
  private generation = 0;
  private current: StreamSnapshot = { status: "connecting", state: null, retryAt: null, reason: "" };
  private readonly listeners = new Set<() => void>();
  private readonly frameListeners = new Set<(frame: Frame) => void>();

  constructor(opts: StreamOptions) {
    this.opts = opts;
  }

  start(): void {
    if (this.running) {
      return;
    }
    this.running = true;
    this.connect();
  }

  stop(): void {
    this.running = false;
    this.generation++;
    this.clearTimer();
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      this.detach(socket);
      socket.close(1000, "closed by the app");
    }
  }

  retryNow(): void {
    if (!this.running || this.current.status !== "offline") {
      return;
    }
    this.clearTimer();
    this.connect();
  }

  snapshot = (): StreamSnapshot => this.current;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  onFrame(listener: (frame: Frame) => void): () => void {
    this.frameListeners.add(listener);
    return () => this.frameListeners.delete(listener);
  }

  send(frame: ClientFrame): boolean {
    if (!this.socket || this.current.status !== "live") {
      return false;
    }
    this.socket.send(JSON.stringify(frame));
    return true;
  }

  private connect(): void {
    this.update({ status: "connecting", retryAt: null });
    const socket = this.opts.open();
    this.socket = socket;
    socket.onopen = () => socket.send(JSON.stringify({ token: this.opts.token }));
    socket.onmessage = (ev) => this.receive(ev.data);
    socket.onclose = (ev) => this.closed(socket, ev.code, ev.reason);
    socket.onerror = () => undefined;
  }

  private receive(data: unknown): void {
    let frame: Frame;
    try {
      frame = JSON.parse(String(data)) as Frame;
    } catch {
      return;
    }
    if (frame.state) {
      this.attempt = 0;
      this.update({ status: "live", state: frame.state, reason: "" });
    } else if (frame.diff && this.current.state) {
      this.update({ state: applyDiff(this.current.state, frame.diff) });
    }
    for (const listener of this.frameListeners) {
      listener(frame);
    }
  }

  private closed(socket: SocketLike, code: number, reason: string): void {
    if (socket !== this.socket) {
      return;
    }
    this.detach(socket);
    this.socket = null;
    if (code === closeUnauthorized) {
      this.confirmUnauthorized(reason);
      return;
    }
    this.retryLater(reason);
  }

  private confirmUnauthorized(reason: string): void {
    const confirm = this.opts.confirmRefused;
    if (!confirm) {
      this.refused(reason);
      return;
    }
    this.update({ status: "connecting", retryAt: null, reason });
    const generation = this.generation;
    const settle = (refused: boolean) => {
      if (generation !== this.generation || !this.running) {
        return;
      }
      if (refused) {
        this.refused(reason);
      } else {
        this.retryLater(reason);
      }
    };
    confirm().then(settle, () => settle(false));
  }

  private refused(reason: string): void {
    this.running = false;
    this.update({ status: "unauthorized", retryAt: null, reason });
  }

  private retryLater(reason: string): void {
    if (!this.running) {
      return;
    }
    const delay = (this.opts.retryDelay ?? defaultRetryDelay)(this.attempt);
    this.attempt++;
    const now = this.opts.now ?? Date.now;
    this.update({ status: "offline", retryAt: now() + delay, reason });
    this.timer = setTimeout(() => {
      this.timer = null;
      if (this.running) {
        this.connect();
      }
    }, delay);
  }

  private detach(socket: SocketLike): void {
    socket.onopen = null;
    socket.onmessage = null;
    socket.onclose = null;
    socket.onerror = null;
  }

  private clearTimer(): void {
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
  }

  private update(patch: Partial<StreamSnapshot>): void {
    this.current = { ...this.current, ...patch };
    for (const listener of this.listeners) {
      listener();
    }
  }
}

export function useStreamClient(opts: StreamOptions): StreamClient {
  const { open, token, retryDelay, now, confirmRefused } = opts;
  const client = useMemo(
    () => new StreamClient({ open, token, retryDelay, now, confirmRefused }),
    [open, token, retryDelay, now, confirmRefused],
  );
  useEffect(() => {
    client.start();
    return () => client.stop();
  }, [client]);
  return client;
}

export function useStream(client: StreamClient): StreamSnapshot {
  return useSyncExternalStore(client.subscribe, client.snapshot, client.snapshot);
}
