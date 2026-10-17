import { useEffect, useState } from "react";
import { type Approval, type Authed, ApiError, decideApproval, listApprovals } from "../api";

const ttlMs = 24 * 60 * 60 * 1000;

function words(kind: string): string {
  return kind.replaceAll("_", " ");
}

function expiresIn(at: string, now: number): string {
  const end = Date.parse(at) + ttlMs;
  if (Number.isNaN(end)) {
    return "";
  }
  const minutes = Math.max(0, Math.floor((end - now) / 60000));
  const hours = Math.floor(minutes / 60);
  return "expires in " + (hours > 0 ? hours + "h" : minutes + "m");
}

function Card({ api, approval, now }: { api: Authed; approval: Approval; now: number }) {
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState("");

  async function decide(approve: boolean) {
    setBusy(true);
    try {
      await decideApproval(api, approval.ID, approve);
      setDone(approve ? "Approved. The session tries it again." : "Denied. The session is told no.");
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setDone(err instanceof ApiError && (err.code === "stale" || err.code === "not_found") ? "This approval is gone: " + message : message);
    } finally {
      setBusy(false);
    }
  }

  const name = (approval.Mate || "crew") + ": " + words(approval.Action.Kind);
  return (
    <li aria-label={name}>
      <div className="approval-head">
        <strong>{approval.Mate || "crew"}</strong>
        <span>{words(approval.Action.Kind)}</span>
        <span>{expiresIn(approval.At, now)}</span>
      </div>
      <p className="muted">
        Asked by {approval.Requester || "someone"} with {approval.Action.Tool}
      </p>
      <pre className="approval-input">{approval.Action.Input}</pre>
      <a href={"#/sessions/" + encodeURIComponent(approval.SessionID)}>Open session</a>
      {done ? (
        <p role="status">{done}</p>
      ) : (
        <div className="approval-actions">
          <button type="button" className="deny" disabled={busy} onClick={() => decide(false)}>
            Deny
          </button>
          <button type="button" className="approve" disabled={busy} onClick={() => decide(true)}>
            Approve
          </button>
        </div>
      )}
    </li>
  );
}

export function ApprovalList({ api, now }: { api: Authed; now: number }) {
  const [list, setList] = useState<Approval[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    listApprovals(api)
      .then((out) => live && setList(out))
      .catch((err) => live && setError(err instanceof Error ? err.message : String(err)));
    return () => {
      live = false;
    };
  }, [api]);

  return (
    <>
      {error && <p role="alert">{error}</p>}
      {list === null && !error && <p>Loading…</p>}
      {list !== null && list.length === 0 && <p>No approvals wait for you.</p>}
      <ul className="approval-list">
        {(list ?? []).map((a) => (
          <Card key={a.ID} api={api} approval={a} now={now} />
        ))}
      </ul>
    </>
  );
}

export function Approvals({ api, now }: { api: Authed; now: number }) {
  return (
    <main className="screen approvals">
      <h1>Approvals</h1>
      <p className="muted">A mate asks here before a dangerous action that a person who is not the owner started.</p>
      <ApprovalList api={api} now={now} />
    </main>
  );
}
