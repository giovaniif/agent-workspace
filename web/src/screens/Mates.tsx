import { type FormEvent, useCallback, useEffect, useRef, useState } from "react";
import {
  type Authed,
  type Mate,
  type MateClaim,
  type MateSetup,
  claimMate,
  listMates,
  mateSetup,
  pauseMate,
  saveMate,
  setMateTokens,
  stopAllMates,
} from "../api";
import type { MateRow, Mates as MateBoard } from "../stream";
import { ApprovalList } from "./Approvals";

function words(s: string): string {
  return s.replaceAll("_", " ");
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function stateLine(row: MateRow): string {
  const parts = [words(row.state)];
  if (row.guard) {
    parts.push("held by " + words(row.guard));
  }
  return parts.join(" · ");
}

function KillSwitch({ api, stopped }: { api: Authed; stopped: boolean }) {
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");

  async function flip(stop: boolean) {
    setBusy(true);
    setNote("");
    try {
      await stopAllMates(api, stop);
      setAsking(false);
    } catch (err) {
      setNote(message(err));
    } finally {
      setBusy(false);
    }
  }

  if (stopped) {
    return (
      <section className="kill-switch stopped" aria-label="Kill switch">
        <strong>All mates are stopped.</strong>
        <p className="muted">No mate starts a new turn.</p>
        <button type="button" disabled={busy} onClick={() => flip(false)}>
          Start all mates
        </button>
        {note && <p role="alert">{note}</p>}
      </section>
    );
  }
  return (
    <section className="kill-switch" aria-label="Kill switch">
      {asking ? (
        <>
          <p>Stop all mates? They start no new turns until you start them again.</p>
          <div className="kill-actions">
            <button type="button" disabled={busy} onClick={() => setAsking(false)}>
              Cancel
            </button>
            <button type="button" className="danger" disabled={busy} onClick={() => flip(true)}>
              Stop all
            </button>
          </div>
        </>
      ) : (
        <button type="button" className="danger" onClick={() => setAsking(true)}>
          Stop all mates
        </button>
      )}
      {note && <p role="alert">{note}</p>}
    </section>
  );
}

function MateForm({ api, start, isNew, onSaved }: { api: Authed; start: Mate; isNew: boolean; onSaved: (m: Mate) => void }) {
  const [mate, setMate] = useState<Mate>(start);
  const [repos, setRepos] = useState((start.repos ?? []).join(", "));
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setNote("");
    const list = repos
      .split(",")
      .map((r) => r.trim())
      .filter((r) => r !== "");
    try {
      onSaved(await saveMate(api, { ...mate, repos: list.length > 0 ? list : undefined }));
    } catch (err) {
      setNote(message(err));
    } finally {
      setBusy(false);
    }
  }

  const field = (key: "model" | "effort") => (
    <label>
      {key === "model" ? "Model" : "Effort"}
      <input value={mate[key] ?? ""} onChange={(e) => setMate({ ...mate, [key]: e.target.value || undefined })} />
    </label>
  );
  return (
    <form className="mate-form" onSubmit={submit} aria-label={isNew ? "New mate" : "Settings of " + start.name}>
      {isNew && (
        <label>
          Name
          <input value={mate.name} onChange={(e) => setMate({ ...mate, name: e.target.value.trim() })} />
        </label>
      )}
      <label>
        Agent
        <select value={mate.agent} onChange={(e) => setMate({ ...mate, agent: e.target.value })}>
          <option value="claude">claude</option>
          <option value="codex">codex</option>
        </select>
      </label>
      {field("model")}
      {field("effort")}
      <label>
        Repos
        <input value={repos} placeholder="api, web" onChange={(e) => setRepos(e.target.value)} />
      </label>
      <label>
        Prompt
        <textarea rows={5} value={mate.prompt} onChange={(e) => setMate({ ...mate, prompt: e.target.value })} />
      </label>
      <button type="submit" disabled={busy || mate.name === "" || mate.prompt.trim() === ""}>
        {isNew ? "Add mate" : "Save"}
      </button>
      {note && <p role="alert">{note}</p>}
    </form>
  );
}

