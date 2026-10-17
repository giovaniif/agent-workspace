import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium, type BrowserContext, type Locator, type Page } from "playwright";

type Loc = { role?: string; name?: string; testId?: string; label?: string; placeholder?: string; text?: string; exact?: boolean };

type Step = {
  goto?: string;
  click?: Loc;
  fill?: Loc;
  press?: string;
  wait?: number;
  expect?: Loc;
  shot?: string;
  aria?: string;
  eval?: string;
  value?: string;
  timeout?: number;
  absent?: boolean;
};

type Viewport = { name: string; width: number; height: number; scale: number };

type Request = {
  base: string;
  viewports: Viewport[];
  steps: Step[];
  out_dir: string;
  settle_ms: number;
  video: boolean;
  ffmpeg?: boolean;
  installed: boolean;
  scheme: "light" | "dark";
  auth: { token: string; device: unknown } | null;
};

type Result = {
  shots: { path: string; viewport: string }[];
  console_errors: string[];
  page_errors: string[];
  failed_requests: string[];
  aria: string[];
  eval: unknown[];
  video: string[];
  error?: string;
  failed_step?: number;
  failure?: string;
};

const noise = [/Download the React DevTools/, /\[vite\]/];

const req = JSON.parse(readFileSync(0, "utf8")) as Request;
const res: Result = { shots: [], console_errors: [], page_errors: [], failed_requests: [], aria: [], eval: [], video: [] };

function isApi(url: string): boolean {
  try {
    return new URL(url).pathname.startsWith("/api/");
  } catch {
    return false;
  }
}

function locate(page: Page, loc: Loc): Locator {
  const exact = loc.exact ?? true;
  let l: Locator;
  if (loc.role) {
    l = page.getByRole(loc.role as Parameters<Page["getByRole"]>[0], { name: loc.name, exact });
  } else if (loc.testId) {
    l = page.getByTestId(loc.testId);
  } else if (loc.label) {
    l = page.getByLabel(loc.label, { exact });
  } else if (loc.placeholder) {
    l = page.getByPlaceholder(loc.placeholder, { exact });
  } else {
    l = page.getByText(loc.text ?? "", { exact });
  }
  return l.filter({ visible: true }).first();
}

function describe(loc: Loc): string {
  return Object.entries(loc)
    .map(([k, v]) => k + "=" + String(v))
    .join(" ");
}

async function settle(page: Page, inflight: { n: number }, quiet: number) {
  const deadline = Date.now() + 10000;
  let since = Date.now();
  while (Date.now() < deadline) {
    if (inflight.n > 0) {
      since = Date.now();
    } else if (Date.now() - since >= quiet) {
      return;
    }
    await page.waitForTimeout(50);
  }
}

async function runViewport(vp: Viewport): Promise<boolean> {
  const videoDir = join(req.out_dir, "video-" + vp.name);
  const context: BrowserContext = await browser.newContext({
    viewport: { width: vp.width, height: vp.height },
    deviceScaleFactor: vp.scale,
    isMobile: vp.name === "mobile",
    hasTouch: vp.name === "mobile",
    colorScheme: req.scheme,
    serviceWorkers: "block",
    recordVideo: req.video ? { dir: videoDir, size: { width: vp.width, height: vp.height } } : undefined,
  });
  if (req.auth) {
    const auth = JSON.stringify(req.auth);
    await context.addInitScript((value) => {
      window.localStorage.setItem("crew.auth", value);
    }, auth);
  }
  if (req.installed) {
    await context.addInitScript(() => {
      Object.defineProperty(window.navigator, "standalone", { value: true });
    });
  }
  const page = await context.newPage();
  const inflight = { n: 0 };
  page.on("console", (m) => {
    if (m.type() === "error" && !noise.some((r) => r.test(m.text()))) {
      res.console_errors.push(vp.name + ": " + m.text());
    }
  });
  page.on("pageerror", (e) => res.page_errors.push(vp.name + ": " + e.message));
  page.on("request", (r) => {
    if (isApi(r.url())) {
      inflight.n++;
    }
  });
  page.on("requestfinished", async (r) => {
    if (!isApi(r.url())) {
      return;
    }
    inflight.n = Math.max(0, inflight.n - 1);
    const resp = await r.response();
    if (resp && resp.status() >= 400) {
      res.failed_requests.push(vp.name + ": " + r.method() + " " + new URL(r.url()).pathname + " " + resp.status());
    }
  });
  page.on("requestfailed", (r) => {
    if (isApi(r.url())) {
      inflight.n = Math.max(0, inflight.n - 1);
      res.failed_requests.push(vp.name + ": " + r.method() + " " + new URL(r.url()).pathname + " " + (r.failure()?.errorText ?? "failed"));
    }
  });
  let ok = true;
  for (const [i, s] of req.steps.entries()) {
    try {
      const timeout = s.timeout ?? 5000;
      if (s.goto !== undefined) {
        await page.goto(new URL(s.goto, req.base).href);
      } else if (s.click) {
        await locate(page, s.click).click({ timeout });
      } else if (s.fill) {
        await locate(page, s.fill).fill(s.value ?? "", { timeout });
      } else if (s.press !== undefined) {
        await page.keyboard.press(s.press);
      } else if (s.wait !== undefined) {
        await page.waitForTimeout(s.wait);
      } else if (s.expect) {
        try {
          await locate(page, s.expect).waitFor({ state: s.absent ? "hidden" : "visible", timeout });
        } catch {
          throw new Error((s.absent ? "still visible: " : "no visible match for ") + describe(s.expect));
        }
      } else if (s.shot !== undefined) {
        const path = join(req.out_dir, vp.name + "-" + s.shot);
        await page.screenshot({ path });
        res.shots.push({ path, viewport: vp.name });
      } else if (s.aria !== undefined) {
        const snap = await page.locator("body").ariaSnapshot();
        const path = join(req.out_dir, vp.name + "-" + s.aria);
        writeFileSync(path, snap);
        res.aria.push(path);
      } else if (s.eval !== undefined) {
        res.eval.push(await page.evaluate(s.eval));
      }
      await settle(page, inflight, req.settle_ms);
    } catch (e) {
      ok = false;
      res.error = vp.name + ": step " + i + ": " + (e instanceof Error ? e.message.split("\n")[0] : String(e));
      res.failed_step = i;
      const shot = join(req.out_dir, vp.name + "-failure.png");
      await page.screenshot({ path: shot }).catch(() => undefined);
      const snap = await page
        .locator("body")
        .ariaSnapshot()
        .catch(() => "");
      writeFileSync(join(req.out_dir, vp.name + "-failure.aria.txt"), snap);
      res.failure = shot;
      break;
    }
  }
  const video = page.video();
  await context.close();
  if (video) {
    const webm = await video.path();
    if (req.ffmpeg) {
      const mp4 = join(req.out_dir, vp.name + ".mp4");
      const r = spawnSync("ffmpeg", ["-y", "-loglevel", "error", "-i", webm, "-pix_fmt", "yuv420p", mp4]);
      res.video.push(r.status === 0 ? mp4 : webm);
    } else {
      res.video.push(webm);
    }
  }
  return ok;
}

mkdirSync(req.out_dir, { recursive: true });
const browser = await chromium.launch();
try {
  for (const vp of req.viewports) {
    if (!(await runViewport(vp))) {
      break;
    }
  }
} catch (e) {
  res.error = e instanceof Error ? e.message : String(e);
} finally {
  await browser.close();
}
process.stdout.write(JSON.stringify(res) + "\n");
process.exitCode = res.error ? 1 : 0;
