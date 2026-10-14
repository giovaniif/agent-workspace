import type { Harness, Quota, Session } from "./stream";

export type GroupKey = "needs" | "working" | "done" | "idle";

export type SessionGroup = {
  key: GroupKey;
  title: string;
  sessions: Session[];
};

const groupOf: Record<string, GroupKey> = {
  permission: "needs",
  waiting: "needs",
  running: "working",
  done: "done",
  idle: "idle",
};

const titles: Record<GroupKey, string> = {
  needs: "Needs you",
  working: "Working",
  done: "Done",
  idle: "Idle",
};

const order: GroupKey[] = ["needs", "working", "done", "idle"];

function sinceMs(s: Session): number | null {
  if (!s.since) {
    return null;
  }
  const t = Date.parse(s.since);
  return Number.isNaN(t) ? null : t;
}

function byTime(newestFirst: boolean) {
  return (a: Session, b: Session): number => {
    const ta = sinceMs(a);
    const tb = sinceMs(b);
    if (ta !== tb) {
      if (ta === null) {
        return 1;
      }
      if (tb === null) {
        return -1;
      }
      return newestFirst ? tb - ta : ta - tb;
    }
    return a.name.localeCompare(b.name) || a.ID.localeCompare(b.ID);
  };
}

function compare(key: GroupKey) {
  const oldest = byTime(false);
  const newest = byTime(true);
  return (a: Session, b: Session): number => {
    if (key === "needs" && a.State !== b.State) {
      return a.State === "permission" ? -1 : 1;
    }
    if (key === "done" && a.Unread !== b.Unread) {
      return a.Unread ? -1 : 1;
    }
    return key === "needs" ? oldest(a, b) : newest(a, b);
  };
}

export function groupSessions(sessions: Session[]): SessionGroup[] {
  const buckets: Record<GroupKey, Session[]> = { needs: [], working: [], done: [], idle: [] };
  for (const s of sessions) {
    if (s.Ended) {
      continue;
    }
    buckets[groupOf[s.State] ?? "idle"].push(s);
  }
  return order
    .filter((key) => buckets[key].length > 0)
    .map((key) => ({ key, title: titles[key], sessions: buckets[key].slice().sort(compare(key)) }));
}

function shortDuration(ms: number): string {
  const minutes = Math.floor(ms / 60000);
  if (minutes >= 24 * 60) {
    return Math.floor(minutes / (24 * 60)) + "d";
  }
  if (minutes >= 60) {
    const rest = minutes % 60;
    return Math.floor(minutes / 60) + "h" + (rest > 0 ? rest + "m" : "");
  }
  return minutes + "m";
}

export function timeInState(since: string | null, now: number): string {
  if (!since) {
    return "";
  }
  const t = Date.parse(since);
  if (Number.isNaN(t)) {
    return "";
  }
  const ms = now - t;
  return ms < 60000 ? "now" : shortDuration(ms);
}

export function sessionMeta(s: Session): string {
  return s.where ? s.where + " · " + s.Harness : String(s.Harness);
}

export type LimitWindow = {
  label: string;
  used: number;
  low: boolean;
  stale: boolean;
  resets: string;
};

export type LimitRow = {
  harness: Harness;
  tag: string;
  stale: string;
  windows: LimitWindow[];
};

const weekdays = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function resetClock(resetsAt: number, now: number): string {
  if (!resetsAt) {
    return "";
  }
  const at = new Date(resetsAt * 1000);
  if (at.getTime() - now >= 24 * 3600 * 1000) {
    return weekdays[at.getDay()];
  }
  return String(at.getHours()).padStart(2, "0") + ":" + String(at.getMinutes()).padStart(2, "0");
}

const harnesses: { harness: Harness; tag: string }[] = [
  { harness: "claude", tag: "CC" },
  { harness: "codex", tag: "CX" },
];

export function limitRows(limits: Quota[], now: number): LimitRow[] {
  const current = limits.filter((q) => !q.ResetsAt || q.ResetsAt * 1000 > now);
  const rows: LimitRow[] = [];
  for (const { harness, tag } of harnesses) {
    const mine = current.filter((q) => q.Harness === harness);
    if (mine.length === 0) {
      continue;
    }
    let oldest = 0;
    const windows = mine.map((q) => {
      const stale = now > Date.parse(q.stale_at);
      if (stale) {
        oldest = Math.max(oldest, now - Date.parse(q.ReportedAt));
      }
      return { label: q.label, used: 100 - q.LeftPercent, low: q.low, stale, resets: resetClock(q.ResetsAt, now) };
    });
    rows.push({ harness, tag, stale: oldest > 0 ? shortDuration(oldest) + " ago" : "", windows });
  }
  return rows;
}

const styles: Record<string, { glyph: string; label: string }> = {
  permission: { glyph: "◆", label: "Permission" },
  waiting: { glyph: "●", label: "Waiting" },
  running: { glyph: "◌", label: "Running" },
  done: { glyph: "✓", label: "Done" },
  idle: { glyph: "○", label: "Idle" },
};

export function stateStyle(state: string): { glyph: string; label: string } {
  return styles[state] ?? { glyph: "?", label: state };
}
