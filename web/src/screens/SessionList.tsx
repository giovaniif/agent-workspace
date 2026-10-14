import { groupSessions, limitRows, sessionMeta, stateStyle, timeInState } from "../sessions";
import type { Session, StreamSnapshot } from "../stream";
import { Brand } from "./Brand";

export function sessionHref(id: string): string {
  return "#/sessions/" + encodeURIComponent(id);
}

export function sessionTitle(s: Session): string {
  return s.name || "Untitled session";
}

const harnessNames: Record<string, string> = { claude: "Claude", codex: "Codex" };

function Limits({ snapshot, now }: { snapshot: StreamSnapshot; now: number }) {
  const rows = limitRows(snapshot.state?.limits ?? [], now);
  if (rows.length === 0) {
    return null;
  }
  return (
    <div className="limits">
      {rows.map((row) => (
        <div key={row.harness} className="limit-row" role="group" aria-label={(harnessNames[row.harness] ?? row.harness) + " limits"}>
          <span className={"tag tag-" + row.harness}>{row.tag}</span>
          {row.windows.map((w) => (
            <span key={w.label} className={w.stale ? "limit stale" : "limit"}>
              <span>{w.label}</span>
              <span className="bar" aria-hidden="true">
                <span className={w.low ? "fill low" : "fill"} style={{ width: Math.min(Math.max(w.used, 0), 100) + "%" }} />
              </span>
              <span className={w.low ? "pct low" : "pct"}>{w.used}%</span>
              {w.resets ? <span className="reset">{"↻" + w.resets}</span> : null}
            </span>
          ))}
          {row.stale ? <span className="stale-age">{row.stale}</span> : null}
        </div>
      ))}
    </div>
  );
}

export function Connection({ snapshot, now, onRetry }: { snapshot: StreamSnapshot; now: number; onRetry: () => void }) {
  if (snapshot.status === "live" || snapshot.status === "unauthorized") {
    return null;
  }
  if (snapshot.status === "connecting") {
    return (
      <div className="connection" role="status">
        <span>Connecting…</span>
      </div>
    );
  }
  const seconds = snapshot.retryAt === null ? 0 : Math.max(0, Math.ceil((snapshot.retryAt - now) / 1000));
  return (
    <div className="connection offline" role="status">
      <span>{seconds > 0 ? "Offline · retrying in " + seconds + "s" : "Offline · retrying"}</span>
      <button type="button" className="retry" onClick={onRetry}>
        Retry now
      </button>
    </div>
  );
}

function Card({ session, now }: { session: Session; now: number }) {
  const age = timeInState(session.since, now);
  return (
    <li>
      <a className={"session-card state-" + session.State} href={sessionHref(session.ID)}>
        <span className="card-top">
          <span className="glyph" aria-hidden="true">
            {stateStyle(session.State).glyph}
          </span>
          <span className="name">{sessionTitle(session)}</span>
          {session.State === "done" && session.Unread ? <span className="badge">unread</span> : null}
          {age ? <span className="age">{age}</span> : null}
        </span>
        <span className="meta">{sessionMeta(session)}</span>
        {session.banner ? <span className="banner">{session.banner}</span> : null}
      </a>
    </li>
  );
}

export function SessionList({
  host,
  snapshot,
  now,
  onRetry,
}: {
  host: string;
  snapshot: StreamSnapshot;
  now: number;
  onRetry: () => void;
}) {
  const groups = groupSessions(snapshot.state?.sessions ?? []);
  return (
    <main className="screen">
      <header className="top">
        <Brand host={host} />
        <h1>Sessions</h1>
        <a className="new-session" href="#/new">
          New session
        </a>
        <Limits snapshot={snapshot} now={now} />
      </header>
      <Connection snapshot={snapshot} now={now} onRetry={onRetry} />
      {snapshot.state && groups.length === 0 ? (
        <section className="card empty">
          <p>No sessions yet</p>
          <p className="muted">Sessions you start in the terminal show up here.</p>
        </section>
      ) : null}
      {groups.map((group) => (
        <section key={group.key} className="group" aria-labelledby={"group-" + group.key}>
          <h2 id={"group-" + group.key}>
            <span>{group.title}</span>
            <span className="count" aria-hidden="true">
              {group.sessions.length}
            </span>
          </h2>
          <ul>
            {group.sessions.map((s) => (
              <Card key={s.ID} session={s} now={now} />
            ))}
          </ul>
        </section>
      ))}
    </main>
  );
}
