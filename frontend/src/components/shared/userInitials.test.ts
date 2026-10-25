import { describe, expect, it } from "vitest";
import { userInitials } from "./userInitials";

describe("userInitials", () => {
  it("takes the first letters of the first two words", () => {
    expect(userInitials("Voorbeeld Gebruiker")).toBe("VG");
    expect(userInitials("anna de vries")).toBe("AD");
    expect(userInitials("Voorbeeld")).toBe("V");
  });

  it("ignores extra spaces", () => {
    expect(userInitials("  Voorbeeld   Gebruiker ")).toBe("VG");
  });

  it("shows a question mark without a name", () => {
    expect(userInitials("")).toBe("?");
    expect(userInitials("   ")).toBe("?");
    expect(userInitials(undefined)).toBe("?");
    expect(userInitials(null)).toBe("?");
  });
});
