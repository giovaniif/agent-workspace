import { describe, expect, it } from "vitest";
import { defaultDeviceName, formatPairCode, isInstalled, pairCodeFromHash } from "./display";

describe("isInstalled", () => {
  const media = (matches: boolean) => () => ({ matches });

  it("is true in a standalone display", () => {
    expect(isInstalled({ matchMedia: media(true), navigator: {} })).toBe(true);
  });

  it("is true for an iOS Home Screen app", () => {
    expect(isInstalled({ matchMedia: media(false), navigator: { standalone: true } })).toBe(true);
  });

  it("is false in a browser tab", () => {
    expect(isInstalled({ matchMedia: media(false), navigator: { standalone: false } })).toBe(false);
    expect(isInstalled({ navigator: {} })).toBe(false);
  });
});

describe("pairCodeFromHash", () => {
  it.each([
    ["#pair=ABCD2345", "ABCD2345"],
    ["#pair=abcd-2345", "ABCD2345"],
    ["#pair=ABCD%202345", "ABCD2345"],
    ["#pair=ABCD2345XYZ", "ABCD2345"],
    ["#pair=IO01ABCD", "ABCD"],
    ["#other=1", ""],
    ["", ""],
  ])("reads %j as %j", (hash, code) => {
    expect(pairCodeFromHash(hash)).toBe(code);
  });
});

describe("display helpers", () => {
  it("splits a code in two for reading", () => {
    expect(formatPairCode("ABCD2345")).toBe("ABCD 2345");
    expect(formatPairCode("ABC")).toBe("ABC");
  });

  it.each([
    ["Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)", "iPhone"],
    ["Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X)", "iPad"],
    ["Mozilla/5.0 (Linux; Android 15; Pixel 9)", "Android phone"],
    ["Mozilla/5.0 (X11; Linux x86_64)", "phone"],
  ])("names a device from %j", (ua, name) => {
    expect(defaultDeviceName(ua)).toBe(name);
  });
});
