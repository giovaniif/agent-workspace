import { describe, expect, it } from "vitest";
import { buildFromWorkerLocation, cacheName, precacheList, requestKind, staleCaches } from "./sw-cache";

const origin = "https://agentws.example";

describe("the shell cache", () => {
  it("is keyed by the build the worker was registered for", () => {
    expect(cacheName(buildFromWorkerLocation(origin + "/sw.js?build=v2%2Babc"))).toBe("agentws-shell-v2+abc");
    expect(cacheName(buildFromWorkerLocation(origin + "/sw.js"))).toBe("agentws-shell-dev");
  });

  it("drops the caches of other builds and nothing else", () => {
    expect(staleCaches(["agentws-shell-v1", "agentws-shell-v2", "other-app"], "agentws-shell-v2")).toEqual(["agentws-shell-v1"]);
  });

  it("precaches the shell at / and every built file once", () => {
    expect(precacheList(["/assets/app-1.js", "/", "/assets/app-1.css"])).toEqual(["/", "/assets/app-1.js", "/assets/app-1.css"]);
  });
});

describe("requestKind", () => {
  it.each([
    ["GET", "navigate", origin + "/", "shell"],
    ["GET", "navigate", origin + "/sessions/abc", "shell"],
    ["GET", "no-cors", origin + "/assets/app-1.js", "file"],
    ["GET", "cors", origin + "/manifest.webmanifest", "file"],
    ["GET", "cors", origin + "/api/v1/hello", "network"],
    ["GET", "navigate", origin + "/api/v1/stream", "network"],
    ["GET", "cors", origin + "/api", "network"],
    ["POST", "cors", origin + "/api/v1/pair", "network"],
    ["GET", "cors", "https://elsewhere.example/x.js", "network"],
    ["GET", "same-origin", origin + "/sw.js?build=v2", "network"],
  ])("%s %s %s is %s", (method, mode, url, kind) => {
    expect(requestKind(method, mode, url, origin)).toBe(kind);
  });
});
