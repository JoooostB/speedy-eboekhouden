import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  List,
  ListItem,
  ListItemText,
  Snackbar,
} from "@mui/material";
import { api } from "../api/client";
import { useAuth } from "../context/AuthContext";
import { track } from "../analytics";
import {
  DISCONNECT_CANCEL_LABEL,
  DISCONNECT_CONFIRM_LABEL,
  DISCONNECT_CONSEQUENCES,
  DISCONNECT_ERROR,
  DISCONNECT_INTRO,
  DISCONNECT_TITLE,
  disconnectResultMessage,
} from "./ebDisconnect";

interface Props {
  open: boolean;
  onClose: () => void;
}

/**
 * Confirmation dialog for "Verbinding verbreken". It spells out what the
 * backend does (log out at e-boekhouden, drop the token, purge the cached
 * administration data) and what stays (learned bookings). After success the
 * auth context flips to "not connected" and a snackbar says whether
 * e-boekhouden confirmed the logout. The snackbar is rendered next to the
 * dialog so it survives the dialog closing; mount the component permanently.
 *
 * Accessibility: focus starts on "Annuleren", the title labels the dialog,
 * and the dialog cannot be dismissed while the request runs.
 */
export function EBoekhoudenDisconnectDialog({ open, onClose }: Props) {
  const { setEBConnected } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [snackbar, setSnackbar] = useState<string | null>(null);

  const handleClose = () => {
    if (busy) return;
    setError("");
    onClose();
  };

  const handleConfirm = async () => {
    setBusy(true);
    setError("");
    try {
      const res = await api.ebDisconnect();
      setEBConnected(false);
      track("EB Disconnect", { ebLoggedOut: String(res.ebLoggedOut), purgedKeys: String(res.purgedKeys) });
      setSnackbar(disconnectResultMessage(res));
      onClose();
    } catch {
      track("EB Disconnect Error");
      setError(DISCONNECT_ERROR);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Dialog
        open={open}
        onClose={handleClose}
        aria-labelledby="eb-disconnect-title"
        aria-describedby="eb-disconnect-description"
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle id="eb-disconnect-title">{DISCONNECT_TITLE}</DialogTitle>
        <DialogContent>
          <DialogContentText id="eb-disconnect-description" sx={{ mb: 1 }}>
            {DISCONNECT_INTRO}
          </DialogContentText>
          <List dense disablePadding component="ul" sx={{ pl: 2, listStyle: "disc" }}>
            {DISCONNECT_CONSEQUENCES.map((text) => (
              <ListItem key={text} component="li" disableGutters sx={{ display: "list-item", py: 0.25 }}>
                <ListItemText primary={text} slotProps={{ primary: { variant: "body2" } }} />
              </ListItem>
            ))}
          </List>
          {error && (
            <Alert severity="error" sx={{ mt: 2 }} role="alert">
              {error}
            </Alert>
          )}
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 2 }}>
          <Button onClick={handleClose} disabled={busy} autoFocus>
            {DISCONNECT_CANCEL_LABEL}
          </Button>
          <Button onClick={handleConfirm} disabled={busy} variant="contained" color="error">
            {busy ? (
              <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
                <CircularProgress size={16} color="inherit" aria-hidden="true" />
                Bezig…
              </Box>
            ) : (
              DISCONNECT_CONFIRM_LABEL
            )}
          </Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={snackbar !== null}
        autoHideDuration={6000}
        onClose={() => setSnackbar(null)}
        anchorOrigin={{ vertical: "bottom", horizontal: "center" }}
      >
        {snackbar ? (
          <Alert severity="success" onClose={() => setSnackbar(null)} variant="filled" sx={{ fontWeight: 600 }}>
            {snackbar}
          </Alert>
        ) : undefined}
      </Snackbar>
    </>
  );
}
