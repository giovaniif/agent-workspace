import { useEffect, useState, type FormEvent } from "react";
import { addWorkspace, expandPath, listWorkspaces, removeWorkspace, resolveWorkItem, sessionOptions, startSession, type HarnessOptions, type WorkItemResolved, type WorkspaceList } from "../api";
import type { AppEnv } from "../App";
import { defaultLabel, effortChoices, harnesses, loadChoices, modelChoices, saveChoices } from "../newsession";

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
  const [defaults, setDefaults] = useState<HarnessOptions[]>([]);
  const [home, setHome] = useState("");
  const [adding, setAdding] = useState(false);
  const [newPath, setNewPath] = useState("");
  const [addError, setAddError] = useState("");
  const [removing, setRemoving] = useState(false);
  const [removeError, setRemoveError] = useState("");
  const [select, setSelect] = useState("");

  useEffect(() => {
    let live = true;
    sessionOptions(env.fetch, token).then(
      (options) => {
        if (live) {
          setDefaults(options.harnesses ?? []);
          setHome(options.home ?? "");
        }
      },
      () => undefined,
    );
    return () => {
      live = false;
    };
  }, [env.fetch, token]);

  async function submitWorkspace(e: FormEvent) {
    e.preventDefault();
    setAddError("");
    try {
      const added = await addWorkspace(env.fetch, token, expandPath(newPath, home));
      setSelect(added.Root);
      setAdding(false);
      setNewPath("");
      setAttempt((n) => n + 1);
    } catch (err) {
      setAddError(message(err));
    }
  }

  async function confirmRemove() {
    setRemoveError("");
    try {
      await removeWorkspace(env.fetch, token, workspace);
      setRemoving(false);
      setSelect("");
      setWorkspace("");
      setAttempt((n) => n + 1);
    } catch (err) {
      setRemoveError(message(err));
    }
  }

  useEffect(() => {
    let live = true;
    listWorkspaces(env.fetch, token).then(
      (list) => {
        if (!live) {
          return;
        }
        setWorkspaces({ state: "ready", list });
        setWorkspace((current) => {
          const has = (root: string) => list.workspaces.some((w) => w.Root === root);
          if (select && has(select)) {
            return select;
          }
          return has(current) ? current : list.last_used || list.workspaces[0]?.Root || "";
        });
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

  const harnessDefault = defaults.find((h) => h.harness === harness);
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
      {workspaces.state === "ready" && roots.length === 0 ? <p className="muted">No workspaces on this server yet. Add a folder of the server.</p> : null}
      <div className="workspace-tools">
        <button type="button" onClick={() => setAdding((v) => !v)}>
          Add workspace…
        </button>
        <button type="button" onClick={() => setRemoving(true)} disabled={!workspace}>
          Remove workspace
        </button>
      </div>
      {adding ? (
        <form className="add-workspace" onSubmit={submitWorkspace}>
          <label>
            Folder on the server
            <input
              value={newPath}
              onChange={(e) => setNewPath(e.target.value)}
              placeholder={home ? home + "/project" : "/home/me/project"}
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
            />
          </label>
          {newPath.trim() ? <p className="muted mono">{expandPath(newPath, home)}</p> : null}
          {addError ? (
            <div className="error" role="alert">
              {addError}
            </div>
          ) : null}
          <button type="submit" disabled={!newPath.trim()}>
            Add
          </button>
        </form>
      ) : null}
      {removing && workspace ? (
        <div className="confirm" role="alertdialog" aria-label={"Remove " + workspace + " from crew?"}>
          <p>{"Remove " + workspace + " from crew?"}</p>
          <p className="muted">crew stops using this workspace. No files, worktrees or branches on the server change.</p>
          {removeError ? (
            <div className="error" role="alert">
              {removeError}
            </div>
          ) : null}
          <button type="button" onClick={() => setRemoving(false)}>
            Cancel
          </button>
          <button type="button" aria-label={"Remove " + workspace} onClick={confirmRemove}>
            Remove
          </button>
        </div>
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
            placeholder={defaultLabel(harnessDefault?.default_model)}
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
            <option value="">{defaultLabel(harnessDefault?.default_effort)}</option>
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
