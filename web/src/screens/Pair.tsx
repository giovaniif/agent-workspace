import { useEffect, useState, type FormEvent } from "react";
import { apiVersion, hello, pair, type Hello, type Paired } from "../api";
import type { AppEnv } from "../App";
import { defaultDeviceName, normalizePairCode } from "../display";
import { pairErrorMessage } from "../pairing";
import { Brand } from "./Brand";

type ServerInfo = { state: "loading" } | { state: "ready"; hello: Hello } | { state: "failed" };

export function Pair({ env, code, onPaired }: { env: AppEnv; code: string; onPaired: (paired: Paired) => void }) {
  const [server, setServer] = useState<ServerInfo>({ state: "loading" });
  const [value, setValue] = useState(code);
  const [name, setName] = useState(() => defaultDeviceName(env.userAgent));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    hello(env.fetch).then(
      (h) => live && setServer({ state: "ready", hello: h }),
      () => live && setServer({ state: "failed" }),
    );
    return () => {
      live = false;
    };
  }, [env.fetch]);

  const normalized = normalizePairCode(value);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      onPaired(await pair(env.fetch, normalized, name.trim() || defaultDeviceName(env.userAgent)));
    } catch (err) {
      setError(pairErrorMessage(err));
      setBusy(false);
    }
  }

  return (
    <main className="screen">
      <Brand host={env.host} />
      <h1>Pair this device</h1>
      <p className="lead">
        Enter the code from <code>agentws remote pair</code> on your computer.
      </p>
      <section className="card" aria-label="Server">
        <h2>Server</h2>
        <dl className="server">
          <dt>Address</dt>
          <dd>{env.host}</dd>
          <dt>API</dt>
          <dd>{server.state === "ready" ? apiVersion(server.hello) : server.state === "loading" ? "…" : "unreachable"}</dd>
          {server.state === "ready" && (
            <>
              <dt>Build</dt>
              <dd>{server.hello.build}</dd>
            </>
          )}
        </dl>
      </section>
      <form onSubmit={submit}>
        <label>
          Pairing code
          <input
            className="code-input"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            autoCapitalize="characters"
            autoComplete="one-time-code"
            autoCorrect="off"
            spellCheck={false}
            inputMode="text"
            placeholder="ABCD2345"
          />
        </label>
        <label>
          Device name
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
        </label>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy || normalized.length !== 8}>
          {busy ? "Pairing…" : "Pair"}
        </button>
      </form>
    </main>
  );
}
