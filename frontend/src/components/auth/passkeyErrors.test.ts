import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/client";
import { PASSKEY_MESSAGES, isBackendMessage, passkeyErrorMessage, type PasskeyCeremony } from "./passkeyErrors";

function browserError(name: string, message = "The operation failed."): Error {
  const err = new Error(message);
  err.name = name;
  return err;
}

const ceremonies: PasskeyCeremony[] = ["login", "register", "recovery"];

describe("passkeyErrorMessage: challenge expiry", () => {
  // The bug: "challenge not found or expired" matched "not found" and
  // showed "Speedy kent deze passkey niet meer"; "challenge expired"
  // matched "challenge" and showed the raw English text.
  it.each(ceremonies)("says the %s took too long, for both backend texts", (ceremony) => {
    for (const text of ["challenge not found or expired", "challenge expired"]) {
      const msg = passkeyErrorMessage(new ApiError(401, text, text), ceremony);
      expect(msg).toBe(PASSKEY_MESSAGES.expired[ceremony]);
      expect(msg).not.toMatch(/challenge|passkey niet meer/i);
    }
  });

  it("asks for a new recovery link, because the token is used up", () => {
    expect(passkeyErrorMessage(new ApiError(400, "challenge expired"), "recovery")).toBe(
      "Het herstel duurde te lang. Vraag hieronder een nieuwe herstellink aan.",
    );
  });
});

describe("passkeyErrorMessage: the browser", () => {
  it("maps the WebAuthn exceptions per ceremony", () => {
    expect(passkeyErrorMessage(browserError("NotAllowedError"), "login")).toBe(PASSKEY_MESSAGES.cancelled.login);
    expect(passkeyErrorMessage(browserError("NotAllowedError"), "register")).toBe(PASSKEY_MESSAGES.cancelled.register);
    expect(passkeyErrorMessage(browserError("NotAllowedError"), "recovery")).toBe(PASSKEY_MESSAGES.cancelled.recovery);
    expect(passkeyErrorMessage(new Error("The operation either timed out or was not allowed."), "login")).toBe(
      PASSKEY_MESSAGES.cancelled.login,
    );
    expect(passkeyErrorMessage(browserError("SecurityError"), "login")).toBe(PASSKEY_MESSAGES.security);
    expect(passkeyErrorMessage(browserError("InvalidStateError"), "register")).toBe(PASSKEY_MESSAGES.alreadyRegistered);
    expect(passkeyErrorMessage(browserError("AbortError"), "login")).toBe(PASSKEY_MESSAGES.aborted.login);
  });
});

describe("passkeyErrorMessage: login answers about the passkey", () => {
  it("recognises the backend's coded answers", () => {
    const notFound =
      "passkey_not_found: deze passkey bestaat niet meer. Verwijder hem uit je browser (Instellingen → Wachtwoorden) en registreer opnieuw.";
    const mismatch =
      "passkey_mismatch: deze passkey hoort bij een ander account. Verwijder oude passkeys voor dit domein uit je browser.";
    expect(passkeyErrorMessage(new ApiError(401, notFound), "login")).toBe(PASSKEY_MESSAGES.unknownPasskey);
    expect(passkeyErrorMessage(new ApiError(401, mismatch), "login")).toBe(PASSKEY_MESSAGES.mismatch);
  });

  it("keeps the plain-English variants", () => {
    expect(passkeyErrorMessage(new ApiError(401, "credential not found"), "login")).toBe(PASSKEY_MESSAGES.unknownPasskey);
    expect(passkeyErrorMessage(new ApiError(401, "User handle and User ID do not match"), "login")).toBe(
      PASSKEY_MESSAGES.mismatch,
    );
    expect(passkeyErrorMessage(new Error("No credentials available"), "login")).toBe(PASSKEY_MESSAGES.noPasskey);
  });
});

describe("passkeyErrorMessage: backend messages", () => {
  it("shows the Dutch rate-limit answer everywhere", () => {
    const text = "Te veel pogingen. Probeer het over 5 minuten opnieuw.";
    for (const ceremony of ceremonies) {
      expect(passkeyErrorMessage(new ApiError(429, text), ceremony)).toBe(text);
    }
  });

  it("shows the Dutch registration answers and hides the internal English ones", () => {
    const exists = "Er bestaat al een account met dit e-mailadres. Log in met je passkey.";
    expect(passkeyErrorMessage(new ApiError(409, exists), "register")).toBe(exists);
    expect(passkeyErrorMessage(new ApiError(400, "finishing registration: Error validating origin"), "register")).toBe(
      PASSKEY_MESSAGES.failed.register,
    );
    expect(passkeyErrorMessage(new ApiError(401, "login mislukt: Error validating the assertion signature"), "login")).toBe(
      PASSKEY_MESSAGES.failed.login,
    );
  });

  // Bug 3: recovery replaced every backend answer with "Herstel mislukt".
  it("shows every recovery answer of the backend", () => {
    for (const [status, text] of [
      [400, "Deze herstellink is ongeldig, verlopen of al gebruikt. Vraag een nieuwe aan."],
      [400, "De herstellink is onvolledig. Open de link uit de e-mail opnieuw."],
      [500, "Het account bij deze herstellink bestaat niet meer."],
      [500, "De nieuwe passkey kon niet worden opgeslagen. Vraag een nieuwe herstellink aan."],
      [400, "Deze herstelaanvraag is ongeldig. Vraag hieronder een nieuwe herstellink aan."],
    ] as const) {
      expect(passkeyErrorMessage(new ApiError(status, text), "recovery")).toBe(text);
    }
  });

  it("does not show what the proxy says when the backend is unreachable", () => {
    expect(passkeyErrorMessage(new ApiError(502, "Bad Gateway"), "recovery")).toBe(PASSKEY_MESSAGES.failed.recovery);
    expect(passkeyErrorMessage(new ApiError(504, ""), "login")).toBe(PASSKEY_MESSAGES.failed.login);
    // A non-JSON error page makes res.json() throw before an ApiError exists.
    expect(passkeyErrorMessage(new SyntaxError("Unexpected token '<'"), "recovery")).toBe(PASSKEY_MESSAGES.failed.recovery);
  });

  it("isBackendMessage accepts backend answers only (the recovery request form uses it)", () => {
    expect(isBackendMessage(new ApiError(400, "Vul een geldig e-mailadres in."))).toBe(true);
    expect(isBackendMessage(new ApiError(429, "Te veel pogingen. Probeer het over 15 minuten opnieuw."))).toBe(true);
    expect(isBackendMessage(new ApiError(502, "Bad Gateway"))).toBe(false);
    expect(isBackendMessage(new ApiError(400, " "))).toBe(false);
    expect(isBackendMessage(new TypeError("Failed to fetch"))).toBe(false);
  });

  it("falls back per ceremony for anything else", () => {
    expect(passkeyErrorMessage(undefined, "login")).toBe("Inloggen mislukt. Probeer het opnieuw.");
    expect(passkeyErrorMessage(null, "register")).toBe("Registratie mislukt. Probeer het opnieuw.");
    expect(passkeyErrorMessage(new Error("boom"), "recovery")).toBe(
      "Herstel mislukt. Vraag hieronder een nieuwe herstellink aan.",
    );
  });
});
