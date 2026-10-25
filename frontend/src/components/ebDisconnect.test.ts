import { describe, expect, it } from "vitest";
import {
  DISCONNECT_CANCEL_LABEL,
  DISCONNECT_CONFIRM_LABEL,
  DISCONNECT_CONSEQUENCES,
  DISCONNECT_ERROR,
  DISCONNECT_TITLE,
  disconnectResultMessage,
} from "./ebDisconnect";

describe("disconnectResultMessage", () => {
  it("claims a logout only when e-boekhouden confirmed it", () => {
    expect(disconnectResultMessage({ ebLoggedOut: true })).toBe(
      "Verbinding verbroken en afgemeld bij e-Boekhouden",
    );
  });

  it("is honest when e-boekhouden did not confirm the logout", () => {
    expect(disconnectResultMessage({ ebLoggedOut: false })).toBe(
      "Verbinding in Speedy verbroken. e-Boekhouden laat de sessie zelf verlopen.",
    );
  });
});

describe("disconnect dialog texts", () => {
  it("explains logout, token, caches, learned bookings and reconnecting", () => {
    const all = DISCONNECT_CONSEQUENCES.join(" ");
    expect(all).toContain("meldt je af bij e-Boekhouden");
    expect(all).toContain("toegangstoken");
    expect(all).toContain("duplicaatmarkeringen");
    expect(all).toContain("negeerlijst");
    expect(all).toContain("geleerde boekingen blijven bewaard");
    expect(all).toContain("opnieuw koppelen");
    expect(DISCONNECT_CONSEQUENCES).toHaveLength(5);
  });

  it("uses the agreed labels", () => {
    expect(DISCONNECT_TITLE).toBe("Verbinding met e-Boekhouden verbreken?");
    expect(DISCONNECT_CONFIRM_LABEL).toBe("Verbinding verbreken");
    expect(DISCONNECT_CANCEL_LABEL).toBe("Annuleren");
    expect(DISCONNECT_ERROR).toMatch(/mislukt/);
  });

  it("follows the Dutch house rules", () => {
    const texts = [DISCONNECT_TITLE, DISCONNECT_ERROR, ...DISCONNECT_CONSEQUENCES, disconnectResultMessage({ ebLoggedOut: false })];
    for (const t of texts) {
      expect(t).not.toMatch(/wordt je/);
      expect(t).not.toMatch(/toegangs token|duplicaat markeringen|negeer lijst/);
      expect(t).not.toMatch(/—/);
    }
  });
});
