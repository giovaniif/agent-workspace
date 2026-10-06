import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { App, type AppEnv } from "./App";
import { decodeKey, type PushEnv } from "./push";
import { FakeServer, MemoryStorage } from "./test/fake-server";
import { FakeSockets } from "./test/fake-socket";

const device = { id: "k3m9p2qx", name: "iPhone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };
const vapidKey = "BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U";
const subscription = {
  endpoint: "https://web.push.apple.com/QGx3",
  expirationTime: null,
  keys: { p256dh: "BNcRdreALRFX", auth: "tBHItJI5svbpez7KI4CCXg" },
};

class FakePush implements PushEnv {
  supported = true;
  current: NotificationPermission = "default";
  answer: NotificationPermission = "granted";
  asked = 0;
  subscribedWith: Uint8Array[] = [];

  permission = () => this.current;

  requestPermission = async () => {
    this.asked++;
    this.current = this.answer;
    return this.answer;
  };

  subscribe = async (key: Uint8Array) => {
    this.subscribedWith.push(key);
    this.existing = true;
    return subscription;
  };

  existing = false;

  subscribed = async () => this.existing;

  unsubscribed = 0;

  unsubscribe = async () => {
    this.unsubscribed++;
    this.existing = false;
  };
}

function setup(over: Partial<AppEnv> = {}) {
  const server = new FakeServer()
    .on("GET", "/api/v1/push/key", { status: 200, body: { public_key: vapidKey } })
    .on("POST", "/api/v1/push/subscribe", { status: 200, body: {} })
    .on("POST", "/api/v1/push/unsubscribe", { status: 200, body: {} });
  const storage = new MemoryStorage();
  storage.setItem("agentws.auth", JSON.stringify({ token: "t0k", device }));
  const push = new FakePush();
  const env: AppEnv = {
    fetch: server.fetch,
    storage,
    installed: true,
    hash: "#/settings",
    host: "agentws.example.ts.net",
    userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
    openStream: new FakeSockets().open,
    now: () => Date.parse("2026-10-03T12:00:00Z"),
    onHashChange: () => () => undefined,
    push,
    ...over,
  };
  return { server, push, env };
}

describe("Enable notifications", () => {
  it("says notifications wait while the owner is at the terminal or has the app open", () => {
    const { env } = setup();
    render(<App env={env} />);
    const card = screen.getByRole("region", { name: "Notifications" });
    expect(card).toHaveTextContent("Held while you're typing at the terminal");
    expect(card).toHaveTextContent("sent once if you step away with a session still waiting");
    expect(card).toHaveTextContent("Not sent while this app is open on screen");
  });

  it("never asks for permission on load", () => {
    const { server, push, env } = setup();
    render(<App env={env} />);
    expect(screen.getByRole("button", { name: "Enable notifications" })).toBeEnabled();
    expect(push.asked).toBe(0);
    expect(server.calls).toEqual([]);
  });

  it("asks once tapped, then subscribes with the server's key and sends the subscription with the device token", async () => {
    const { server, push, env } = setup();
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Enable notifications" }));
    expect(await screen.findByText("Notifications are on for this device.")).toBeInTheDocument();
    expect(push.asked).toBe(1);
    expect(push.subscribedWith).toEqual([decodeKey(vapidKey)]);
    const sent = server.calls.findIndex((c) => c.path === "/api/v1/push/subscribe");
    expect(server.calls[sent]).toEqual({ path: "/api/v1/push/subscribe", method: "POST", body: subscription });
    expect(server.headers[sent].authorization).toBe("Bearer t0k");
    const key = server.calls.findIndex((c) => c.path === "/api/v1/push/key");
    expect(server.headers[key].authorization).toBe("Bearer t0k");
  });

  it("says how to unblock notifications when permission is denied, and subscribes nothing", async () => {
    const { server, push, env } = setup();
    push.answer = "denied";
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Enable notifications" }));
    expect(await screen.findByText(/Notifications are blocked/)).toBeInTheDocument();
    expect(push.subscribedWith).toEqual([]);
    expect(server.calls.filter((c) => c.path === "/api/v1/push/subscribe")).toEqual([]);
  });

  it("shows the server's refusal", async () => {
    const { server, env } = setup();
    server.on("POST", "/api/v1/push/subscribe", { status: 400, body: { error: { code: "bad_request", message: "the endpoint must be an https URL" } } });
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Enable notifications" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("the endpoint must be an https URL");
  });

  it("hints at the Home Screen outside the installed app", () => {
    const { env } = setup({ installed: false });
    render(<App env={env} />);
    expect(screen.getByText(/only in the app opened from the Home Screen/)).toBeInTheDocument();
  });

  it("explains a browser without push and offers no button", () => {
    const { push, env } = setup();
    push.supported = false;
    render(<App env={env} />);
    expect(screen.getByText(/cannot show notifications/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Enable notifications" })).not.toBeInTheDocument();
  });
});

describe("Notification state in Settings", () => {
  it("shows on, with Turn off and no Enable, when permission is granted and a subscription exists", async () => {
    const { push, env } = setup();
    push.current = "granted";
    push.existing = true;
    render(<App env={env} />);
    expect(await screen.findByText("Notifications are on for this device.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Turn off" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Enable notifications" })).not.toBeInTheDocument();
  });

  it("shows Enable when permission is granted but there is no subscription", async () => {
    const { push, env } = setup();
    push.current = "granted";
    render(<App env={env} />);
    expect(await screen.findByRole("button", { name: "Enable notifications" })).toBeEnabled();
    expect(screen.queryByText(/are on for this device/)).not.toBeInTheDocument();
  });

  it("does not look for a subscription before permission is granted", () => {
    const { push, env } = setup();
    push.existing = true;
    render(<App env={env} />);
    expect(screen.getByRole("button", { name: "Enable notifications" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Turn off" })).not.toBeInTheDocument();
  });

  it("swaps Enable for Turn off after enabling", async () => {
    const { env } = setup();
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Enable notifications" }));
    expect(await screen.findByRole("button", { name: "Turn off" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Enable notifications" })).not.toBeInTheDocument();
  });

  it("turns off on the server and in the browser, then offers Enable again", async () => {
    const { server, push, env } = setup();
    push.current = "granted";
    push.existing = true;
    render(<App env={env} />);
    await userEvent.click(await screen.findByRole("button", { name: "Turn off" }));
    expect(await screen.findByRole("button", { name: "Enable notifications" })).toBeEnabled();
    expect(server.calls.some((c) => c.path === "/api/v1/push/unsubscribe" && c.method === "POST")).toBe(true);
    expect(push.unsubscribed).toBe(1);
    expect(screen.queryByText(/are on for this device/)).not.toBeInTheDocument();
  });

  it("stays on with Turn off and an error when the browser refuses to unsubscribe", async () => {
    const { push, env } = setup();
    push.current = "granted";
    push.existing = true;
    push.unsubscribe = async () => {
      throw new Error("unsubscribe failed");
    };
    render(<App env={env} />);
    await userEvent.click(await screen.findByRole("button", { name: "Turn off" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("unsubscribe failed");
    expect(screen.getByRole("button", { name: "Turn off" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Enable notifications" })).not.toBeInTheDocument();
  });

  it("shows blocked, not on, when permission is denied", () => {
    const { push, env } = setup();
    push.current = "denied";
    push.existing = true;
    render(<App env={env} />);
    expect(screen.getByText(/Notifications are blocked/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Turn off" })).not.toBeInTheDocument();
  });
});

describe("Sign out", () => {
  it("drops this device's push subscription on the server and in the browser before forgetting the token", async () => {
    const { server, push, env } = setup();
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(await screen.findByRole("heading", { name: "Pair this device" })).toBeInTheDocument();
    const i = server.calls.findIndex((c) => c.path === "/api/v1/push/unsubscribe");
    expect(server.calls[i]?.method).toBe("POST");
    expect(server.headers[i].authorization).toBe("Bearer t0k");
    expect(push.unsubscribed).toBe(1);
  });

  it("still signs out when the server cannot be reached", async () => {
    const { server, env } = setup();
    server.on("POST", "/api/v1/push/unsubscribe", new TypeError("Failed to fetch"));
    render(<App env={env} />);
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(await screen.findByRole("heading", { name: "Pair this device" })).toBeInTheDocument();
  });
});

describe("decodeKey", () => {
  it("turns the base64url VAPID key into the 65 bytes of a P-256 point", () => {
    const key = decodeKey(vapidKey);
    expect(key).toHaveLength(65);
    expect(key[0]).toBe(4);
  });
});
