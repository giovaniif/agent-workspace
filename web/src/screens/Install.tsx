import { formatPairCode } from "../display";
import { Brand } from "./Brand";

export function Install({ code, host }: { code: string; host: string }) {
  return (
    <main className="screen">
      <Brand host={host} />
      <h1>Add agentws to your Home Screen</h1>
      <p className="lead">
        Pairing happens in the installed app. A Home Screen app keeps its own storage, so a device paired in this
        browser tab would not stay paired there.
      </p>
      {code && (
        <section className="card" aria-label="Pairing code">
          <h2>Pairing code</h2>
          <p className="code">{formatPairCode(code)}</p>
          <p className="muted">The app fills it in when you open it. It works once, for 5 minutes.</p>
        </section>
      )}
      <section className="card">
        <h2>iPhone and iPad</h2>
        <ol className="steps">
          <li>In Safari, tap the Share button.</li>
          <li>Choose Add to Home Screen, then Add.</li>
          <li>Open agentws from your Home Screen.</li>
        </ol>
      </section>
      <section className="card">
        <h2>Android</h2>
        <ol className="steps">
          <li>In Chrome, open the menu.</li>
          <li>Choose Install app or Add to Home screen.</li>
          <li>Open agentws from your Home Screen.</li>
        </ol>
      </section>
    </main>
  );
}
