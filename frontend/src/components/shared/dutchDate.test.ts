import { describe, it, expect } from "vitest";
import {
  addDays,
  addMonths,
  buildMonthGrid,
  endOfWeek,
  formatDutchDate,
  formatLongDutchDate,
  formatMonthYear,
  isValidIso,
  parseDutchDate,
  startOfWeek,
  todayIso,
  weekdayIndex,
} from "./dutchDate";

const TODAY = "2026-11-20";

describe("isValidIso", () => {
  it("accepts real calendar dates", () => {
    expect(isValidIso("2026-11-15")).toBe(true);
    expect(isValidIso("2026-01-31")).toBe(true);
    expect(isValidIso("2024-02-29")).toBe(true);
    expect(isValidIso("2000-02-29")).toBe(true);
  });

  it("rejects impossible dates", () => {
    expect(isValidIso("2026-02-30")).toBe(false);
    expect(isValidIso("2026-02-29")).toBe(false);
    expect(isValidIso("1900-02-29")).toBe(false); // not a leap year
    expect(isValidIso("2100-02-29")).toBe(false); // not a leap year
    expect(isValidIso("2026-04-31")).toBe(false);
    expect(isValidIso("2026-13-01")).toBe(false);
    expect(isValidIso("2026-00-10")).toBe(false);
    expect(isValidIso("2026-11-00")).toBe(false);
  });

  it("rejects other shapes and years outside 1900..2100", () => {
    expect(isValidIso("")).toBe(false);
    expect(isValidIso("2026-1-5")).toBe(false);
    expect(isValidIso("15-11-2026")).toBe(false);
    expect(isValidIso("2026-11-15T00:00:00")).toBe(false);
    expect(isValidIso("1899-12-31")).toBe(false);
    expect(isValidIso("2101-01-01")).toBe(false);
    expect(isValidIso("1900-01-01")).toBe(true);
    expect(isValidIso("2100-12-31")).toBe(true);
  });
});

