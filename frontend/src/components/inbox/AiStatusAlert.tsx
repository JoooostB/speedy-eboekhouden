import type { ReactElement } from "react";
import { Alert, AlertTitle, Button } from "@mui/material";
import type { SxProps, Theme } from "@mui/material";

/** Result of GET /settings/api-key/status. */
export interface AiStatus {
  status: string;
  message?: string;
}

/** Statuses in which Claude cannot be called at all. */
export function aiBlocked(status: AiStatus | null | undefined): boolean {
  return !!status && (status.status === "not_configured" || status.status === "no_credits" || status.status === "invalid");
}

/** Short reason for disabled AI buttons, or null when AI is usable. */
export function aiBlockedReason(status: AiStatus | null | undefined): string | null {
  switch (status?.status) {
    case "not_configured":
      return "AI-analyse is niet actief: stel eerst een API-sleutel in";
    case "no_credits":
      return "AI-tegoed is op";
    case "invalid":
      return "API-sleutel is ongeldig";
    default:
      return null;
  }
}

interface Props {
  status: AiStatus | null;
  /** "bank": the classification alert of the Afschriften tab, shown by the
   *  parent when nothing was classified. "documents": the analysis alert of
   *  the Documenten tab, shown whenever AI is blocked. */
  variant: "bank" | "documents";
  sx?: SxProps<Theme>;
}

/** The alert's action button, or undefined so MUI renders no action slot. */
function statusAction(status: AiStatus): ReactElement | undefined {
  if (status.status === "not_configured") {
    return (
      <Button color="inherit" size="small" href="/app/instellingen" sx={{ fontWeight: 600 }}>
        Instellen
      </Button>
    );
  }
  if (status.status === "no_credits") {
    return (
      <Button
        color="inherit" size="small"
        href="https://console.anthropic.com/settings/plans"
        target="_blank" rel="noopener noreferrer"
        sx={{ fontWeight: 600 }}
      >
        Tegoed opwaarderen
      </Button>
    );
  }
  return undefined;
}

/**
 * AiStatusAlert — tells the user why AI is not doing its work and what to do
 * about it. Shared by the bank-line inbox and the documents inbox so both
 * tabs explain a missing key or an empty balance the same way.
 */
export function AiStatusAlert({ status, variant, sx }: Props) {
  if (!status) return null;

  if (variant === "bank") {
    return (
      <Alert
        severity={status.status === "no_credits" ? "warning" : status.status === "active" ? "info" : "info"}
        sx={sx}
        action={statusAction(status)}
      >
        <AlertTitle sx={{ fontWeight: 600 }}>
          {status.status === "not_configured" && "AI-classificatie is niet actief"}
          {status.status === "no_credits" && "AI-tegoed is op"}
          {status.status === "invalid" && "API-sleutel is ongeldig"}
          {status.status === "active" && "AI is beschikbaar — klik op 'Vernieuwen' om te classificeren"}
          {status.status === "error" && "AI-status kon niet worden gecontroleerd"}
        </AlertTitle>
        {status.status === "not_configured" && (
          "Met een Anthropic API-sleutel classificeert Speedy je afschriftregels automatisch. Bankkosten, abonnementen en leveranciersbetalingen worden herkend en met een klik verwerkt."
        )}
        {status.status === "no_credits" && (
          "Je API-sleutel is geldig, maar je tegoed bij Anthropic is op. Waardeer je tegoed op om AI-classificatie te hervatten."
        )}
        {status.status === "invalid" && (
          "Controleer je API-sleutel in de instellingen."
        )}
      </Alert>
    );
  }

  // Documents: the list works without AI, only the analysis does not.
  if (!aiBlocked(status)) return null;
  return (
    <Alert
      severity={status.status === "no_credits" ? "warning" : "info"}
      sx={sx}
      action={statusAction(status)}
    >
      <AlertTitle sx={{ fontWeight: 600 }}>
        {status.status === "not_configured" && "Geen API-sleutel ingesteld"}
        {status.status === "no_credits" && "AI-tegoed is op"}
        {status.status === "invalid" && "API-sleutel is ongeldig"}
      </AlertTitle>
      AI-analyse is niet actief — je kunt documenten wel bekijken en negeren.{" "}
      {status.status === "not_configured" && (
        "Met een Anthropic API-sleutel leest Speedy je facturen uit en koppelt ze aan je betalingen."
      )}
      {status.status === "no_credits" && (
        "Je API-sleutel is geldig, maar je tegoed bij Anthropic is op. Waardeer je tegoed op om AI-analyse te hervatten."
      )}
      {status.status === "invalid" && (
        "Controleer je API-sleutel in de instellingen."
      )}
    </Alert>
  );
}
