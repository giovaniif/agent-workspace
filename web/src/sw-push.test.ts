import { describe, expect, it } from "vitest";
import { notificationFor, openFromNotification, payloadOf, type WindowLike } from "./sw-push";

const origin = "https://agentws.example.ts.net";

function pushData(value: unknown) {
  return {
    json: () => (typeof value === "string" ? JSON.parse(value) : value),
    text: () => (typeof value === "string" ? value : JSON.stringify(value)),
  };
}

describe("push", () => {
  it("shows the title and body, tagged by session, and keeps the session URL", () => {
    const n = notificationFor(
      payloadOf(pushData({ title: "api · api@fix-login", body: "needs permission: Bash", url: "/#/sessions/s1", tag: "s1" })),
    );
    expect(n.title).toBe("api · api@fix-login");
    expect(n.options).toMatchObject({ body: "needs permission: Bash", tag: "s1", data: { url: "/#/sessions/s1" } });
  });

  it("is never silent", () => {
    for (const data of [null, pushData({}), pushData({ title: " ", body: "" }), pushData("not json")]) {
      const n = notificationFor(payloadOf(data));
      expect(n.title).not.toBe("");
      expect(n.options.body).not.toBe("");
    }
    expect(notificationFor(payloadOf(pushData("not json"))).options.body).toBe("not json");
    expect(notificationFor(payloadOf(null))).toMatchObject({ title: "crew", options: { body: "needs you", data: { url: "/" } } });
  });

  it("opens only this app's own pages", () => {
    for (const url of ["https://evil.example/", "//evil.example/x", "javascript:alert(1)", 42]) {
      expect(notificationFor(payloadOf(pushData({ title: "t", body: "b", url }))).options.data.url).toBe("/");
    }
  });
});

class FakeWindow implements WindowLike {
  focused = 0;
  navigated: string[] = [];
  constructor(
    readonly url: string,
    private readonly navigable = true,
  ) {}
  focus = async () => {
    this.focused++;
    return this;
  };
  navigate = async (url: string) => {
    if (!this.navigable) {
      throw new TypeError("not controlled");
    }
    this.navigated.push(url);
    return this;
  };
}

function fakeClients(windows: FakeWindow[]) {
  const opened: string[] = [];
  return {
    opened,
    matchAll: async () => windows,
    openWindow: async (url: string) => {
      opened.push(url);
      return null;
    },
  };
}

describe("notificationclick", () => {
  it("focuses the open app and shows the session's chat", async () => {
    const other = new FakeWindow("https://elsewhere.example/");
    const app = new FakeWindow(origin + "/#/settings");
    const clients = fakeClients([other, app]);
    await openFromNotification(clients, origin, "/#/sessions/s1");
    expect(app.focused).toBe(1);
    expect(app.navigated).toEqual([origin + "/#/sessions/s1"]);
    expect(other.focused).toBe(0);
    expect(clients.opened).toEqual([]);
  });

  it("opens the app at the session when none is open", async () => {
    const clients = fakeClients([new FakeWindow("https://elsewhere.example/")]);
    await openFromNotification(clients, origin, "/#/sessions/s1");
    expect(clients.opened).toEqual(["/#/sessions/s1"]);
  });

  it("opens the session in a new window when the open one cannot be navigated", async () => {
    const app = new FakeWindow(origin + "/", false);
    const clients = fakeClients([app]);
    await openFromNotification(clients, origin, "/#/sessions/s1");
    expect(clients.opened).toEqual(["/#/sessions/s1"]);
  });
});
