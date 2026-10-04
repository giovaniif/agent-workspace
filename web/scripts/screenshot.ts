import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { chromium, devices, type Browser, type BrowserContext, type Page } from "playwright";

const base = process.env.WEB_SHOT_BASE ?? "http://127.0.0.1:5199";
const cwd = process.env.WEB_SHOT_CWD ?? process.cwd();

type State = { browser: Browser; device: string; installed: boolean; context?: BrowserContext; page?: Page };

async function open(state: State): Promise<Page> {
  if (state.page) {
    return state.page;
  }
  const descriptor = devices[state.device];
  if (!descriptor) {
    throw new Error("unknown device " + state.device);
  }
  const { defaultBrowserType: _browserType, ...options } = descriptor;
  const context = await state.browser.newContext(options);
  if (state.installed) {
    await context.addInitScript(() => {
      Object.defineProperty(window.navigator, "standalone", { value: true });
    });
  }
  state.context = context;
  state.page = await context.newPage();
  return state.page;
}

async function reset(state: State) {
  await state.context?.close();
  state.context = undefined;
  state.page = undefined;
}

function split(rest: string): [string, string] {
  const at = rest.indexOf("=");
  if (at < 0) {
    throw new Error("expected <label>=<text>, got " + rest);
  }
  return [rest.slice(0, at).trim(), rest.slice(at + 1)];
}

async function step(state: State, line: string) {
  const space = line.indexOf(" ");
  const verb = space < 0 ? line : line.slice(0, space);
  const rest = space < 0 ? "" : line.slice(space + 1).trim();
  switch (verb) {
    case "device":
      await reset(state);
      state.device = rest;
      return;
    case "mode":
      await reset(state);
      state.installed = rest === "installed";
      return;
    case "goto":
      await (await open(state)).goto(new URL(rest, base).href);
      return;
    case "fill": {
      const [label, text] = split(rest);
      await (await open(state)).getByLabel(label, { exact: true }).fill(text);
      return;
    }
    case "click":
      await (await open(state)).getByRole("button", { name: rest, exact: true }).click();
      return;
    case "see":
      await (await open(state)).getByText(rest).first().waitFor();
      return;
    case "wait":
      await (await open(state)).waitForTimeout(Number(rest));
      return;
    case "shot":
      await (await open(state)).screenshot({ path: resolve(cwd, rest), fullPage: true });
      console.log("wrote " + rest);
      return;
    default:
      throw new Error("unknown step: " + line);
  }
}

const lines = readFileSync(0, "utf8")
  .split("\n")
  .map((l) => l.trim())
  .filter((l) => l !== "");
const browser = await chromium.launch();
const state: State = { browser, device: "iPhone 15", installed: false };
try {
  for (const line of lines) {
    await step(state, line);
  }
} finally {
  await browser.close();
}
