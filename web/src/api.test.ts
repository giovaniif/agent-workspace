import { describe, expect, it } from "vitest";
import { ApiError, apiVersion, hello, pair } from "./api";
import { FakeServer } from "./test/fake-server";

const device = { id: "k3m9p2qx", name: "iPhone", created_at: "2026-10-03T10:00:00Z", last_seen: "2026-10-03T10:00:00Z" };

async function failure(promise: Promise<unknown>): Promise<ApiError> {
  try {
    await promise;
  } catch (err) {
    if (err instanceof ApiError) {
      return err;
    }
    throw err;
  }
  throw new Error("expected the call to fail");
}

describe("hello", () => {
  it("reads the API version and build without a token", async () => {
    const server = new FakeServer().on("GET", "/api/v1/hello", { status: 200, body: { api: "v1", build: "v0.12.0+abc" } });
    await expect(hello(server.fetch)).resolves.toEqual({ api: "v1", build: "v0.12.0+abc" });
  });

  it("names the API version with a v whatever the server sends", () => {
    expect(apiVersion({ api: "v1", build: "x" })).toBe("v1");
    expect(apiVersion({ api: 1 as unknown as string, build: "x" })).toBe("v1");
  });
});

describe("pair", () => {
  it("swaps the code and device name for a device and token", async () => {
    const server = new FakeServer().on("POST", "/api/v1/pair", { status: 200, body: { device, token: "t0k" } });
    await expect(pair(server.fetch, "ABCD2345", "iPhone")).resolves.toEqual({ device, token: "t0k" });
    expect(server.calls).toEqual([{ path: "/api/v1/pair", method: "POST", body: { code: "ABCD2345", name: "iPhone" } }]);
  });

  it.each([
    [401, { error: { code: "unauthorized", message: "the pairing code is wrong or has expired" } }, "unauthorized"],
    [429, { error: { code: "rate_limited", message: "too many failed pairing tries; wait a minute" } }, "rate_limited"],
    [401, { code: "unauthorized", message: "flat" }, "unauthorized"],
    [401, undefined, "unauthorized"],
    [429, undefined, "rate_limited"],
    [500, undefined, "failed"],
  ])("maps a %i answer to %j to the error code", async (status, body, code) => {
    const server = new FakeServer().on("POST", "/api/v1/pair", { status, body });
    const err = await failure(pair(server.fetch, "ABCD2345", "iPhone"));
    expect(err.status).toBe(status);
    expect(err.code).toBe(code);
  });

  it("keeps the server's message", async () => {
    const server = new FakeServer().on("POST", "/api/v1/pair", {
      status: 401,
      body: { error: { code: "unauthorized", message: "the pairing code is wrong or has expired" } },
    });
    expect((await failure(pair(server.fetch, "ABCD2345", "x"))).message).toBe("the pairing code is wrong or has expired");
  });

  it("reports an unreachable server as a network error", async () => {
    const server = new FakeServer().on("POST", "/api/v1/pair", new TypeError("Load failed"));
    const err = await failure(pair(server.fetch, "ABCD2345", "iPhone"));
    expect(err.code).toBe("network");
    expect(err.status).toBe(0);
  });

  it("survives an error page that is not JSON", async () => {
    const server = new FakeServer().on("POST", "/api/v1/pair", { status: 502, raw: "<html>Bad Gateway</html>" });
    const err = await failure(pair(server.fetch, "ABCD2345", "iPhone"));
    expect(err.code).toBe("failed");
    expect(err.message).toContain("Bad Gateway");
  });
});
