import { describe, it, expect } from "vitest";
import type { InboxClassification, InboxProcessItem, MutatieSoort } from "../../api/types";
import { toInboxProcessItem } from "./inboxBooking";

/**
 * Characterisation against main (a524a1a): before toInboxProcessItem existed,
 * BatchApproveBar, InboxRow and Dashboard each built the POST
 * /inbox/process-batch item inline. The three reference builders below are
 * main's code, copied verbatim apart from the function wrapper. For every
 * row main could book (no open-post overlay, so no relatieId/factuurnummer
 * on the row) the new shared builder must send exactly the same body.
 */

// main: frontend/src/components/inbox/BatchApproveBar.tsx
function mainBatchApproveBarItem(item: InboxClassification): InboxProcessItem {
  const soortCodes: Record<string, number> = {
    FactuurOntvangen: 1, FactuurVerstuurd: 2,
    FactuurbetalingOntvangen: 3, FactuurbetalingVerstuurd: 4,
    GeldOntvangen: 5, GeldUitgegeven: 6, Memoriaal: 7,
  };
  return {
    id: item.id,
    grootboekId: item.grootboekId,
    soort: soortCodes[item.soort] || 6,
    grootboekcode: item.grootboekcode,
    btwCode: item.btwCode,
    omschrijving: item.aiOmschrijving || item.omschrijving,
    bedrag: Math.abs(item.bedrag),
  };
}

// main: frontend/src/components/inbox/InboxRow.tsx (handleProcess); relatieId
// is the relation the user picked in the row, if any.
function mainInboxRowItem(edited: InboxClassification, pickedRelatieId?: number): InboxProcessItem {
  const soortCodes: Record<string, number> = {
    FactuurOntvangen: 1, FactuurVerstuurd: 2,
    FactuurbetalingOntvangen: 3, FactuurbetalingVerstuurd: 4,
    GeldOntvangen: 5, GeldUitgegeven: 6, Memoriaal: 7,
  };
  return {
    id: edited.id,
    grootboekId: edited.grootboekId,
    soort: soortCodes[edited.soort] || 6,
    grootboekcode: edited.grootboekcode,
    btwCode: edited.btwCode,
    omschrijving: edited.aiOmschrijving || edited.omschrijving,
    relatieId: pickedRelatieId,
    bedrag: Math.abs(edited.bedrag),
  };
}

// main: frontend/src/components/Dashboard.tsx (handleAutoProcess)
function mainDashboardItem(item: InboxClassification) {
  const soortCodes: Record<string, number> = {
    FactuurOntvangen: 1, FactuurVerstuurd: 2,
    FactuurbetalingOntvangen: 3, FactuurbetalingVerstuurd: 4,
    GeldOntvangen: 5, GeldUitgegeven: 6, Memoriaal: 7,
  };
  return {
    id: item.id,
    grootboekId: item.grootboekId || 0,
    soort: soortCodes[item.soort] || 6,
    grootboekcode: item.grootboekcode,
    btwCode: item.btwCode,
    omschrijving: item.aiOmschrijving || item.omschrijving,
    bedrag: Math.abs(item.bedrag),
  };
}

/** What the backend's inboxBatchItem decodes from a body: absent fields are
 *  Go zero values, so an omitted key and an explicit zero are the same. */
function asBackendDecodes(body: object) {
  const o = JSON.parse(JSON.stringify(body)) as Partial<InboxProcessItem>;
  return {
    id: o.id ?? 0,
    grootboekId: o.grootboekId ?? 0,
    soort: o.soort ?? 0,
    grootboekcode: o.grootboekcode ?? "",
    btwCode: o.btwCode ?? "",
    omschrijving: o.omschrijving ?? "",
    bedrag: o.bedrag ?? 0,
    relatieId: o.relatieId ?? 0,
    factuurnummer: o.factuurnummer ?? "",
  };
}

