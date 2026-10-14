import { describe, expect, it } from "vitest";
import { clearAuth, loadAuth, saveAuth } from "./auth";
import { loadChoices } from "./newsession";
import { cacheName, staleCaches } from "./sw-cache";
import { MemoryStorage } from "./test/fake-server";

const device = { id: "k3m9p2qx", name: "phone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };

describe("the crew storage names", () => {
  it("copies an agentws login one time, so a paired phone stays paired", () => {
    const storage = new MemoryStorage();
    storage.setItem("agentws.auth", JSON.stringify({ token: "old", device }));
    expect(loadAuth(storage)?.token).toBe("old");
    expect(JSON.parse(storage.getItem("crew.auth") ?? "{}").token).toBe("old");
  });

  it("uses the crew login when both exist", () => {
    const storage = new MemoryStorage();
    storage.setItem("agentws.auth", JSON.stringify({ token: "old", device }));
    storage.setItem("crew.auth", JSON.stringify({ token: "new", device }));
    expect(loadAuth(storage)?.token).toBe("new");
  });

  it("saves the crew login and a sign out forgets both names", () => {
    const storage = new MemoryStorage();
    storage.setItem("agentws.auth", JSON.stringify({ token: "old", device }));
    saveAuth(storage, { token: "new", device });
    expect(JSON.parse(storage.getItem("crew.auth") ?? "{}").token).toBe("new");
    clearAuth(storage);
    expect(storage.getItem("crew.auth")).toBeNull();
    expect(storage.getItem("agentws.auth")).toBeNull();
    expect(loadAuth(storage)).toBeNull();
  });

  it("copies the new-session choices of agentws", () => {
    const storage = new MemoryStorage();
    storage.setItem("agentws.new", JSON.stringify({ workspace: "/home/me/api", harness: "codex", model: "", effort: "" }));
    expect(loadChoices(storage)).toMatchObject({ workspace: "/home/me/api", harness: "codex" });
    expect(storage.getItem("crew.new")).not.toBeNull();
  });

  it("names the shell cache crew and drops the agentws caches", () => {
    expect(cacheName("b1")).toBe("crew-shell-b1");
    expect(staleCaches(["agentws-shell-b0", "crew-shell-b0", "crew-shell-b1", "other"], "crew-shell-b1")).toEqual(["agentws-shell-b0", "crew-shell-b0"]);
  });
});
