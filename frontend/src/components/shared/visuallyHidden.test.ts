import { describe, expect, it } from "vitest";
import { visuallyHidden } from "./visuallyHidden";

describe("visuallyHidden", () => {
  it("is one pixel in real pixels, not MUI's 100%", () => {
    expect(visuallyHidden.width).toBe("1px");
    expect(visuallyHidden.height).toBe("1px");
    expect(visuallyHidden.position).toBe("absolute");
  });

  // A hand-written copy with `width: 1` is a 100% wide invisible box that
  // gives a dialog scrollbars. Every hidden region uses the shared style.
  it("is the only place that writes a clip rect", () => {
    const sources = import.meta.glob("../../**/*.{ts,tsx}", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
    expect(Object.keys(sources).length).toBeGreaterThan(50);
    const offenders = Object.entries(sources)
      .filter(([path, text]) => !/visuallyHidden\.(test\.)?ts$/.test(path) && text.includes("rect(0 0 0 0)"))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });
});
