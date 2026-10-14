import { useEffect, useState, type FormEvent } from "react";
import { listWorkspaces, resolveWorkItem, startSession, type WorkItemResolved, type WorkspaceList } from "../api";
import type { AppEnv } from "../App";
import { effortChoices, harnesses, loadChoices, modelChoices, saveChoices } from "../newsession";

type Resolution = { state: "idle" } | { state: "checking" } | { state: "ok"; item: WorkItemResolved } | { state: "bad"; message: string };

type Workspaces = { state: "loading" } | { state: "ready"; list: WorkspaceList } | { state: "failed"; message: string };

const checkDelay = 350;

function baseName(root: string): string {
  const parts = root.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? root;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function NewSession({ env, token }: { env: AppEnv; token: string }) {
  const [remembered] = useState(() => loadChoices(env.storage));
  const [workspaces, setWorkspaces] = useState<Workspaces>({ state: "loading" });
  const [workspace, setWorkspace] = useState(remembered.workspace);
  const [item, setItem] = useState("");
  const [prompt, setPrompt] = useState("");
  const [harness, setHarness] = useState(remembered.harness);
  const [model, setModel] = useState(remembered.model);
  const [effort, setEffort] = useState(remembered.effort);
  const [resolution, setResolution] = useState<Resolution>({ state: "idle" });
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    listWorkspaces(env.fetch, token).then(
      (list) => {
        if (!live) {
          return;
        }
        setWorkspaces({ state: "ready", list });
        setWorkspace((current) => (list.workspaces.some((w) => w.Root === current) ? current : list.last_used || list.workspaces[0]?.Root || ""));
      },
      (err) => live && setWorkspaces({ state: "failed", message: message(err) }),
    );
    return () => {
      live = false;
    };
  }, [env.fetch, token, attempt]);

  useEffect(() => {
    if (!item.trim() || !workspace) {
      setResolution({ state: "idle" });
      return;
    }
    let live = true;
    setResolution({ state: "checking" });
    const timer = setTimeout(() => {
      resolveWorkItem(env.fetch, token, workspace, item.trim()).then(
        (resolved) => live && setResolution({ state: "ok", item: resolved }),
        (err) => live && setResolution({ state: "bad", message: message(err) }),
      );
    }, checkDelay);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [env.fetch, token, workspace, item]);

  function chooseHarness(next: string) {
    setHarness(next);
    if (model && !(modelChoices[next] ?? []).includes(model)) {
      setModel("");
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setFailure("");
    try {
      const body = {
        workspace,
        work_item: item.trim(),
        harness,
        ...(model.trim() ? { model: model.trim() } : {}),
        ...(effort ? { effort } : {}),
        ...(prompt.trim() ? { prompt: prompt.trim() } : {}),
      };
      const session = await startSession(env.fetch, token, body);
      saveChoices(env.storage, { workspace, harness, model: model.trim(), effort });
      window.location.hash = "#/sessions/" + encodeURIComponent(session.ID);
    } catch (err) {
      setFailure(message(err));
      setBusy(false);
    }
  }

  const roots = workspaces.state === "ready" ? workspaces.list.workspaces : [];
  const canStart = !busy && !!workspace && resolution.state !== "bad";

  return (
    <main className="screen">
      <a className="back" href="#/">
        <span aria-hidden="true">{"‹ "}</span>
        Sessions
      </a>
      <h1>New session</h1>
      {workspaces.state === "failed" ? (
        <div className="error" role="alert">
          <p>{"Could not load workspaces: " + workspaces.message}</p>
          <button type="button" className="retry" onClick={() => {
              setWorkspaces({ state: "loading" });
              setAttempt((n) => n + 1);
            }}>
            Retry now
          </button>
        </div>
      ) : null}
      {workspaces.state === "ready" && roots.length === 0 ? (
        <p className="muted">
          No workspaces yet. Run <code>agentws workspace add</code> on your computer.
        </p>
      ) : null}
      <form onSubmit={submit}>
        <label>
          Workspace
          <select value={workspace} onChange={(e) => setWorkspace(e.target.value)} disabled={roots.length === 0}>
            {roots.map((w) => (
              <option key={w.Root} value={w.Root}>
                {baseName(w.Root) + " · " + w.Root}
              </option>
            ))}
          </select>
        </label>
        <label>
          Task (optional)
          <input
            value={item}
            onChange={(e) => setItem(e.target.value)}
            placeholder="PR URL, or what to do"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            aria-describedby="resolution"
          />
        </label>
        <p className="muted">Groups sessions and names them. Empty: the first prompt names the session.</p>
        <div id="resolution" className="resolution">
          {resolution.state === "checking" ? <p className="muted">Checking…</p> : null}
          {resolution.state === "ok" ? (
            <dl className="resolved">
              {resolution.item.title ? (
                <>
                  <dt>Title</dt>
                  <dd>{resolution.item.title}</dd>
                </>
              ) : null}
              <dt>Worktree</dt>
              <dd className="mono">{resolution.item.worktree}</dd>
            </dl>
          ) : null}
          {resolution.state === "bad" ? (
            <div className="error" role="alert">
              {resolution.message}
            </div>
          ) : null}
        </div>
        <label>
          First prompt
          <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} placeholder="Optional" />
        </label>
        <fieldset className="harness">
          <legend>Agent</legend>
          {harnesses.map((h) => (
            <label key={h.id} className="choice">
              <input type="radio" name="harness" value={h.id} checked={harness === h.id} onChange={() => chooseHarness(h.id)} />
              {h.label}
            </label>
          ))}
        </fieldset>
        <label>
          Model
          <input
            value={model}
            onChange={(e) => setModel(e.target.value)}
            list="model-choices"
            placeholder="Default"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
          />
          <datalist id="model-choices">
            {(modelChoices[harness] ?? []).map((m) => (
              <option key={m} value={m} />
            ))}
          </datalist>
        </label>
        <label>
          Effort
          <select value={effort} onChange={(e) => setEffort(e.target.value)}>
            <option value="">Default</option>
            {effortChoices.map((e) => (
              <option key={e} value={e}>
                {e}
              </option>
            ))}
          </select>
        </label>
        {failure ? (
          <div className="error" role="alert">
            <pre>{failure}</pre>
          </div>
        ) : null}
        <button type="submit" disabled={!canStart}>
          {busy ? "Starting…" : "Start session"}
        </button>
      </form>
    </main>
  );
}
