import { describe, it, expect } from "vitest";
import {
  buildHourOverlay,
  formatHours,
  isoToDate,
  strictMatchKey,
} from "./hourOverlay";
import type { HourOverviewEntry } from "../../api/types";

const entry = (overrides: Partial<HourOverviewEntry> = {}): HourOverviewEntry => ({
  id: 1,
  datum: "2026-04-01T00:00:00",
  medewerker: "Joost Buskermolen",
  project: "PUP",
  activiteit: "Consultancy",
  opmerkingen: "",
  aantalUren: 8,
  aantalKilometers: 0,
  ...overrides,
});

describe("isoToDate", () => {
  it("strips the time portion safely without timezone drift", () => {
    expect(isoToDate("2026-04-01T00:00:00")).toBe("2026-04-01");
    expect(isoToDate("2026-12-31T23:59:59")).toBe("2026-12-31");
  });

  it("passes through already-trimmed dates", () => {
    expect(isoToDate("2026-04-01")).toBe("2026-04-01");
  });

  it("handles short / malformed input without throwing", () => {
    expect(isoToDate("")).toBe("");
    expect(isoToDate("nope")).toBe("nope");
  });
});

describe("strictMatchKey", () => {
  it("produces identical keys for identical inputs regardless of case/whitespace", () => {
    const a = strictMatchKey({
      date: "2026-04-01",
      employeeName: "Joost Buskermolen",
      projectLabel: "PUP",
      activityLabel: "Consultancy",
    });
    const b = strictMatchKey({
      date: "2026-04-01",
      employeeName: "  joost buskermolen  ",
      projectLabel: "pup",
      activityLabel: "consultancy",
    });
    expect(a).toBe(b);
  });

  it("differs when any field differs", () => {
    const base = {
      date: "2026-04-01",
      employeeName: "Joost",
      projectLabel: "PUP",
      activityLabel: "Consultancy",
    };
    const key = strictMatchKey(base);
    expect(strictMatchKey({ ...base, date: "2026-04-02" })).not.toBe(key);
    expect(strictMatchKey({ ...base, employeeName: "Other" })).not.toBe(key);
    expect(strictMatchKey({ ...base, projectLabel: "OTHER" })).not.toBe(key);
    expect(strictMatchKey({ ...base, activityLabel: "Other" })).not.toBe(key);
  });
});

describe("buildHourOverlay", () => {
  it("aggregates total hours per date", () => {
    const overlay = buildHourOverlay([
      entry({ datum: "2026-04-01T00:00:00", aantalUren: 4 }),
      entry({ datum: "2026-04-01T00:00:00", aantalUren: 4, activiteit: "Admin" }),
      entry({ datum: "2026-04-02T00:00:00", aantalUren: 8 }),
    ]);
    expect(overlay.byDate.get("2026-04-01")).toBe(8);
    expect(overlay.byDate.get("2026-04-02")).toBe(8);
    expect(overlay.byDate.size).toBe(2);
  });

  it("collects per-date details preserving order", () => {
    const overlay = buildHourOverlay([
      entry({ id: 1, datum: "2026-04-01T00:00:00", project: "A" }),
      entry({ id: 2, datum: "2026-04-01T00:00:00", project: "B" }),
    ]);
    const list = overlay.byDateDetails.get("2026-04-01");
    expect(list?.length).toBe(2);
    expect(list?.[0].project).toBe("A");
    expect(list?.[1].project).toBe("B");
  });

  it("indexes strict-match keys for duplicate detection", () => {
    // The exact regression we're protecting against: bulk-booking the
    // same date+employee+project+activity that already exists.
    const overlay = buildHourOverlay([
      entry({ datum: "2026-04-01T00:00:00" }),
    ]);
    expect(
      overlay.strictKeys.has(
        strictMatchKey({
          date: "2026-04-01",
          employeeName: "Joost Buskermolen",
          projectLabel: "PUP",
          activityLabel: "Consultancy",
        }),
      ),
    ).toBe(true);
  });

  it("treats different project or activity on same day as non-conflicting", () => {
    // Legitimate split-day: 4h Consultancy + 4h Admin should NOT trigger
    // a duplicate warning for a new 4h Onboarding booking.
    const overlay = buildHourOverlay([
      entry({ datum: "2026-04-01T00:00:00", activiteit: "Consultancy", aantalUren: 4 }),
      entry({ datum: "2026-04-01T00:00:00", activiteit: "Admin", aantalUren: 4 }),
    ]);
    const onboardingKey = strictMatchKey({
      date: "2026-04-01",
      employeeName: "Joost Buskermolen",
      projectLabel: "PUP",
      activityLabel: "Onboarding",
    });
    expect(overlay.strictKeys.has(onboardingKey)).toBe(false);
    // But the original two stay flagged
    expect(overlay.byDate.get("2026-04-01")).toBe(8);
  });

  it("handles an empty input", () => {
    const overlay = buildHourOverlay([]);
    expect(overlay.byDate.size).toBe(0);
    expect(overlay.byDateDetails.size).toBe(0);
    expect(overlay.strictKeys.size).toBe(0);
  });

  it("tolerates a zero-hour entry gracefully", () => {
    const overlay = buildHourOverlay([
      entry({ datum: "2026-04-01T00:00:00", aantalUren: 0 }),
    ]);
    expect(overlay.byDate.get("2026-04-01")).toBe(0);
    expect(overlay.byDateDetails.get("2026-04-01")?.length).toBe(1);
  });
});

describe("formatHours", () => {
  it("renders whole hours without decimals", () => {
    expect(formatHours(8)).toBe("8u");
    expect(formatHours(0)).toBe("0u");
  });

  it("renders fractional hours with a comma (Dutch decimal sep)", () => {
    expect(formatHours(4.5)).toBe("4,5u");
    expect(formatHours(7.75)).toBe("7,75u");
  });

  it("rounds to 2 decimals", () => {
    expect(formatHours(0.333333)).toBe("0,33u");
  });

  it("handles non-finite input safely", () => {
    expect(formatHours(NaN)).toBe("0u");
    expect(formatHours(Infinity)).toBe("0u");
  });
});
