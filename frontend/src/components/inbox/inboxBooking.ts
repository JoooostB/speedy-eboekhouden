import type {
  InboxClassification,
  InboxProcessItem,
  InboxVerificationKind,
  LedgerAccount,
  MutatieSoort,
  OpenPostGridItem,
} from "../../api/types";
import { MUTATIE_SOORT_CODES } from "../../api/types";
import { formatMoney } from "../../hooks/mailDocumentsModel";

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

/** Accessible name of the "Koppelen (n)" button on a payment-group row:
 *  "Koppel 3 betalingen aan factuur VF-2026-0017 van Voorbeeld Bouw". */
export function koppelenGroupLabel(count: number, factuurnummer: string, relatieNaam: string): string {
  const betalingen = count === 1 ? "1 betaling" : `${count} betalingen`;
  const factuur = factuurnummer ? ` aan factuur ${factuurnummer}` : " aan openstaande factuur";
  const van = relatieNaam ? ` van ${relatieNaam}` : "";
  return `Koppel ${betalingen}${factuur}${van}`;
}

/** Row indicator for a bank line that pays an analysed mail-folder document. */
export function documentMatchIndicator(title: string): string {
  return `Factuur ${title} staat in je mailmap`;
}

// Verification payments (card and IBAN checks), booked on Bijzondere baten.

/** True when the backend recognised the line as a card or IBAN check. */
export function isVerification(item: Pick<InboxClassification, "verification">): boolean {
  return !!item.verification;
}

export const VERIFICATION_CHIP_LABELS: Record<InboxVerificationKind, string> = {
  card_check: "Kaartcontrole",
  iban_check: "Rekeningcontrole",
};

export const VERIFICATION_CHIP_TOOLTIPS: Record<InboxVerificationKind, string> = {
  card_check:
    "Een webwinkel schrijft een klein bedrag af om je betaalpas te controleren en stort het terug. Speedy boekt beide op Bijzondere baten.",
  iban_check: "Een kleine overboeking die controleert of dit rekeningnummer van jou is. Speedy boekt het op Bijzondere baten.",
};

/** The slate chip of a verification payment: neither a cost nor revenue, so
 *  a colour no other chip in the list uses. #334155 on the tinted background
 *  over white is about 8:1. Text label always, never colour alone. Rendered
 *  as a button so the tooltip reaches keyboard and screen-reader users. */
export const VERIFICATION_CHIP_SX = {
  fontWeight: 600,
  fontSize: "0.6875rem",
  height: 22,
  flexShrink: 0,
  bgcolor: "rgba(100, 116, 139, 0.14)",
  color: "#334155",
  cursor: "help",
  border: 0,
  "&:focus-visible": { outline: "2px solid", outlineColor: "primary.main", outlineOffset: 2 },
} as const;

/** The info line in the confirm dialog: a verification payment needs no invoice. */
export const VERIFICATION_NO_INVOICE = "Controlebetaling: geen factuur nodig.";

/** "9300 Bijzondere baten" for a ledger code, as the account is really
 *  called (the 9300 fallback may carry another name); the bare code when the
 *  account list does not know it. */
export function ledgerLabel(code: string, accounts: ReadonlyArray<Pick<LedgerAccount, "code" | "omschrijving">>): string {
  if (!code) return "";
  const acc = accounts.find((a) => a.code === code);
  return acc ? `${acc.code} ${acc.omschrijving}`.trim() : code;
}

/** "Afgeschreven € 1,00" / "Bijgeschreven € 1,00": the amount read out with
 *  the direction in words, so the sign never depends on colour or a glyph. */
export function amountAriaLabel(bedrag: number): string {
  const direction = bedrag < 0 ? "Afgeschreven" : "Bijgeschreven";
  return `${direction} ${formatMoney(Math.abs(bedrag))}`;
}

/** Accessible name of the "Boeken" button on a verification row. */
export function boekenVerificationLabel(
  item: Pick<InboxClassification, "bedrag" | "grootboekcode">,
  accounts: ReadonlyArray<Pick<LedgerAccount, "code" | "omschrijving">>,
): string {
  const label = ledgerLabel(item.grootboekcode, accounts);
  return `Boek controlebetaling van ${formatMoney(Math.abs(item.bedrag))}${label ? ` op ${label}` : ""}`;
}
