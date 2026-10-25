import { ApiError } from "../../api/client";

/**
 * Dutch messages for what can go wrong in a passkey ceremony: the browser's
 * WebAuthn errors and the backend's answers. Pure, so it is unit-tested.
 *
 * Order matters: the backend's "challenge not found or expired" contains
 * "not found", so challenge expiry is recognised before anything that
 * looks for a missing passkey.
 */

export type PasskeyCeremony = "login" | "register" | "recovery";

/** The recovery token is consumed when the ceremony starts, so every
 *  failure after that needs a new link; the request form sits below. */
const NEW_LINK = "Vraag hieronder een nieuwe herstellink aan.";

export const PASSKEY_MESSAGES = {
  expired: {
    login: "Het inloggen duurde te lang. Probeer het opnieuw.",
    register: "Het aanmaken van je account duurde te lang. Probeer het opnieuw.",
    recovery: `Het herstel duurde te lang. ${NEW_LINK}`,
  },
  cancelled: {
    login: "Passkey-aanvraag geannuleerd of verlopen. Probeer het opnieuw.",
    register: "Passkey-registratie geannuleerd of verlopen. Probeer het opnieuw.",
    recovery: `Passkey-registratie geannuleerd of verlopen. ${NEW_LINK}`,
  },
  aborted: {
    login: "Aanvraag afgebroken. Probeer het opnieuw.",
    register: "Aanvraag afgebroken. Probeer het opnieuw.",
    recovery: `Aanvraag afgebroken. ${NEW_LINK}`,
  },
  failed: {
    login: "Inloggen mislukt. Probeer het opnieuw.",
    register: "Registratie mislukt. Probeer het opnieuw.",
    recovery: `Herstel mislukt. ${NEW_LINK}`,
  },
  security: "Beveiligingsfout. Controleer of je Speedy via het juiste webadres opent.",
  alreadyRegistered: "Deze passkey is al geregistreerd op dit apparaat.",
  noPasskey: "Geen passkey gevonden. Heb je al een account aangemaakt?",
  unknownPasskey:
    "Speedy kent deze passkey niet meer. Verwijder hem uit je wachtwoordbeheer en vraag via 'Passkey kwijt?' een nieuwe aan.",
  mismatch:
    "Deze passkey past niet bij je account. Verwijder de oude passkey van je apparaat en vraag via 'Passkey kwijt?' een nieuwe aan.",
} as const;

interface ErrorLike {
  name?: unknown;
  message?: unknown;
}

/**
 * Whether the backend wrote this message for the user: an ApiError that
 * did not come from the proxy in front of it (nginx answers 502-504 with an
 * HTML page, so the message is only the HTTP status text).
 */
export function isBackendMessage(err: unknown): err is ApiError {
  return err instanceof ApiError && err.status < 502 && err.message.trim() !== "";
}

export function passkeyErrorMessage(err: unknown, ceremony: PasskeyCeremony): string {
  const e = (err ?? {}) as ErrorLike;
  const name = typeof e.name === "string" ? e.name : "";
  const msg = (typeof e.message === "string" ? e.message : "").toLowerCase();

  // The ceremony took longer than the five minutes a challenge lives, or
  // the server restarted in between. Both backend texts: "challenge not
  // found or expired" and the older "challenge expired".
  if (msg.includes("challenge") && (msg.includes("expired") || msg.includes("not found"))) {
    return PASSKEY_MESSAGES.expired[ceremony];
  }

  // WebAuthn errors from the browser.
  if (name === "NotAllowedError" || msg.includes("not allowed") || msg.includes("timed out")) {
    return PASSKEY_MESSAGES.cancelled[ceremony];
  }
  if (name === "SecurityError" || msg.includes("security")) {
    return PASSKEY_MESSAGES.security;
  }
  if (name === "InvalidStateError" || msg.includes("already registered") || msg.includes("invalid state")) {
    return PASSKEY_MESSAGES.alreadyRegistered;
  }
  if (name === "AbortError" || msg.includes("abort")) {
    return PASSKEY_MESSAGES.aborted[ceremony];
  }

  // Login answers about the passkey itself. The backend prefixes them with
  // a code ("passkey_not_found: …", "passkey_mismatch: …").
  if (msg.startsWith("passkey_mismatch") || msg.includes("user handle") || msg.includes("do not match")) {
    return PASSKEY_MESSAGES.mismatch;
  }
  if (
    msg.startsWith("passkey_not_found") ||
    msg.includes("credential not found") ||
    msg.includes("user not found")
  ) {
    return PASSKEY_MESSAGES.unknownPasskey;
  }
  if (msg.includes("no credentials") || msg.includes("no passkey")) {
    return PASSKEY_MESSAGES.noPasskey;
  }

  if (isBackendMessage(err)) {
    // Rate limiting answers in Dutch on every auth endpoint.
    if (err.status === 429) return err.message;
    // Every recovery endpoint answers in Dutch.
    if (ceremony === "recovery") return err.message;
    // Login and registration mix Dutch answers with internal English ones;
    // only the Dutch ones are shown.
    if (msg.includes("e-mailadres") || msg.includes("geregistreerd")) return err.message;
  }

  return PASSKEY_MESSAGES.failed[ceremony];
}