describe("parseDutchDate", () => {
  it("reads d-m-jjjj with -, / , . or spaces", () => {
    expect(parseDutchDate("15-11-2026", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("15/11/2026", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("15.11.2026", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("15 11 2026", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("5-1-2026", TODAY)).toBe("2026-01-05");
    expect(parseDutchDate("05-01-2026", TODAY)).toBe("2026-01-05");
    expect(parseDutchDate("5 - 1 - 2026", TODAY)).toBe("2026-01-05");
  });

  it("trims whitespace", () => {
    expect(parseDutchDate("  15-11-2026 \t", TODAY)).toBe("2026-11-15");
  });

  it("takes a 2-digit year as 20xx", () => {
    expect(parseDutchDate("15-11-26", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("1/2/00", TODAY)).toBe("2000-02-01");
    expect(parseDutchDate("31.12.99", TODAY)).toBe("2099-12-31");
  });

  it("takes day-month without a year as the year of today", () => {
    expect(parseDutchDate("15-11", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("1/2", TODAY)).toBe("2026-02-01");
    expect(parseDutchDate("29-2", "2028-06-01")).toBe("2028-02-29");
    expect(parseDutchDate("29-2", TODAY)).toBeNull();
  });

  it("reads ddmmjjjj and ddmmjj digits", () => {
    expect(parseDutchDate("15112026", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("151126", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("01012000", TODAY)).toBe("2000-01-01");
  });

  it("reads ISO", () => {
    expect(parseDutchDate("2026-11-15", TODAY)).toBe("2026-11-15");
    expect(parseDutchDate("2026-1-5", TODAY)).toBe("2026-01-05");
  });

  it("rejects impossible dates", () => {
    expect(parseDutchDate("30-02-2026", TODAY)).toBeNull();
    expect(parseDutchDate("29-02-2026", TODAY)).toBeNull();
    expect(parseDutchDate("31-04-2026", TODAY)).toBeNull();
    expect(parseDutchDate("32-01-2026", TODAY)).toBeNull();
    expect(parseDutchDate("15-13-2026", TODAY)).toBeNull();
    expect(parseDutchDate("0-1-2026", TODAY)).toBeNull();
    expect(parseDutchDate("2026-02-30", TODAY)).toBeNull();
    expect(parseDutchDate("30022026", TODAY)).toBeNull();
  });

  it("accepts 29 februari in a leap year only", () => {
    expect(parseDutchDate("29-02-2024", TODAY)).toBe("2024-02-29");
    expect(parseDutchDate("29-02-2000", TODAY)).toBe("2000-02-29");
    expect(parseDutchDate("29-02-1900", TODAY)).toBeNull();
  });

  it("rejects years outside 1900..2100", () => {
    expect(parseDutchDate("31-12-1899", TODAY)).toBeNull();
    expect(parseDutchDate("01-01-2101", TODAY)).toBeNull();
    expect(parseDutchDate("1899-12-31", TODAY)).toBeNull();
    expect(parseDutchDate("01-01-1900", TODAY)).toBe("1900-01-01");
    expect(parseDutchDate("31-12-2100", TODAY)).toBe("2100-12-31");
  });

  it("rejects other text", () => {
    expect(parseDutchDate("", TODAY)).toBeNull();
    expect(parseDutchDate("   ", TODAY)).toBeNull();
    expect(parseDutchDate("morgen", TODAY)).toBeNull();
    expect(parseDutchDate("15-11-202", TODAY)).toBeNull();
    expect(parseDutchDate("15-11-20266", TODAY)).toBeNull();
    expect(parseDutchDate("15", TODAY)).toBeNull();
    expect(parseDutchDate("1511", TODAY)).toBeNull();
    expect(parseDutchDate("1511202", TODAY)).toBeNull();
    expect(parseDutchDate("15-11-2026x", TODAY)).toBeNull();
    expect(parseDutchDate("15--11-2026", TODAY)).toBeNull();
    expect(parseDutchDate("-15-11-2026", TODAY)).toBeNull();
  });
});

describe("formatDutchDate", () => {
  it("writes dd-mm-jjjj", () => {
    expect(formatDutchDate("2026-11-15")).toBe("15-11-2026");
    expect(formatDutchDate("2026-01-05")).toBe("05-01-2026");
  });

  it("is empty for empty or invalid input", () => {
    expect(formatDutchDate("")).toBe("");
    expect(formatDutchDate("2026-02-30")).toBe("");
    expect(formatDutchDate("nonsense")).toBe("");
  });

  it("round-trips through parseDutchDate", () => {
    for (const iso of ["2026-11-15", "2024-02-29", "1900-01-01", "2100-12-31"]) {
      expect(parseDutchDate(formatDutchDate(iso), TODAY)).toBe(iso);
    }
  });
});

describe("formatLongDutchDate", () => {
  it("writes weekday, day, month and year in Dutch", () => {
    expect(formatLongDutchDate("2026-11-15")).toBe("zondag 15 november 2026");
    expect(formatLongDutchDate("2026-11-16")).toBe("maandag 16 november 2026");
    expect(formatLongDutchDate("2024-02-29")).toBe("donderdag 29 februari 2024");
    expect(formatLongDutchDate("2026-01-01")).toBe("donderdag 1 januari 2026");
  });

  it("is empty for empty or invalid input", () => {
    expect(formatLongDutchDate("")).toBe("");
    expect(formatLongDutchDate("2026-02-30")).toBe("");
  });
});

describe("formatMonthYear", () => {
  it("writes the month name and year", () => {
    expect(formatMonthYear(2026, 11)).toBe("november 2026");
    expect(formatMonthYear(2027, 1)).toBe("januari 2027");
  });
});

describe("weekdayIndex", () => {
  it("counts from maandag", () => {
    expect(weekdayIndex("2026-11-16")).toBe(0);
    expect(weekdayIndex("2026-11-15")).toBe(6);
    expect(weekdayIndex("nonsense")).toBe(-1);
  });
});

describe("addDays", () => {
  it("moves within a month", () => {
    expect(addDays("2026-11-15", 1)).toBe("2026-11-16");
    expect(addDays("2026-11-15", -7)).toBe("2026-11-08");
    expect(addDays("2026-11-15", 0)).toBe("2026-11-15");
  });

  it("crosses month and year boundaries", () => {
    expect(addDays("2026-11-30", 1)).toBe("2026-12-01");
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
    expect(addDays("2027-01-01", -1)).toBe("2026-12-31");
    expect(addDays("2026-12-28", 7)).toBe("2027-01-04");
  });

  it("knows leap years", () => {
    expect(addDays("2024-02-28", 1)).toBe("2024-02-29");
    expect(addDays("2026-02-28", 1)).toBe("2026-03-01");
    expect(addDays("2024-03-01", -1)).toBe("2024-02-29");
  });

  it("is not thrown off by daylight saving time", () => {
    // NL switches clocks on the last Sundays of March and October.
    expect(addDays("2026-03-28", 1)).toBe("2026-03-29");
    expect(addDays("2026-03-29", 1)).toBe("2026-03-30");
    expect(addDays("2026-10-25", 1)).toBe("2026-10-26");
    expect(addDays("2026-10-24", 7)).toBe("2026-10-31");
  });

  it("returns invalid input unchanged", () => {
    expect(addDays("", 1)).toBe("");
  });
});

describe("addMonths", () => {
  it("moves the month and keeps the day", () => {
    expect(addMonths("2026-11-15", 1)).toBe("2026-12-15");
    expect(addMonths("2026-11-15", -1)).toBe("2026-10-15");
  });

  it("crosses year boundaries", () => {
    expect(addMonths("2026-12-15", 1)).toBe("2027-01-15");
    expect(addMonths("2026-01-15", -1)).toBe("2025-12-15");
    expect(addMonths("2026-11-15", 12)).toBe("2027-11-15");
    expect(addMonths("2026-11-15", -24)).toBe("2024-11-15");
    expect(addMonths("2026-11-15", -23)).toBe("2024-12-15");
  });

  it("clamps the day to the target month", () => {
    expect(addMonths("2026-01-31", 1)).toBe("2026-02-28");
    expect(addMonths("2024-01-31", 1)).toBe("2024-02-29");
    expect(addMonths("2026-03-31", -1)).toBe("2026-02-28");
    expect(addMonths("2026-05-31", 1)).toBe("2026-06-30");
    expect(addMonths("2024-02-29", 12)).toBe("2025-02-28");
    expect(addMonths("2024-02-29", 48)).toBe("2028-02-29");
  });

  it("returns invalid input unchanged", () => {
    expect(addMonths("", 1)).toBe("");
  });
});

describe("startOfWeek / endOfWeek", () => {
  it("runs from maandag to zondag", () => {
    // 2026-11-15 is a Sunday: it ends its week.
    expect(startOfWeek("2026-11-15")).toBe("2026-11-09");
    expect(endOfWeek("2026-11-15")).toBe("2026-11-15");
    expect(startOfWeek("2026-11-16")).toBe("2026-11-16");
    expect(endOfWeek("2026-11-16")).toBe("2026-11-22");
    expect(startOfWeek("2026-11-20")).toBe("2026-11-16");
    expect(endOfWeek("2026-11-20")).toBe("2026-11-22");
  });

  it("crosses month and year boundaries", () => {
    // 2027-01-01 is a Friday.
    expect(startOfWeek("2027-01-01")).toBe("2026-12-28");
    expect(endOfWeek("2027-01-01")).toBe("2027-01-03");
    expect(endOfWeek("2026-02-26")).toBe("2026-03-01");
  });
});

describe("todayIso", () => {
  it("uses the local date parts", () => {
    expect(todayIso(new Date(2026, 10, 20, 0, 5))).toBe("2026-11-20");
    expect(todayIso(new Date(2026, 10, 20, 23, 55))).toBe("2026-11-20");
    expect(todayIso(new Date(2024, 1, 29, 12))).toBe("2024-02-29");
  });
});

describe("buildMonthGrid", () => {
  it("has 6 rows of 7 days, Monday first", () => {
    const grid = buildMonthGrid(2026, 11);
    expect(grid).toHaveLength(6);
    for (const row of grid) {
      expect(row).toHaveLength(7);
      expect(weekdayIndex(row[0].iso)).toBe(0);
      expect(weekdayIndex(row[6].iso)).toBe(6);
    }
  });

  it("covers november 2026 with the adjacent days", () => {
    // 1 november 2026 is a Sunday, so the first row is 26 okt .. 1 nov.
    const grid = buildMonthGrid(2026, 11);
    expect(grid[0][0]).toEqual({ iso: "2026-10-26", day: 26, inMonth: false });
    expect(grid[0][6]).toEqual({ iso: "2026-11-01", day: 1, inMonth: true });
    expect(grid[5][0]).toEqual({ iso: "2026-11-30", day: 30, inMonth: true });
    expect(grid[5][6]).toEqual({ iso: "2026-12-06", day: 6, inMonth: false });
    const inMonth = grid.flat().filter((c) => c.inMonth);
    expect(inMonth).toHaveLength(30);
    expect(inMonth.map((c) => c.day)).toEqual(Array.from({ length: 30 }, (_, i) => i + 1));
  });

  it("starts on the 1st when the month starts on a Monday", () => {
    // 1 juni 2026 is a Monday.
    const grid = buildMonthGrid(2026, 6);
    expect(grid[0][0]).toEqual({ iso: "2026-06-01", day: 1, inMonth: true });
    expect(grid[5][6].iso).toBe("2026-07-12");
  });

  it("handles februari in leap and common years", () => {
    expect(buildMonthGrid(2024, 2).flat().filter((c) => c.inMonth)).toHaveLength(29);
    // februari 2027 starts on a Monday and has exactly 4 weeks.
    const feb = buildMonthGrid(2027, 2);
    expect(feb.flat().filter((c) => c.inMonth)).toHaveLength(28);
    expect(feb[0][0].iso).toBe("2027-02-01");
    expect(feb[4][0]).toEqual({ iso: "2027-03-01", day: 1, inMonth: false });
  });

  it("crosses the year boundary", () => {
    const jan = buildMonthGrid(2027, 1);
    expect(jan[0][0]).toEqual({ iso: "2026-12-28", day: 28, inMonth: false });
    const dec = buildMonthGrid(2026, 12);
    expect(dec[5][6]).toEqual({ iso: "2027-01-10", day: 10, inMonth: false });
  });

  it("consecutive cells are consecutive days", () => {
    const flat = buildMonthGrid(2026, 3).flat();
    for (let i = 1; i < flat.length; i++) {
      expect(flat[i].iso).toBe(addDays(flat[i - 1].iso, 1));
    }
  });
});