const line = (overrides: Partial<InboxClassification> = {}): InboxClassification => ({
  id: 9001,
  datum: "2026-10-03T00:00:00",
  bedrag: -42.5,
  omschrijving: "Pinbetaling Voorbeeld Leverancier",
  rekening: "NL00BANK0000000001",
  grootboekId: 1234,
  category: "review",
  needsInvoice: false,
  confidence: 0.82,
  grootboekcode: "4300",
  btwCode: "HOOG_INK_21",
  soort: "GeldUitgegeven",
  aiOmschrijving: "Kantoorartikelen Voorbeeld Leverancier",
  indicator: "AI",
  ...overrides,
});

// Rows main could produce: every soort the row's picker offers that is not a
// factuurbetaling, outgoing and incoming, with and without an AI description.
const soortCases: Array<{ name: string; row: InboxClassification }> = [
  { name: "GeldUitgegeven (6)", row: line() },
  {
    name: "GeldOntvangen (5)",
    row: line({ bedrag: 125, soort: "GeldOntvangen", grootboekcode: "8000", btwCode: "HOOG_VERK_21", aiOmschrijving: "Omzet Voorbeeld Klant" }),
  },
  { name: "GeldUitgegeven without AI description", row: line({ aiOmschrijving: "" }) },
  { name: "GeldUitgegeven, BTW GEEN, learned", row: line({ btwCode: "GEEN", learned: true, category: "auto", confidence: 1 }) },
  { name: "Memoriaal (7)", row: line({ soort: "Memoriaal" }) },
  { name: "FactuurOntvangen (1)", row: line({ soort: "FactuurOntvangen" }) },
  { name: "FactuurVerstuurd (2)", row: line({ bedrag: 250, soort: "FactuurVerstuurd" }) },
  { name: "unknown soort falls back to 6", row: line({ soort: "Onbekend" as MutatieSoort }) },
  { name: "grootboekId 0 (fallback rows)", row: line({ grootboekId: 0 }) },
];

describe("process-batch body for rows main could book is unchanged", () => {
  describe("BatchApproveBar", () => {
    for (const c of soortCases) {
      it(`${c.name}: byte-for-byte the same JSON`, () => {
        expect(JSON.stringify(toInboxProcessItem(c.row))).toBe(JSON.stringify(mainBatchApproveBarItem(c.row)));
      });
    }
  });

  describe("Dashboard auto-process", () => {
    for (const c of soortCases) {
      it(`${c.name}: byte-for-byte the same JSON`, () => {
        expect(JSON.stringify(toInboxProcessItem(c.row))).toBe(JSON.stringify(mainDashboardItem(c.row)));
      });
    }
  });

  describe("InboxRow single booking", () => {
    for (const c of soortCases) {
      it(`${c.name}, no relation picked: byte-for-byte the same JSON`, () => {
        // main put `relatieId: undefined` in the object; JSON.stringify drops it.
        expect(JSON.stringify(toInboxProcessItem(c.row, undefined))).toBe(JSON.stringify(mainInboxRowItem(c.row, undefined)));
      });

      it(`${c.name}, relation picked: the backend decodes the same item`, () => {
        // The only difference is key order (relatieId now comes after bedrag),
        // which JSON decoding ignores.
        const now = toInboxProcessItem(c.row, 777);
        const before = mainInboxRowItem(c.row, 777);
        expect(asBackendDecodes(now)).toEqual(asBackendDecodes(before));
        expect(Object.keys(now).sort()).toEqual(Object.keys(JSON.parse(JSON.stringify(before))).sort());
      });
    }
  });

  it("never adds relatieId or factuurnummer to a GeldUitgegeven/GeldOntvangen row without an overlay", () => {
    for (const c of soortCases) {
      const body = JSON.parse(JSON.stringify(toInboxProcessItem(c.row)));
      expect(body).not.toHaveProperty("relatieId");
      expect(body).not.toHaveProperty("factuurnummer");
    }
  });
});
