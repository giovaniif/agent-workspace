import { useEffect, useState } from "react";
import type { Fetch } from "../api";
import type { Auth } from "../auth";
import { disablePush, enablePush, type PushEnv } from "../push";
import { Brand } from "./Brand";

type Notify = { state: "idle" } | { state: "checking" } | { state: "busy" } | { state: "on"; error?: string } | { state: "denied" } | { state: "failed"; message: string };

export type SettingsProps = {
  auth: Auth;
  host: string;
  installed: boolean;
  fetch: Fetch;
  push: PushEnv;
  onSignOut: () => void | Promise<void>;
};

export function Settings({ auth, host, installed, fetch, push, onSignOut }: SettingsProps) {
  const [notify, setNotify] = useState<Notify>(() => {
    if (!push.supported) {
      return { state: "idle" };
    }
    const permission = push.permission();
    if (permission === "denied") {
      return { state: "denied" };
    }
    return permission === "granted" ? { state: "checking" } : { state: "idle" };
  });

  useEffect(() => {
    if (notify.state !== "checking") {
      return;
    }
    let live = true;
    push
      .subscribed()
      .catch(() => false)
      .then((on) => {
        if (live) {
          setNotify({ state: on ? "on" : "idle" });
        }
      });
    return () => {
      live = false;
    };
  }, [notify.state, push]);

  async function enable() {
    setNotify({ state: "busy" });
    try {
      setNotify({ state: await enablePush(push, fetch, auth.token) });
    } catch (err) {
      setNotify({ state: "failed", message: err instanceof Error ? err.message : String(err) });
    }
  }

  async function disable() {
    setNotify({ state: "busy" });
    const error = await disablePush(push, fetch, auth.token);
    setNotify(error === null ? { state: "idle" } : { state: "on", error });
  }

  return (
    <main className="screen">
      <Brand host={host} />
      <h1>Settings</h1>
      <section className="card" aria-label="Server">
        <h2>Server</h2>
        <dl className="server">
          <dt>Address</dt>
          <dd>{host}</dd>
        </dl>
      </section>
      <section className="card" aria-label="This device">
        <h2>This device</h2>
        <dl className="server">
          <dt>Name</dt>
          <dd>{auth.device.name}</dd>
          <dt>ID</dt>
          <dd>{auth.device.id}</dd>
        </dl>
      </section>
      <section className="card" aria-label="Notifications">
        <h2>Notifications</h2>
        {!installed && (
          <p className="muted">
            On iPhone and iPad, notifications work only in the app opened from the Home Screen. Add agentws to your Home
            Screen, then open it from there.
          </p>
        )}
        {!push.supported ? (
          <p className="muted">This browser cannot show notifications from agentws.</p>
        ) : (
          <>
            <p className="muted">Get a notification when a session needs you, waits for an answer or finishes.</p>
            <p className="muted">
              Held while you're typing at the terminal, and sent once if you step away with a session still waiting. Not
              sent while this app is open on screen.
            </p>
            {notify.state === "on" && <p>Notifications are on for this device.</p>}
            {notify.state === "denied" && (
              <p className="error">Notifications are blocked. Allow them for agentws in your device's settings, then try again.</p>
            )}
            {notify.state === "on" && notify.error && (
              <p className="error" role="alert">
                {notify.error}
              </p>
            )}
            {notify.state === "failed" && (
              <p className="error" role="alert">
                {notify.message}
              </p>
            )}
            {notify.state === "on" ? (
              <button type="button" className="secondary" onClick={disable}>
                Turn off
              </button>
            ) : (
              <button type="button" onClick={enable} disabled={notify.state === "busy" || notify.state === "checking"}>
                Enable notifications
              </button>
            )}
          </>
        )}
      </section>
      <button type="button" className="secondary" onClick={onSignOut}>
        Sign out
      </button>
    </main>
  );
}
