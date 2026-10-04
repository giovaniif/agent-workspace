import { describe, expect, it } from "vitest";
import { buildOfWorker, checkBuild, workerURL, type BuildDeps } from "./build";

function deps(served: string | null, reported: string, log: string[]): BuildDeps {
  return {
    hello: async () => ({ api: "v1", build: reported }),
    servedBuild: () => served,
    install: async (build) => {
      log.push("install " + build);
    },
    reload: (build) => {
      log.push("reload " + build);
    },
  };
}

describe("checkBuild", () => {
  it("installs the worker for the server's build on the first load, without reloading", async () => {
    const log: string[] = [];
    await expect(checkBuild(deps(null, "v2", log))).resolves.toMatchObject({ result: "first" });
    expect(log).toEqual(["install v2"]);
  });

  it("leaves an app on the server's build alone", async () => {
    const log: string[] = [];
    await expect(checkBuild(deps("v2", "v2", log))).resolves.toMatchObject({ result: "current" });
    expect(log).toEqual([]);
  });

  it("reloads an app open on an old build once the new worker is installed", async () => {
    const log: string[] = [];
    await expect(checkBuild(deps("v1", "v2", log))).resolves.toMatchObject({ result: "reloading" });
    expect(log).toEqual(["install v2", "reload v2"]);
  });

  it("does not reload when the new worker fails to take over", async () => {
    const log: string[] = [];
    const d = deps("v1", "v2", log);
    d.install = async () => {
      throw new Error("the worker for v2 did not activate");
    };
    await expect(checkBuild(d)).rejects.toThrow("did not activate");
    expect(log).toEqual([]);
  });

  it("does not reload when the server cannot be reached", async () => {
    const log: string[] = [];
    const d = deps("v1", "v2", log);
    d.hello = async () => {
      throw new Error("offline");
    };
    await expect(checkBuild(d)).rejects.toThrow("offline");
    expect(log).toEqual([]);
  });
});

describe("the worker URL", () => {
  it("carries the build so each build gets its own worker and cache", () => {
    expect(workerURL("v0.12.0+abc")).toBe("/sw.js?build=v0.12.0%2Babc");
    expect(buildOfWorker("https://host.example/sw.js?build=v0.12.0%2Babc")).toBe("v0.12.0+abc");
  });

  it("has no build when no worker controls the page", () => {
    expect(buildOfWorker(undefined)).toBeNull();
    expect(buildOfWorker("https://host.example/sw.js")).toBeNull();
  });
});
