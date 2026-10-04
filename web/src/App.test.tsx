import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { App, type AppEnv } from "./App";
import { loadAuth } from "./auth";
import { FakeServer, MemoryStorage } from "./test/fake-server";

const device = { id: "k3m9p2qx", name: "iPhone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };
const iPhone = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)";

function setup(over: Partial<AppEnv> = {}) {
  const server = new FakeServer().on("GET", "/api/v1/hello", { status: 200, body: { api: "v1", build: "v0.12.0+abc" } });
  const storage = new MemoryStorage();
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash: "#pair=ABCD2345",
    host: "agentws.example.ts.net",
    userAgent: iPhone,
    ...over,
  };
  return { server, storage, env };
}

describe("in a browser tab", () => {
  it("explains Add to Home Screen and shows the code instead of pairing", () => {
    const { server, env } = setup({ installed: false });
    render(<App env={env} />);
    expect(screen.getByRole("heading", { name: "Add agentws to your Home Screen" })).toBeInTheDocument();
    expect(screen.getByText(/Add to Home Screen, then Add/)).toBeInTheDocument();
    expect(screen.getByLabelText("Pairing code")).toHaveTextContent("ABCD 2345");
    expect(screen.queryByRole("button", { name: "Pair" })).not.toBeInTheDocument();
    expect(server.calls.filter((c) => c.path === "/api/v1/pair")).toEqual([]);
  });

  it("still explains installing without a code in the URL", () => {
    const { env } = setup({ installed: false, hash: "" });
    render(<App env={env} />);
    expect(screen.getByRole("heading", { name: "Add agentws to your Home Screen" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Pairing code")).not.toBeInTheDocument();
  });
});

describe("in the installed app", () => {
  it("prefills the code from the URL and shows the server and its API version", async () => {
    const { env } = setup();
    render(<App env={env} />);
    expect(screen.getByRole("heading", { name: "Pair this device" })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Pairing code" })).toHaveValue("ABCD2345");
    expect(screen.getByRole("textbox", { name: "Device name" })).toHaveValue("iPhone");
    expect(screen.getByText("agentws.example.ts.net", { selector: "dd" })).toBeInTheDocument();
    expect(await screen.findByText("v1")).toBeInTheDocument();
    expect(screen.getByText("v0.12.0+abc")).toBeInTheDocument();
  });

  it("stores the token and lands on the session list once paired", async () => {
    const { server, storage, env } = setup();
    server.on("POST", "/api/v1/pair", { status: 200, body: { device, token: "t0k" } });
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Pair" }));
    expect(await screen.findByRole("heading", { name: "Sessions" })).toBeInTheDocument();
    expect(loadAuth(storage)).toEqual({ token: "t0k", device });
    expect(server.calls.find((c) => c.path === "/api/v1/pair")?.body).toEqual({ code: "ABCD2345", name: "iPhone" });
  });

  it("sends a typed code normalized, with the device name the user chose", async () => {
    const { server, env } = setup({ hash: "" });
    server.on("POST", "/api/v1/pair", { status: 200, body: { device, token: "t0k" } });
    render(<App env={env} />);
    const pairButton = screen.getByRole("button", { name: "Pair" });
    expect(pairButton).toBeDisabled();
    await userEvent.type(screen.getByRole("textbox", { name: "Pairing code" }), "wxyz-2345");
    await userEvent.clear(screen.getByRole("textbox", { name: "Device name" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Device name" }), "work phone");
    await userEvent.click(pairButton);
    await screen.findByRole("heading", { name: "Sessions" });
    expect(server.calls.find((c) => c.path === "/api/v1/pair")?.body).toEqual({ code: "WXYZ2345", name: "work phone" });
  });

  it.each([
    [401, "unauthorized", /wrong or has expired/],
    [429, "rate_limited", /Too many wrong codes/],
  ])("says why a %i %s answer failed and stays on pairing", async (status, code, why) => {
    const { server, storage, env } = setup();
    server.on("POST", "/api/v1/pair", { status, body: { error: { code, message: code } } });
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Pair" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(why);
    expect(screen.getByRole("heading", { name: "Pair this device" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pair" })).toBeEnabled();
    expect(loadAuth(storage)).toBeNull();
  });

  it("says when the server cannot be reached", async () => {
    const { server, env } = setup();
    server.on("GET", "/api/v1/hello", new TypeError("Load failed"));
    server.on("POST", "/api/v1/pair", new TypeError("Load failed"));
    render(<App env={env} />);
    expect(await screen.findByText("unreachable")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Pair" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/Can't reach the server/);
  });

  it("opens on the session list when a token is stored", () => {
    const { storage, env } = setup();
    storage.setItem("agentws.auth", JSON.stringify({ token: "t0k", device }));
    render(<App env={env} />);
    expect(screen.getByRole("heading", { name: "Sessions" })).toBeInTheDocument();
    expect(screen.getByText("Paired as iPhone")).toBeInTheDocument();
  });

  it("asks to pair again when the stored token is unreadable", () => {
    const { storage, env } = setup();
    storage.setItem("agentws.auth", "{not json");
    render(<App env={env} />);
    expect(screen.getByRole("heading", { name: "Pair this device" })).toBeInTheDocument();
  });
});
