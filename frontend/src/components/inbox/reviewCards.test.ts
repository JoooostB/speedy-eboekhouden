import { describe, it, expect } from "vitest";
import {
  focusAfterRemoval,
  removeCard,
  reviewCardId,
  skipAnnouncement,
  skipButtonAriaLabel,
  updateCard,
} from "./reviewCards";

const cards = () => [
  { id: reviewCardId(0), naam: "a" },
  { id: reviewCardId(1), naam: "b" },
  { id: reviewCardId(2), naam: "c" },
];

describe("reviewCardId", () => {
  it("is unique per position", () => {
    expect(new Set([0, 1, 2].map(reviewCardId)).size).toBe(3);
  });
});

describe("updateCard", () => {
  it("updates only the card with the id", () => {
    const next = updateCard(cards(), "card-1", (c) => ({ ...c, naam: "B" }));
    expect(next.map((c) => c.naam)).toEqual(["a", "B", "c"]);
  });

  it("finds the card by id after another card was taken out", () => {
    // The position of card-2 moved from 2 to 1; an update by id still hits it.
    const next = updateCard(removeCard(cards(), "card-0"), "card-2", (c) => ({ ...c, naam: "C" }));
    expect(next.map((c) => c.naam)).toEqual(["b", "C"]);
  });

  it("is a no-op for a card that was taken out", () => {
    const list = removeCard(cards(), "card-1");
    expect(updateCard(list, "card-1", (c) => ({ ...c, naam: "x" }))).toBe(list);
  });

  it("returns the same list when the update changes nothing", () => {
    const list = cards();
    expect(updateCard(list, "card-0", (c) => c)).toBe(list);
  });
});

describe("removeCard", () => {
  it("drops exactly the card with the id", () => {
    expect(removeCard(cards(), "card-1").map((c) => c.id)).toEqual(["card-0", "card-2"]);
    expect(removeCard(cards(), "card-9")).toHaveLength(3);
  });
});

describe("focusAfterRemoval", () => {
  const ids = ["card-0", "card-1", "card-2"];
  it("moves to the next card", () => {
    expect(focusAfterRemoval(ids, "card-0")).toBe("card-1");
    expect(focusAfterRemoval(ids, "card-1")).toBe("card-2");
  });
  it("moves to the previous card when the last one goes", () => {
    expect(focusAfterRemoval(ids, "card-2")).toBe("card-1");
  });
  it("has nowhere to go for the only card or an unknown id", () => {
    expect(focusAfterRemoval(["card-0"], "card-0")).toBeNull();
    expect(focusAfterRemoval(ids, "card-9")).toBeNull();
  });
});

describe("texts", () => {
  it("names the document", () => {
    expect(skipButtonAriaLabel("Voorbeeld Leverancier · INV-0042")).toBe("Voorbeeld Leverancier · INV-0042 niet boeken");
    expect(skipAnnouncement("factuur.pdf")).toBe("factuur.pdf wordt niet geboekt");
  });
});
