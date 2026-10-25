import { describe, expect, it } from "vitest";
import { ApiError } from "./api/client";
import { errorClass, sanitizeTrackProps } from "./analytics";

describe("sanitizeTrackProps", () => {
  it("keeps enum tokens, counts and flags", () => {
    expect(
      sanitizeTrackProps({ status: "receipt_waiting", mode: "invoice", count: 3, ok: true, soort: "GeldUitgegeven", code: "http_412" }),
    ).toEqual({ status: "receipt_waiting", mode: "invoice", count: "3", ok: "true", soort: "GeldUitgegeven", code: "http_412" });
  });

  it("drops free text", () => {
    expect(
      sanitizeTrackProps({
        reason: "Inloggen bij e-Boekhouden mislukt. Controleer je gegevens.",
        leverancier: "Voorbeeld Leverancier B.V.",
        bedrag: "€ 1.234,56",
        omschrijving: "SEPA Overboeking IBAN NL00BANK0123456789",
        bestand: "factuur 2026.pdf",
      }),
    ).toEqual({});
  });

  it("rounds numbers and drops the non-finite ones", () => {
    expect(sanitizeTrackProps({ confidence: 87.6, total: 12, nan: NaN, inf: Infinity })).toEqual({ confidence: "88", total: "12" });
  });

  it("accepts the stringified counts the existing call sites pass", () => {
    expect(sanitizeTrackProps({ count: String(5), confidence: String(Math.round(0.876 * 100)), mfa: "true" })).toEqual({
      count: "5",
      confidence: "88",
      mfa: "true",
    });
  });
});

describe("errorClass", () => {
  it("uses the backend code when it is a token", () => {
    expect(errorClass(new ApiError(400, "Stel eerst een Anthropic API-sleutel in", "no_api_key"))).toBe("no_api_key");
    expect(errorClass(new ApiError(412, "Je e-Boekhouden sessie is verlopen.", "eboekhouden_session_expired"))).toBe(
      "eboekhouden_session_expired",
    );
  });

  it("falls back to the status when the code is a Dutch message", () => {
    expect(errorClass(new ApiError(400, "Bedrag moet positief zijn", "Bedrag moet positief zijn"))).toBe("http_400");
    expect(errorClass(new ApiError(502, "Bad Gateway"))).toBe("http_502");
  });

  it("uses the error name, never the message, for other errors", () => {
    const webauthn = Object.assign(new Error("The operation either timed out or was not allowed."), { name: "NotAllowedError" });
    expect(errorClass(webauthn)).toBe("NotAllowedError");
    expect(errorClass(new Error("Failed to fetch"))).toBe("Error");
    expect(errorClass(new TypeError("x"))).toBe("TypeError");
  });

  it("is unknown for non-errors", () => {
    expect(errorClass("boom")).toBe("unknown");
    expect(errorClass(undefined)).toBe("unknown");
  });
});

describe("track() call sites", () => {
  // Every source file, as text, through Vite's raw import; the test files
  // and this module itself are left out.
  const sources = import.meta.glob("./**/*.{ts,tsx}", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

  const calls: Array<{ file: string; call: string }> = [];
  for (const [file, text] of Object.entries(sources)) {
    if (/\.test\.tsx?$/.test(file) || file.endsWith("/analytics.ts")) continue;
    for (const m of text.matchAll(/\btrack\(([\s\S]*?)\);/g)) {
      calls.push({ file, call: m[1] });
    }
  }

  it("finds the call sites", () => {
    expect(calls.length).toBeGreaterThan(20);
  });

  it("never pass error messages, names, amounts or descriptions", () => {
    const leaky = /\.message\b|JSON\.stringify|\berr\.name\b|omschrijving|leverancier|bedrag|filename|\.naam\b|email/i;
    const offenders = calls.filter((c) => leaky.test(c.call)).map((c) => `${c.file}: track(${c.call.trim()})`);
    expect(offenders).toEqual([]);
  });
});
