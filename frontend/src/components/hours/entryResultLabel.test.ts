import { describe, expect, it } from "vitest";
import { employeeNames, entryResultLabel } from "./entryResultLabel";

const employees = [
  { id: 12340001, naam: "Voorbeeld Medewerker" },
  { id: 12340002, naam: "  Tweede Voorbeeld " },
  { id: 12340003, naam: "" },
];

describe("entryResultLabel", () => {
  const names = employeeNames(employees);

  // The bug: the results list said "Medewerker 12340001 · 2026-10-12".
  it("shows the employee's name and an nl-NL date", () => {
    expect(entryResultLabel({ employeeId: 12340001, date: "2026-10-12" }, names)).toBe(
      "Voorbeeld Medewerker · 12 okt 2026",
    );
    expect(entryResultLabel({ employeeId: 12340002, date: "2026-03-01" }, names)).toBe(
      "Tweede Voorbeeld · 1 mrt 2026",
    );
  });

  it("falls back to the id for an unknown or nameless employee", () => {
    expect(entryResultLabel({ employeeId: 12340099, date: "2026-10-12" }, names)).toBe("Medewerker 12340099 · 12 okt 2026");
    expect(entryResultLabel({ employeeId: 12340003, date: "2026-10-12" }, names)).toBe("Medewerker 12340003 · 12 okt 2026");
    expect(entryResultLabel({ employeeId: 12340001, date: "2026-10-12" }, employeeNames(undefined))).toBe(
      "Medewerker 12340001 · 12 okt 2026",
    );
  });

  it("keeps a date it cannot parse as it is", () => {
    expect(entryResultLabel({ employeeId: 12340001, date: "" }, names)).toBe("Voorbeeld Medewerker");
    expect(entryResultLabel({ employeeId: 12340001, date: "morgen" }, names)).toBe("Voorbeeld Medewerker · morgen");
  });
});
