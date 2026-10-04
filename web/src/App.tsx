import { useMemo, useState } from "react";
import type { Fetch } from "./api";
import { loadAuth, saveAuth, type Auth, type KeyValue } from "./auth";
import { pairCodeFromHash } from "./display";
import { Install } from "./screens/Install";
import { Pair } from "./screens/Pair";
import { Sessions } from "./screens/Sessions";

export type AppEnv = {
  fetch: Fetch;
  storage: KeyValue;
  installed: boolean;
  hash: string;
  host: string;
  userAgent: string;
};

export function App({ env }: { env: AppEnv }) {
  const [auth, setAuth] = useState<Auth | null>(() => loadAuth(env.storage));
  const code = useMemo(() => pairCodeFromHash(env.hash), [env.hash]);
  if (auth) {
    return <Sessions auth={auth} host={env.host} />;
  }
  if (!env.installed) {
    return <Install code={code} host={env.host} />;
  }
  return <Pair env={env} code={code} onPaired={(paired) => setAuth(saveAuth(env.storage, paired))} />;
}
