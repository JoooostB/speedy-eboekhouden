import { useState } from "react";
import { Alert, Box, Button, CircularProgress, Paper, Typography } from "@mui/material";
import { ArchiveFolderPicker } from "../shared/ArchiveFolderPicker";
import type { ArchiveFolderWithPath } from "../../hooks/useArchiveFolders";

interface Props {
  /** Whether e-boekhouden has a mail folder configured (and it exists). */
  configured: boolean;
  /** "missing" when a mail folder is configured but no longer exists. */
  reason?: "missing";
  /** Name of the configured folder, for the empty state. */
  folderName?: string;
  /** Sets up the mail folder: without an id a root folder "Inbox" is made,
   *  with one that existing folder is used. Rejections are shown inline. */
  onSetup: (folderId?: number) => Promise<void>;
}

/**
 * MailFolderSetupCard — the first screen of the documents inbox.
 *
 * Without a mail folder it explains Mail & herken and offers to create the
 * folder ("Mailmap instellen") or pick an existing one. With a folder but
 * no documents it explains where the mail address lives in e-boekhouden
 * and why the address without "herken" is the one to use.
 */
export function MailFolderSetupCard({ configured, reason, folderName, onSetup }: Props) {
  const [choosing, setChoosing] = useState(false);
  const [folder, setFolder] = useState<ArchiveFolderWithPath | null>(null);
  const [submitting, setSubmitting] = useState<"create" | "existing" | null>(null);
  const [error, setError] = useState<string | null>(null);

  const run = async (kind: "create" | "existing", folderId?: number) => {
    setSubmitting(kind);
    setError(null);
    try {
      await onSetup(folderId);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Mailmap instellen mislukt");
    } finally {
      setSubmitting(null);
    }
  };

  if (configured) {
    return (
      <Paper variant="outlined" sx={{ borderRadius: 2, p: { xs: 3, sm: 4 } }}>
        <Typography variant="h6" component="h2" gutterBottom>
          {folderName ? `Je mailmap "${folderName}" is leeg` : "Je mailmap is leeg"}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
          Je vindt je mailadres in e-Boekhouden onder Digitaal archief → Mail &amp; herken. Gebruik het adres{" "}
          <Box component="strong" sx={{ color: "text.primary" }}>zonder</Box> 'herken': Speedy leest de facturen zelf uit.
        </Typography>
        <Typography variant="body2" color="text.secondary">
          Tip: stel in je mailprogramma een doorstuurregel in voor facturen, dan komen ze hier automatisch binnen.
        </Typography>
      </Paper>
    );
  }

  return (
    <Paper variant="outlined" sx={{ borderRadius: 2, p: { xs: 3, sm: 4 } }}>
      <Typography variant="h6" component="h2" gutterBottom>
        Facturen automatisch binnenhalen
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: reason === "missing" ? 1 : 2.5 }}>
        Stuur facturen naar je mailadres van e-Boekhouden. Speedy leest ze uit en zoekt de bijbehorende betalingen.
      </Typography>
      {reason === "missing" && (
        <Alert severity="warning" sx={{ mb: 2.5 }}>
          Je mailmap bestaat niet meer. Stel een nieuwe mailmap in om verder te gaan.
        </Alert>
      )}

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {!choosing ? (
        <Box sx={{ display: "flex", gap: 1.5, flexWrap: "wrap" }}>
          <Button
            variant="contained"
            onClick={() => run("create")}
            disabled={submitting !== null}
            startIcon={submitting === "create" ? <CircularProgress size={16} color="inherit" aria-hidden="true" /> : undefined}
          >
            Mailmap instellen
          </Button>
          <Button variant="outlined" onClick={() => setChoosing(true)} disabled={submitting !== null}>
            Bestaande map kiezen
          </Button>
        </Box>
      ) : (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, maxWidth: 480 }}>
          <ArchiveFolderPicker
            value={folder}
            onChange={setFolder}
            label="Mailmap"
            helperText="Mail & herken zet je bijlagen voortaan in deze map."
            disabled={submitting !== null}
            autoFocus
          />
          <Box sx={{ display: "flex", gap: 1.5, flexWrap: "wrap" }}>
            <Button
              variant="contained"
              onClick={() => folder && run("existing", folder.id)}
              disabled={!folder || submitting !== null}
              startIcon={submitting === "existing" ? <CircularProgress size={16} color="inherit" aria-hidden="true" /> : undefined}
            >
              Deze map gebruiken
            </Button>
            <Button
              variant="text"
              onClick={() => {
                setChoosing(false);
                setFolder(null);
              }}
              disabled={submitting !== null}
            >
              Annuleren
            </Button>
          </Box>
        </Box>
      )}
    </Paper>
  );
}
