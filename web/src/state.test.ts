import { describe, expect, it } from "vitest";
import { stateStyle } from "./sessions";

describe("the state glyphs of the Mac app", () => {
  it.each([
    ["permission", "◆", "Permission"],
    ["waiting", "●", "Waiting"],
    ["running", "◌", "Running"],
    ["done", "✓", "Done"],
    ["idle", "○", "Idle"],
    ["paused", "?", "paused"],
  ])("%s is %s %s", (state, glyph, label) => {
    expect(stateStyle(state)).toEqual({ glyph, label });
  });
});
