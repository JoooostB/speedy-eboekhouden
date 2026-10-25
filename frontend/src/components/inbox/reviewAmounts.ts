import { formatMoney } from "../../hooks/mailDocumentsModel";

/**
 * Amounts as the invoice review dialog shows them, in nl-NL notation
 * ("€ 12,50", "CHF 32,80"). The dialog keeps amounts as form strings with a
 * decimal point ("12.50"), which is what the booking payload needs; these
 * helpers only format for display. Pure, so they are unit-tested.
 */

/** A form amount as money: "12.5" → "€ 12,50". Text that is not a number
 *  is returned as it is. */
export function formatFormAmount(amount: string): string {
  const num = parseFloat(amount);
  return Number.isFinite(num) ? formatMoney(num) : amount;
}

/** The note under a bank line that was matched to an invoice in another
 *  currency: "Factuur: CHF 32,80, omgerekend naar euro …". */
export function fxInvoiceNote(amount: number | undefined, currency: string): string {
  const shown = typeof amount === "number" && Number.isFinite(amount) ? formatMoney(amount, currency) : currency;
  return `Factuur: ${shown}, omgerekend naar euro om de betaling te vinden. Speedy vult het werkelijke eurobedrag van de bank in.`;
}
