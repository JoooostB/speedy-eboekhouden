import type { Employee, EntryResult } from "../../api/types";
import { formatLongDate } from "../../hooks/mailDocumentsModel";

/**
 * The label of one submitted hour line in the results list:
 * "Voorbeeld Medewerker · 12 okt 2026". Pure, so it is unit-tested.
 */

/** Employee names by id, for looking up many results. */
export function employeeNames(employees: readonly Employee[] | undefined): Map<number, string> {
  const names = new Map<number, string>();
  for (const e of employees ?? []) {
    const naam = e.naam?.trim();
    if (naam) names.set(e.id, naam);
  }
  return names;
}

/** Falls back to "Medewerker {id}" for an employee that is not in the list
 *  (removed meanwhile) and to the raw date when it does not parse. */
export function entryResultLabel(result: Pick<EntryResult, "employeeId" | "date">, names: Map<number, string>): string {
  const who = names.get(result.employeeId) ?? `Medewerker ${result.employeeId}`;
  const when = formatLongDate(result.date) || result.date?.trim();
  return when ? `${who} · ${when}` : who;
}
