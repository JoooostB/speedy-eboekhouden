import type {
  InboxClassification,
  InboxProcessItem,
  LedgerAccount,
  MutatieSoort,
  OpenPostGridItem,
} from "../../api/types";
import { MUTATIE_SOORT_CODES } from "../../api/types";

/**
 * Pure rules for turning an inbox row into a booking. Shared by InboxRow,
 * BatchApproveBar, BookingConfirmDialog and the dashboard so the three
 * places that call processInboxBatch agree on the payload.
 */

/** A factuurbetaling settles an open invoice: it needs a relation and an
 *  invoice number instead of a tegenrekening, and never carries BTW. */
export function isFactuurbetalingSoort(soort: MutatieSoort | string): boolean {
  return soort === "FactuurbetalingVerstuurd" || soort === "FactuurbetalingOntvangen";
}

/** True when the row carries what a factuurbetaling needs. */
export function hasOpenInvoiceReference(item: Pick<InboxClassification, "relatieId" | "factuurnummer">): boolean {
  return !!item.relatieId && !!item.factuurnummer && item.factuurnummer.trim() !== "";
}

/**
 * The reason a row cannot be booked yet, or null when it can. A normal
 * booking needs a tegenrekening; a factuurbetaling needs the relation and
 * invoice number of the open invoice instead.
 */
export function bookingBlocker(
  item: InboxClassification,
  edit: { ledger: LedgerAccount | null } | undefined,
): "Geen tegenrekening" | "Geen openstaande factuur" | null {
  if (isFactuurbetalingSoort(item.soort)) {
    return hasOpenInvoiceReference(item) ? null : "Geen openstaande factuur";
  }
  return edit?.ledger ? null : "Geen tegenrekening";
}

/**
 * Builds the process-batch payload for one row. relatieId overrides the
 * row's own relation (InboxRow lets the user pick one). The invoice number
 * only travels with a factuurbetaling; on any other soort it would end up on
 * the mutation line where e-boekhouden does not expect it.
 */
export function toInboxProcessItem(item: InboxClassification, relatieId?: number): InboxProcessItem {
  const payload: InboxProcessItem = {
    id: item.id,
    grootboekId: item.grootboekId || 0,
    soort: MUTATIE_SOORT_CODES[item.soort] || MUTATIE_SOORT_CODES.GeldUitgegeven,
    grootboekcode: item.grootboekcode,
    btwCode: item.btwCode,
    omschrijving: item.aiOmschrijving || item.omschrijving,
    bedrag: Math.abs(item.bedrag),
  };
  const relatie = relatieId ?? item.relatieId;
  if (relatie) payload.relatieId = relatie;
  if (isFactuurbetalingSoort(item.soort) && item.factuurnummer) {
    payload.factuurnummer = item.factuurnummer;
    payload.btwCode = "GEEN";
  }
  return payload;
}

/**
 * Finds the open purchase invoice an incoming amount could be a refund of:
 * the first post whose outstanding amount equals the bank amount to the cent.
 * Best-effort hint only; it never blocks a booking.
 */
export function findRefundCandidate(bedrag: number, posts: OpenPostGridItem[]): OpenPostGridItem | undefined {
  if (!(bedrag > 0)) return undefined;
  return posts.find((p) => p.teOntvangen > 0 && Math.round(Math.abs(p.teOntvangen - bedrag) * 100) < 1);
}

/** Accessible name of the "Koppelen" button on an open-post row. */
export function koppelenLabel(item: InboxClassification): string {
  const factuur = item.factuurnummer ? ` aan factuur ${item.factuurnummer}` : " aan openstaande factuur";
  const van = item.relatieNaam ? ` van ${item.relatieNaam}` : "";
  return `Koppel betaling${factuur}${van}`;
}

/** Row indicator for a bank line that pays an analysed mail-folder document. */
export function documentMatchIndicator(title: string): string {
  return `Factuur ${title} staat in je mailmap`;
}
