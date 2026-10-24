import type { EBDisconnectResponse } from "../api/types";

/**
 * Text and outcome logic of "Verbinding verbreken", kept apart from the
 * dialog so the wording and the honesty rule (only claim a logout when
 * e-boekhouden confirmed it) are unit-tested.
 */

export const DISCONNECT_TITLE = "Verbinding met e-Boekhouden verbreken?";
export const DISCONNECT_INTRO = "Dit gebeurt er:";
export const DISCONNECT_CONFIRM_LABEL = "Verbinding verbreken";
export const DISCONNECT_CANCEL_LABEL = "Annuleren";

/** What the backend does on disconnect, in that order, plus what stays. */
export const DISCONNECT_CONSEQUENCES: readonly string[] = [
  "Speedy meldt je af bij e-Boekhouden.",
  "De verbindingstoken wordt uit Speedy verwijderd.",
  "Tijdelijk opgeslagen analyses, suggesties, duplicaatmarkeringen en de negeerlijst van deze administratie worden verwijderd.",
  "Geleerde boekingen blijven staan (verwijderbaar via Instellingen).",
  "Je kunt op elk moment opnieuw verbinden.",
];

/** Shown when the disconnect request itself failed; nothing changed then. */
export const DISCONNECT_ERROR = "Verbinding verbreken mislukt. Probeer het opnieuw.";

/**
 * Snackbar text after a successful disconnect. Speedy only says "afgemeld"
 * when e-boekhouden confirmed that the session is gone; otherwise it says
 * what really happened.
 */
export function disconnectResultMessage(resp: Pick<EBDisconnectResponse, "ebLoggedOut">): string {
  return resp.ebLoggedOut
    ? "Verbinding verbroken en afgemeld bij e-Boekhouden"
    : "Verbinding in Speedy verbroken. e-Boekhouden laat de sessie zelf verlopen.";
}
