import "@fontsource/ibm-plex-sans/latin-400.css";
import "@fontsource/ibm-plex-sans/latin-500.css";
import "@fontsource/ibm-plex-sans/latin-600.css";
import "@fontsource/jetbrains-mono/latin-400.css";
import "@fontsource/jetbrains-mono/latin-500.css";
import "./theme.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { hello } from "./api";
import { App } from "./App";
import { buildOfWorker, checkBuild, workerURL } from "./build";
import { isInstalled } from "./display";
import { browserPush } from "./push";

const checkEvery = 5 * 60 * 1000;
const installTimeout = 15 * 1000;
const reloadedKey = "agentws.reloaded-for";
const apiFetch = (input: string, init?: RequestInit) => fetch(input, init);
const streamURL = (window.location.protocol === "https:" ? "wss://" : "ws://") + window.location.host + "/api/v1/stream";
const openStream = () => new WebSocket(streamURL);
const now = () => Date.now();
const visibility = {
  visible: () => document.visibilityState === "visible",
  onChange: (listener: () => void) => {
    document.addEventListener("visibilitychange", listener);
    return () => document.removeEventListener("visibilitychange", listener);
  },
};
const onHashChange = (listener: (hash: string) => void) => {
  const changed = () => listener(window.location.hash);
  window.addEventListener("hashchange", changed);
  return () => window.removeEventListener("hashchange", changed);
};

function reloadOnceFor(build: string) {
  try {
    if (window.sessionStorage.getItem(reloadedKey) === build) {
      return;
    }
    window.sessionStorage.setItem(reloadedKey, build);
  } catch {
    return;
  }
  window.location.reload();
}

async function installWorker(build: string): Promise<void> {
  if (!("serviceWorker" in navigator)) {
    return;
  }
  const registration = await navigator.serviceWorker.register(workerURL(build), { scope: "/" });
  const ready = () => registration.active?.state === "activated" && buildOfWorker(registration.active.scriptURL) === build;
  if (ready()) {
    return;
  }
  await new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(() => reject(new Error("the worker for " + build + " did not activate")), installTimeout);
    const watch = (worker: ServiceWorker | null) =>
      worker?.addEventListener("statechange", () => {
        if (ready()) {
          window.clearTimeout(timer);
          resolve();
        }
      });
    for (const worker of [registration.installing, registration.waiting, registration.active]) {
      watch(worker);
    }
    registration.addEventListener("updatefound", () => watch(registration.installing));
  });
}

function watchBuild() {
  if (!import.meta.env.PROD) {
    return;
  }
  let running: Promise<unknown> | null = null;
  const check = () => {
    running ??= checkBuild({
      hello: () => hello(apiFetch),
      servedBuild: () => buildOfWorker(navigator.serviceWorker?.controller?.scriptURL),
      install: installWorker,
      reload: reloadOnceFor,
    })
      .catch(() => undefined)
      .finally(() => {
        running = null;
      });
    return running;
  };
  void check();
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") {
      void check();
    }
  });
  window.setInterval(() => {
    if (document.visibilityState === "visible") {
      void check();
    }
  }, checkEvery);
}

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App
        env={{
          fetch: apiFetch,
          storage: window.localStorage,
          installed: isInstalled(window),
          hash: window.location.hash,
          host: window.location.host,
          userAgent: navigator.userAgent,
          openStream,
          now,
          onHashChange,
          push: browserPush(window),
          visibility,
        }}
      />
    </StrictMode>,
  );
}
watchBuild();
