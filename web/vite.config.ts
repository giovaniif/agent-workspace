import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";
import { fakeApi } from "./fake-api.ts";

const shellFilesMarker = "__AGENTWS_SHELL_FILES__";

function appShell(): Plugin {
  return {
    name: "agentws-app-shell",
    apply: "build",
    enforce: "post",
    generateBundle(_options, bundle) {
      const html = bundle["index.html"];
      if (html && html.type === "asset") {
        delete bundle["index.html"];
        this.emitFile({ type: "asset", fileName: "app.html", source: html.source });
      }
      const files = Object.keys(bundle)
        .filter((name) => name !== "sw.js" && name !== "app.html" && name !== "index.html" && !name.endsWith(".woff"))
        .map((name) => "/" + name)
        .sort();
      const sw = bundle["sw.js"];
      if (!sw || sw.type !== "chunk") {
        this.error("the service worker chunk sw.js is missing");
      }
      const list = JSON.stringify(files);
      const replaced = sw.code.replace(new RegExp("([\"'`])" + shellFilesMarker + "\\1", "g"), list);
      if (replaced === sw.code) {
        this.error("the service worker has no shell file marker");
      }
      sw.code = replaced;
    },
  };
}

const api = process.env.AGENTWS_API;

export default defineConfig({
  plugins: [react(), appShell(), process.env.AGENTWS_FAKE_API ? fakeApi() : null],
  build: {
    outDir: "../internal/serve/dist",
    emptyOutDir: false,
    rollupOptions: {
      input: { app: "index.html", sw: "src/sw.ts" },
      output: {
        entryFileNames: (chunk) => (chunk.name === "sw" ? "sw.js" : "assets/[name]-[hash].js"),
      },
    },
  },
  server: {
    host: true,
    allowedHosts: [".ts.net"],
    proxy: api ? { "/api": { target: api, ws: true, secure: process.env.AGENTWS_API_INSECURE !== "1" } } : undefined,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
