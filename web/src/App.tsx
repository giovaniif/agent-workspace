import { useEffect, useMemo, useState } from "react";
import type { Fetch } from "./api";
import { clearAuth, loadAuth, saveAuth, type Auth, type KeyValue } from "./auth";
import { pairCodeFromHash } from "./display";
import { disablePush, noPush, type PushEnv } from "./push";
import { parseRoute } from "./route";
import { Install } from "./screens/Install";
import { NewSession } from "./screens/NewSession";
import { Pair } from "./screens/Pair";
import { SessionScreen } from "./screens/Session";
import { SessionList } from "./screens/SessionList";
import { Settings } from "./screens/Settings";
import { useStream, useStreamClient, type OpenSocket } from "./stream";

export type AppEnv = {
  fetch: Fetch;
  storage: KeyValue;
  installed: boolean;
  hash: string;
  host: string;
  userAgent: string;
  openStream: OpenSocket;
  retryDelay?: (attempt: number) => number;
  now: () => number;
  onHashChange: (listener: (hash: string) => void) => () => void;
  push?: PushEnv;
  visibility?: PageVisibility;
};

export type PageVisibility = {
  visible: () => boolean;
  onChange: (listener: () => void) => () => void;
};

function Tabs({ settings }: { settings: boolean }) {
  return (
    <nav className="tabs" aria-label="Screens">
      <a href="#/" aria-current={settings ? undefined : "page"}>
        Sessions
      </a>
      <a href="#/settings" aria-current={settings ? "page" : undefined}>
        Settings
      </a>
    </nav>
  );
}

function useNow(now: () => number, every: number): number {
  const [value, setValue] = useState(now);
  useEffect(() => {
    setValue(now());
    const timer = setInterval(() => setValue(now()), every);
    return () => clearInterval(timer);
  }, [now, every]);
  return value;
}

type ConnectedProps = { env: AppEnv; auth: Auth; onUnauthorized: () => void; onSignOut: () => Promise<void> };

function Connected({ env, auth, onUnauthorized, onSignOut }: ConnectedProps) {
  const visibility = env.visibility;
  const client = useStreamClient({
    open: env.openStream,
    token: auth.token,
    retryDelay: env.retryDelay,
    now: env.now,
    visible: visibility?.visible,
  });
  const snapshot = useStream(client);
  const [hash, setHash] = useState(env.hash);
  const now = useNow(env.now, snapshot.status === "offline" ? 1000 : 30000);
  const { onHashChange, fetch } = env;
  const api = useMemo(() => ({ fetch, token: auth.token }), [fetch, auth.token]);

  useEffect(() => onHashChange(setHash), [onHashChange]);

  useEffect(() => visibility?.onChange(() => client.visibilityChanged()), [visibility, client]);

  useEffect(() => {
    if (snapshot.status === "unauthorized") {
      onUnauthorized();
    }
  }, [snapshot.status, onUnauthorized]);

  const route = parseRoute(hash);
  const retry = () => client.retryNow();
  if (route.screen === "new") {
    return <NewSession env={env} token={auth.token} />;
  }
  if (route.screen === "session") {
    return <SessionScreen key={route.id} id={route.id} snapshot={snapshot} now={now} onRetry={retry} client={client} api={api} />;
  }
  if (hash === "#/settings") {
    return (
      <>
        <Settings
          auth={auth}
          host={env.host}
          installed={env.installed}
          fetch={env.fetch}
          push={env.push ?? noPush}
          onSignOut={onSignOut}
        />
        <Tabs settings />
      </>
    );
  }
  return (
    <>
      <SessionList host={env.host} snapshot={snapshot} now={now} onRetry={retry} />
      <Tabs settings={false} />
    </>
  );
}

export function App({ env }: { env: AppEnv }) {
  const [auth, setAuth] = useState<Auth | null>(() => loadAuth(env.storage));
  const code = useMemo(() => pairCodeFromHash(env.hash), [env.hash]);
  const { storage } = env;
  const unpair = useMemo(
    () => () => {
      clearAuth(storage);
      setAuth(null);
    },
    [storage],
  );
  if (auth) {
    const signOut = async () => {
      await disablePush(env.push ?? noPush, env.fetch, auth.token);
      unpair();
    };
    return <Connected env={env} auth={auth} onUnauthorized={unpair} onSignOut={signOut} />;
  }
  if (!env.installed) {
    return <Install code={code} host={env.host} />;
  }
  return <Pair env={env} code={code} onPaired={(paired) => setAuth(saveAuth(env.storage, paired))} />;
}
