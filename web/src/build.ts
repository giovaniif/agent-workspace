import type { Hello } from "./api";

export type BuildDeps = {
  hello: () => Promise<Hello>;
  servedBuild: () => string | null;
  install: (build: string) => Promise<void>;
  reload: (build: string) => void;
};

export type BuildCheck = "first" | "current" | "reloading";

export function buildOfWorker(scriptURL: string | undefined): string | null {
  if (!scriptURL) {
    return null;
  }
  return new URL(scriptURL, "http://x").searchParams.get("build");
}

export function workerURL(build: string): string {
  return "/sw.js?build=" + encodeURIComponent(build);
}

export async function checkBuild(deps: BuildDeps): Promise<{ hello: Hello; result: BuildCheck }> {
  const h = await deps.hello();
  const served = deps.servedBuild();
  if (served === h.build) {
    return { hello: h, result: "current" };
  }
  await deps.install(h.build);
  if (served === null) {
    return { hello: h, result: "first" };
  }
  deps.reload(h.build);
  return { hello: h, result: "reloading" };
}
