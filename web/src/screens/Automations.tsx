import { type FormEvent, useEffect, useState } from "react";
import { type Automation, type Authed, listAutomations, runAutomation } from "../api";

function trigger(a: Automation): string {
  const parts: string[] = [];
  if (a.schedule) {
    parts.push(a.schedule);
  }
  if (a.on && a.on.length > 0) {
    parts.push("on " + a.on.join(", "));
  }
  return parts.length > 0 ? parts.join(" · ") : "manual";
}

function RunForm({ api, automation, onDone }: { api: Authed; automation: Automation; onDone: () => void }) {
  const [context, setContext] = useState("");
  const [workItem, setWorkItem] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setNote("");
    try {
      const out = await runAutomation(api, automation.name, { context, work_item: workItem });
      if (out.run.session_id) {
        window.location.hash = "#/sessions/" + encodeURIComponent(out.run.session_id);
        return;
      }
      setNote(out.run.outcome + (out.run.reason ? ": " + out.run.reason : ""));
    } catch (err) {
      setNote(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="run-form" onSubmit={submit}>
      <label>
        Context
        <textarea value={context} onChange={(e) => setContext(e.target.value)} rows={4} />
      </label>
      <label>
        Work item
        <input value={workItem} onChange={(e) => setWorkItem(e.target.value)} />
      </label>
      <div className="run-actions">
        <button type="button" onClick={onDone}>
          Cancel
        </button>
        <button type="submit" disabled={busy}>
          Run
        </button>
      </div>
      {note && <p role="status">{note}</p>}
    </form>
  );
}

export function Automations({ api }: { api: Authed }) {
  const [list, setList] = useState<Automation[] | null>(null);
  const [error, setError] = useState("");
  const [running, setRunning] = useState("");

  useEffect(() => {
    let live = true;
    listAutomations(api)
      .then((out) => live && setList(out.automations ?? []))
      .catch((err) => live && setError(err instanceof Error ? err.message : String(err)));
    return () => {
      live = false;
    };
  }, [api]);

  return (
    <main className="automations">
      <h1>Automations</h1>
      {error && <p role="alert">{error}</p>}
      {list === null && !error && <p>Loading…</p>}
      {list !== null && list.length === 0 && <p>No automations. Add [[automation]] tables to config.toml on the server.</p>}
      <ul>
        {(list ?? []).map((a) => (
          <li key={a.source + a.name} aria-label={a.name}>
            <div className="automation-row">
              <strong>{a.name}</strong>
              <span>{a.agent}</span>
              <span>{trigger(a)}</span>
              <span>{a.enabled ? "enabled" : "paused"}</span>
              <button type="button" aria-label={"Run " + a.name} onClick={() => setRunning(a.name)}>
                Run…
              </button>
            </div>
            {running === a.name && <RunForm api={api} automation={a} onDone={() => setRunning("")} />}
          </li>
        ))}
      </ul>
    </main>
  );
}
