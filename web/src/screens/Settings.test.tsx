import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { App, type AppEnv } from "../App";
import { loadAuth } from "../auth";
import { FakeServer, MemoryStorage } from "../test/fake-server";
import { FakeSockets } from "../test/fake-socket";

const device = { id: "k3m9p2qx", name: "work phone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };

function setup(hash: string) {
  const server = new FakeServer().on("GET", "/api/v1/hello", { status: 200, body: { api: "v1", build: "v0.12.0+abc" } });
  const storage = new MemoryStorage();
  storage.setItem("crew.auth", JSON.stringify({ token: "t0k", device }));
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash,
    host: "crew.example.ts.net",
    userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
    openStream: new FakeSockets().open,
    now: () => Date.parse("2026-10-03T12:00:00Z"),
    onHashChange: () => () => undefined,
  };
  return { storage, env };
}

describe("Settings", () => {
  it("is a tab next to the session list", () => {
    const { env } = setup("");
    render(<App env={env} />);
    expect(screen.getByRole("heading", { name: "Sessions" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute("href", "#/settings");
  });

  it("shows the server and this device", () => {
    const { env } = setup("#/settings");
    render(<App env={env} />);
    expect(screen.getByRole("heading", { name: "Settings" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Server" })).toHaveTextContent("crew.example.ts.net");
    expect(screen.getByRole("region", { name: "This device" })).toHaveTextContent("work phone");
    expect(screen.getByRole("link", { name: "Sessions" })).toHaveAttribute("href", "#/");
  });

  it("signs out by forgetting the token and going back to pairing", async () => {
    const { storage, env } = setup("#/settings");
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(await screen.findByRole("heading", { name: "Pair this device" })).toBeInTheDocument();
    expect(loadAuth(storage)).toBeNull();
  });
});
