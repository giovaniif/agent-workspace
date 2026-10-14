import { type FormEvent, useEffect, useState } from "react";
import { type Automation, type AutomationRun, type Authed, automationRuns, listAutomations, runAutomation } from "../api";

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

function lastRun(run: AutomationRun | undefined): string {
  if (!run) {
    return "never run";
  }
  return "last run " + run.outcome + (run.reason ? ": " + run.reason : "");
}

function when(start: string | undefined): string {
  if (!start) {
    return "";
  }
  const at = new Date(start);
  return Number.isNaN(at.getTime()) ? "" : at.toISOString().slice(0, 16).replace("T", " ") + " UTC";
}

function History({ name, runs }: { name: string; runs: AutomationRun[] }) {
  if (runs.length === 0) {
    return <p className="muted">No runs yet.</p>;
  }
  return (
    <ul className="runs" aria-label={"Runs of " + name}>
      {runs.map((r) => (
        <li key={r.id}>
          <span className={"outcome-" + r.outcome}>{r.outcome}</span>
          {r.trigger ? <span>{r.trigger}</span> : null}
          {when(r.start) ? <span>{when(r.start)}</span> : null}
          {r.reason ? <span>{r.reason}</span> : null}
          {r.session_id ? <a href={"#/sessions/" + encodeURIComponent(r.session_id)}>Open session</a> : null}
        </li>
      ))}
    </ul>
  );
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
  const [runs, setRuns] = useState<AutomationRun[]>([]);
  const [open, setOpen] = useState("");

  useEffect(() => {
    let live = true;
    listAutomations(api)
      .then((out) => live && setList(out.automations ?? []))
      .catch((err) => live && setError(err instanceof Error ? err.message : String(err)));
    automationRuns(api)
      .then((out) => live && setRuns(out.runs ?? []))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [api]);

  const runsOf = (name: string) => runs.filter((r) => r.automation === name);

  return (
    <main className="screen automations">
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
                Run with context
              </button>
            </div>
            <div className="automation-meta">
              <span className={runsOf(a.name)[0]?.outcome === "failed" ? "outcome-failed" : undefined}>{lastRun(runsOf(a.name)[0])}</span>
              <button type="button" className="history" aria-label={"History of " + a.name} aria-expanded={open === a.name} onClick={() => setOpen(open === a.name ? "" : a.name)}>
                History
              </button>
            </div>
            {open === a.name && <History name={a.name} runs={runsOf(a.name)} />}
            {running === a.name && <RunForm api={api} automation={a} onDone={() => setRunning("")} />}
          </li>
        ))}
      </ul>
    </main>
  );
}
