/**
 * Pure rules for the cards of the review dialog (InvoiceReviewDialog): every
 * card has a stable id from the moment the list is built, so a card can be
 * taken out ("Niet boeken") without an asynchronous update or React's
 * reconciliation landing on the card that moved into its place. No React
 * and no API calls, so it is unit-tested.
 */

/** A card in the review list. */
export interface ReviewCard {
  id: string;
}

/** The id of the card built from the n-th analysed document. Unique for
 *  the life of a dialog: the list is built once and only ever shrinks. */
export function reviewCardId(n: number): string {
  return `card-${n}`;
}

/** Replaces the card with this id by update(card). The list itself is
 *  returned when the card is gone (taken out while an update was still on
 *  its way) or update returns the same card, so a late write-back is a
 *  no-op instead of landing on another card. */
export function updateCard<T extends ReviewCard>(cards: T[], id: string, update: (card: T) => T): T[] {
  const index = cards.findIndex((c) => c.id === id);
  if (index < 0) return cards;
  const updated = update(cards[index]);
  if (updated === cards[index]) return cards;
  const next = [...cards];
  next[index] = updated;
  return next;
}

/** The list without the card with this id. */
export function removeCard<T extends ReviewCard>(cards: T[], id: string): T[] {
  return cards.filter((c) => c.id !== id);
}

/** Where focus goes when the card with this id is taken out: the card
 *  after it, or the one before it when it was the last; null when it was
 *  the only card (the dialog then closes) or is not in the list. */
export function focusAfterRemoval(ids: readonly string[], removedId: string): string | null {
  const index = ids.indexOf(removedId);
  if (index < 0) return null;
  return ids[index + 1] ?? ids[index - 1] ?? null;
}

/** The visible label of the button that takes a card out. */
export const SKIP_BUTTON_LABEL = "Niet boeken";

/** The accessible name of that button, naming the document. */
export function skipButtonAriaLabel(title: string): string {
  return `${title} niet boeken`;
}

/** What the polite live region says after a card was taken out. */
export function skipAnnouncement(title: string): string {
  return `${title} wordt niet geboekt`;
}
