import { formatMoney } from "../../hooks/mailDocumentsModel";

/**
 * Amounts in the invoice review dialog. The amount fields are text the user
 * types, shown with a decimal comma ("12,50"); parseAmount turns them into
 * the numbers the booking payload needs, and formatAmountInput writes a
 * computed amount back into a field. The other helpers format for display
 * ("€ 12,50", "CHF 32,80"). Pure, so they are unit-tested.
 */

/** A group separator used correctly: "1.234", "12.345.678". */
function isGrouped(s: string, sep: "." | ","): boolean {
  return new RegExp(`^[1-9]\\d{0,2}(\\${sep}\\d{3})+$`).test(s);
}

/**
 * Reads an amount the way a Dutch user types it, without guessing wrong:
 * "12,50", "1.234,56", "1234,5" and "€ 12,50" are Dutch; "12.50" (how the
 * fields used to be filled) and "1,234.56" are read too. When both
 * separators occur, the last one is the decimal separator. A lone dot
 * followed by exactly three digits ("1.234") is a thousands separator, a
 * lone comma always a decimal comma. Returns null for anything that is not
 * an amount, including more than two decimals ("12,505") and malformed
 * groups, so a typo is refused instead of booked as another amount. Empty
 * input is null as well. A trailing separator ("12,") is accepted, so a
 * field is valid while the user is still typing.
 */
export function parseAmount(input: string): number | null {
  let s = input.replace(/[€\s\u00a0\u202f]/g, "");
  let sign = 1;
  if (s.startsWith("-")) {
    sign = -1;
    s = s.slice(1);
  } else if (s.startsWith("+")) {
    s = s.slice(1);
  }
  if (!/^[\d.,]+$/.test(s) || !/\d/.test(s)) return null;

  const lastDot = s.lastIndexOf(".");
  const lastComma = s.lastIndexOf(",");
  let whole: string;
  let fraction = "";
  if (lastDot >= 0 && lastComma >= 0) {
    const decimalAt = Math.max(lastDot, lastComma);
    const group = decimalAt === lastDot ? "," : ".";
    whole = s.slice(0, decimalAt);
    fraction = s.slice(decimalAt + 1);
    if (whole.includes(s[decimalAt]) || !isGrouped(whole, group)) return null;
    whole = whole.split(group).join("");
  } else {
    const sep = lastDot >= 0 ? "." : lastComma >= 0 ? "," : null;
    const count = sep ? s.split(sep).length - 1 : 0;
    if (sep === null || count === 0) {
      whole = s;
    } else if (count === 1 && !(sep === "." && isGrouped(s, "."))) {
      [whole, fraction] = s.split(sep);
    } else {
      if (!isGrouped(s, sep)) return null;
      whole = s.split(sep).join("");
    }
  }
  if (fraction.length > 2) return null;
  const value = Number(`${whole || "0"}.${fraction || "0"}`);
  return Number.isFinite(value) ? sign * value : null;
}

/** Helper text under an amount field that does not read as an amount. */
export const AMOUNT_HINT = "Vul een bedrag in, bijvoorbeeld 12,50";

/** Whether an amount field shows an error: it holds text that is not an
 *  amount. An empty field is fine unless it is required. */
export function invalidAmountField(value: string, opts: { required?: boolean } = {}): boolean {
  if (value.trim() === "") return opts.required === true;
  return parseAmount(value) === null;
}

/** A computed amount for an amount field: 10.3 → "10,30". */
export function formatAmountInput(amount: number): string {
  return amount.toFixed(2).replace(".", ",");
}

/** A form amount as money: "12,5" → "€ 12,50". Text that is not an amount
 *  is returned as it is. */
export function formatFormAmount(amount: string): string {
  const num = parseAmount(amount);
  return num === null ? amount : formatMoney(num);
}

/** The note under a bank line that was matched to an invoice in another
 *  currency: "Factuur: CHF 32,80, omgerekend naar euro …". */
export function fxInvoiceNote(amount: number | undefined, currency: string): string {
  const shown = typeof amount === "number" && Number.isFinite(amount) ? formatMoney(amount, currency) : currency;
  return `Factuur: ${shown}, omgerekend naar euro om de betaling te vinden. Speedy vult het werkelijke eurobedrag van de bank in.`;
}
