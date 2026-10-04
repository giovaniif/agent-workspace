import type { Auth } from "../auth";
import { Brand } from "./Brand";

export function Sessions({ auth, host }: { auth: Auth; host: string }) {
  return (
    <main className="screen">
      <Brand host={host} />
      <h1>Sessions</h1>
      <p className="muted">Paired as {auth.device.name}</p>
      <section className="card empty">
        <p>No sessions to show yet.</p>
        <p className="muted">The session list arrives in the next update of the app.</p>
      </section>
    </main>
  );
}
