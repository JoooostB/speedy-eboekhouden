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
  "Speedy verwijdert het opgeslagen toegangstoken van e-Boekhouden.",
  "Speedy wist de tijdelijk bewaarde analyses, boekingsvoorstellen, duplicaatmarkeringen en je negeerlijst.",
  "Je geleerde boekingen blijven bewaard. Die kun je verwijderen onder Instellingen.",
  "Je kunt e-Boekhouden op elk moment opnieuw koppelen.",
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
