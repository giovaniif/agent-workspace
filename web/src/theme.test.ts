import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "theme.css"), "utf8");

function tokens(): Record<string, string> {
  const root = css.match(/:root\s*{([^}]*)}/)?.[1] ?? "";
  const out: Record<string, string> = {};
  for (const m of root.matchAll(/--([\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) {
    out[m[1]] = m[2];
  }
  return out;
}

function rule(selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const body = css.match(new RegExp("(?:^|[\\s,}])" + escaped + "\\s*(?:,[^{]*)?{([^}]*)}", "m"))?.[1];
  if (body === undefined) {
    throw new Error("no rule for " + selector);
  }
  const out: Record<string, string> = {};
  for (const decl of body.split(";")) {
    const at = decl.indexOf(":");
    if (at > 0) {
      out[decl.slice(0, at).trim()] = decl.slice(at + 1).trim();
    }
  }
  return out;
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

describe("the Latte palette", () => {
  const palette = tokens();

  it("reads its tokens from theme.css", () => {
    expect(palette.text).toMatch(/^#/);
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21);
  });

  it.each([
    ["text", "base"],
    ["text", "mantle"],
    ["text", "selected"],
    ["subtext", "base"],
    ["subtext", "mantle"],
    ["red", "base"],
    ["on-accent", "red"],
    ["link", "base"],
    ["link", "mantle"],
    ["on-accent", "blue"],
  ])("%s text on %s meets 4.5:1", (fg, bg) => {
    expect(palette[fg], fg).toBeDefined();
    expect(palette[bg], bg).toBeDefined();
    expect(contrast(palette[fg], palette[bg])).toBeGreaterThanOrEqual(4.5);
  });
});

describe("the Mocha palette", () => {
  const dark = css.match(/@media \(prefers-color-scheme: dark\)\s*{\s*:root\s*{([^}]*)}/)?.[1] ?? "";
  const palette: Record<string, string> = {};
  for (const m of dark.matchAll(/--([\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) {
    palette[m[1]] = m[2];
  }

  it("is the dark scheme of the app", () => {
    expect(css).toMatch(/color-scheme:\s*light dark/);
    expect(palette.base).toBe("#1e1e2e");
    expect(palette.blue).toBe("#89b4fa");
  });

  it.each([
    ["text", "base"],
    ["text", "mantle"],
    ["text", "selected"],
    ["subtext", "base"],
    ["subtext", "mantle"],
    ["red", "base"],
    ["on-accent", "red"],
    ["link", "base"],
    ["link", "mantle"],
    ["on-accent", "blue"],
  ])("%s text on %s meets 4.5:1", (fg, bg) => {
    expect(palette[fg], fg).toBeDefined();
    expect(palette[bg], bg).toBeDefined();
    expect(contrast(palette[fg], palette[bg])).toBeGreaterThanOrEqual(4.5);
  });
});

describe("touch targets", () => {
  it.each([".session-card", ".back", ".retry", ".new-session", ".choice", ".interrupt", ".queued-action", ".permission-choice", ".load-older", ".tool summary", ".automation-row button", ".run-actions button", ".approval-actions button", ".kill-actions button", ".mate-actions button", ".mate-threads a"])("%s is at least 44px tall", (selector) => {
    expect(parseInt(rule(selector)["min-height"] ?? "0", 10)).toBeGreaterThanOrEqual(44);
  });
});

describe("the chat layout", () => {
  it("scrolls a long queue of sends instead of pushing the composer off the screen", () => {
    const queued = rule(".queued");
    expect(queued["max-height"]).toMatch(/vh|dvh/);
    expect(queued["overflow-y"]).toBe("auto");
  });
});