function Connect({ api, name }: { api: Authed; name: string }) {
  const [setup, setSetup] = useState<MateSetup | null>(null);
  const [bot, setBot] = useState("");
  const [app, setApp] = useState("");
  const [claim, setClaim] = useState<MateClaim | null>(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");

  useEffect(() => {
    let live = true;
    mateSetup(api, name)
      .then((out) => live && setSetup(out))
      .catch((err) => live && setNote(message(err)));
    return () => {
      live = false;
    };
  }, [api, name]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setNote("");
    try {
      await setMateTokens(api, name, bot.trim(), app.trim());
      setBot("");
      setApp("");
      setClaim(await claimMate(api, name));
    } catch (err) {
      setNote(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="mate-connect" aria-label={"Connect " + name}>
      <ol>
        <li>
          {setup ? (
            <a href={setup.url} target="_blank" rel="noreferrer">
              Make the Slack app from the manifest
            </a>
          ) : (
            "Make the Slack app from the manifest"
          )}
        </li>
        <li>Install the app in your workspace. A workspace admin may need to approve it first.</li>
        <li>Copy the bot token (xoxb-) from OAuth &amp; Permissions.</li>
        <li>Make an app-level token (xapp-) with the scope connections:write in Basic Information, and copy it.</li>
      </ol>
      {claim ? (
        <p role="status">
          To become the owner of {name}, send this code to @{name} in a direct message in Slack in the next 10 minutes: <code>{claim.code}</code>
        </p>
      ) : (
        <form onSubmit={submit}>
          <label>
            Bot token
            <input type="password" autoComplete="off" value={bot} onChange={(e) => setBot(e.target.value)} />
          </label>
          <label>
            App token
            <input type="password" autoComplete="off" value={app} onChange={(e) => setApp(e.target.value)} />
          </label>
          <button type="submit" disabled={busy || bot.trim() === "" || app.trim() === ""}>
            Save tokens
          </button>
        </form>
      )}
      {note && <p role="alert">{note}</p>}
    </section>
  );
}

function MateCard({ api, row, mate, onChanged }: { api: Authed; row: MateRow; mate?: Mate; onChanged: () => void }) {
  const [editing, setEditing] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const paused = row.state === "paused";

  async function toggle() {
    setBusy(true);
    setNote("");
    try {
      await pauseMate(api, row.name, !paused);
      onChanged();
    } catch (err) {
      setNote(message(err));
    } finally {
      setBusy(false);
    }
  }

  const working = row.threads.filter((t) => row.work.includes(t.session_id));
  return (
    <li aria-label={row.name} className={"mate mate-" + row.state + (row.guard ? " mate-held" : "")}>
      <div className="mate-head">
        <strong>{row.name}</strong>
        <span className="mate-state">{stateLine(row)}</span>
        {mate && <span className="muted">{mate.agent + (mate.model ? " · " + mate.model : "")}</span>}
      </div>
      {row.error && <p className="mate-error">{row.error}</p>}
      <p className="muted">
        {working.length > 0 ? "Works on " + working.map((t) => t.session || t.session_id).join(", ") : "No current work."}
      </p>
      {row.threads.length > 0 && (
        <ul className="mate-threads" aria-label={"Threads of " + row.name}>
          {row.threads.map((t) => (
            <li key={t.channel + t.thread_ts}>
              <span className={"thread-state state-" + (t.ended ? "ended" : t.state)}>{t.ended ? "ended" : t.state}</span>
              <a href={"#/sessions/" + encodeURIComponent(t.session_id)}>{t.session || t.session_id}</a>
              {t.url && (
                <a href={t.url} target="_blank" rel="noreferrer">
                  Slack
                </a>
              )}
            </li>
          ))}
        </ul>
      )}
      <div className="mate-actions">
        <button type="button" disabled={busy} onClick={toggle}>
          {paused ? "Resume" : "Pause"}
        </button>
        {mate && (
          <button type="button" onClick={() => setEditing(!editing)}>
            {editing ? "Close settings" : "Settings"}
          </button>
        )}
        {row.state === "no_tokens" && (
          <button type="button" onClick={() => setConnecting(!connecting)}>
            Connect to Slack
          </button>
        )}
      </div>
      {note && <p role="alert">{note}</p>}
      {editing && mate && (
        <MateForm
          api={api}
          start={mate}
          isNew={false}
          onSaved={() => {
            setEditing(false);
            onChanged();
          }}
        />
      )}
      {connecting && <Connect api={api} name={row.name} />}
    </li>
  );
}

function AddMate({ api, onAdded }: { api: Authed; onAdded: () => void }) {
  const [open, setOpen] = useState(false);
  const [added, setAdded] = useState("");
  if (added) {
    return (
      <section className="mate-add" aria-label="Add a mate">
        <h2>Connect {added}</h2>
        <Connect api={api} name={added} />
        <button type="button" onClick={() => setAdded("")}>
          Done
        </button>
      </section>
    );
  }
  return (
    <section className="mate-add" aria-label="Add a mate">
      {open ? (
        <MateForm
          api={api}
          isNew
          start={{ name: "", agent: "claude", prompt: "", enabled: true }}
          onSaved={(m) => {
            setOpen(false);
            setAdded(m.name);
            onAdded();
          }}
        />
      ) : (
        <button type="button" onClick={() => setOpen(true)}>
          Add a mate
        </button>
      )}
    </section>
  );
}

export function Mates({ api, now, board }: { api: Authed; now: number; board?: MateBoard }) {
  const [defs, setDefs] = useState<Mate[]>([]);
  const [problems, setProblems] = useState<string[]>([]);
  const [error, setError] = useState("");

  const seq = useRef(0);

  const load = useCallback(() => {
    const mine = ++seq.current;
    listMates(api)
      .then((out) => {
        if (seq.current !== mine) {
          return;
        }
        setDefs(out.mates ?? []);
        setProblems(out.problems ?? []);
        setError("");
      })
      .catch((err) => seq.current === mine && setError(message(err)));
  }, [api]);

  useEffect(() => {
    load();
    return () => {
      seq.current++;
    };
  }, [load]);

  const rows = board?.mates ?? [];
  return (
    <main className="screen mates">
      <h1>Mates</h1>
      <KillSwitch api={api} stopped={board?.stopped ?? false} />
      {error && <p role="alert">{error}</p>}
      {problems.map((p) => (
        <p key={p} className="mate-error">
          {p}
        </p>
      ))}
      {board === undefined && <p>Loading…</p>}
      {board !== undefined && rows.length === 0 && <p>No mates yet.</p>}
      <ul className="mate-list">
        {rows.map((row) => (
          <MateCard key={row.name} api={api} row={row} mate={defs.find((d) => d.name === row.name)} onChanged={load} />
        ))}
      </ul>
      <AddMate api={api} onAdded={load} />
      <h2>Approvals</h2>
      <ApprovalList api={api} now={now} />
    </main>
  );
}
