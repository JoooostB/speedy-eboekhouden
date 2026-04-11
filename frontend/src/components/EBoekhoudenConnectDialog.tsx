import { useState, useRef, useEffect } from "react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  Alert,
  Typography,
  Box,
  CircularProgress,
} from "@mui/material";
import { api } from "../api/client";
import { useAuth } from "../context/AuthContext";
import { track } from "../analytics";

/**
 * EBoekhoudenConnectDialog allows users to (re)connect their
 * e-Boekhouden session from anywhere in the app.
 *
 * Design rationale:
 * - Wraps all inputs in a single <form> so Enter submits in both
 *   credential and MFA states.
 * - Trust signal matches the onboarding wizard for consistency.
 * - MFA transition explains where the code comes from.
 * - Loading spinner replaces button text to prevent double-submit.
 * - State resets fully on close so re-opening is clean.
 *
 * Accessibility:
 * - Dialog uses aria-labelledby pointing to the DialogTitle.
 * - Focus auto-moves to the first input on open and on MFA transition.
 * - Form fields have autoComplete hints for password managers.
 */

interface Props {
  open: boolean;
  onClose: () => void;
}

export function EBoekhoudenConnectDialog({ open, onClose }: Props) {
  const { setEBConnected } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [mfaCode, setMfaCode] = useState("");
  const [needsMFA, setNeedsMFA] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const emailRef = useRef<HTMLInputElement>(null);
  const mfaRef = useRef<HTMLInputElement>(null);

  // Focus first field on open and on MFA transition
  useEffect(() => {
    if (open && !needsMFA) {
      const t = setTimeout(() => emailRef.current?.focus(), 100);
      return () => clearTimeout(t);
    }
    if (open && needsMFA) {
      const t = setTimeout(() => mfaRef.current?.focus(), 100);
      return () => clearTimeout(t);
    }
  }, [open, needsMFA]);

  const handleSubmit = async () => {
    setError("");
    setLoading(true);
    try {
      if (needsMFA) {
        await api.ebMfa(mfaCode);
        track("EB Connect", { mfa: "true" });
        setEBConnected(true);
        handleClose();
      } else {
        const res = await api.ebLogin(email, password);
        if (res.status === "mfa_required") {
          setNeedsMFA(true);
          setLoading(false);
          return;
        }
        track("EB Connect");
        setEBConnected(true);
        handleClose();
      }
    } catch (err: any) {
      track("EB Connect Error");
      if (needsMFA) {
        setError(err.message || "Verificatie mislukt. Controleer de code.");
      } else {
        setError(err.message || "Inloggen mislukt. Controleer je gegevens.");
      }
    } finally {
      setLoading(false);
    }
  };

  const handleClose = () => {
    setEmail("");
    setPassword("");
    setMfaCode("");
    setNeedsMFA(false);
    setError("");
    setLoading(false);
    onClose();
  };

  const dialogId = "eb-connect-dialog-title";

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
      aria-labelledby={dialogId}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          handleSubmit();
        }}
        noValidate
      >
        <DialogTitle id={dialogId} sx={{ pb: 0 }}>
          {needsMFA ? "Verificatiecode invoeren" : "Verbinden met e-Boekhouden"}
        </DialogTitle>

        <DialogContent sx={{ pt: 2 }}>
          {!needsMFA ? (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2.5, mt: 1 }}>
                Verbind met je e-Boekhouden account om aan de slag te gaan.
                Voor je veiligheid slaan wij je wachtwoord niet op — daarom
                vragen we je elke sessie opnieuw in te loggen.
              </Typography>

              {error && (
                <Alert severity="error" sx={{ mb: 2 }}>
                  {error}
                </Alert>
              )}

              <TextField
                fullWidth
                label="E-mailadres"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                inputRef={emailRef}
                autoComplete="email"
                sx={{ mb: 2 }}
              />
              <TextField
                fullWidth
                label="Wachtwoord"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                sx={{ mb: 2 }}
              />

              {/* Trust signal */}
              <Box
                sx={{
                  display: "flex",
                  alignItems: "flex-start",
                  gap: 1,
                  p: 1.5,
                  bgcolor: "grey.50",
                  borderRadius: 2,
                }}
              >
                <Box
                  component="svg"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={1.5}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                  sx={{
                    width: 18,
                    height: 18,
                    color: "text.secondary",
                    flexShrink: 0,
                    mt: 0.25,
                  }}
                >
                  <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                </Box>
                <Typography variant="caption" color="text.secondary" sx={{ lineHeight: 1.5 }}>
                  Je wachtwoord wordt doorgestuurd naar e-boekhouden.nl en
                  direct verwijderd. Speedy slaat geen inloggegevens op.
                </Typography>
              </Box>
            </>
          ) : (
            <>
              {error && (
                <Alert severity="error" sx={{ mb: 2, mt: 1 }}>
                  {error}
                </Alert>
              )}

              <Alert severity="info" sx={{ mb: 2, mt: 1 }} icon={false}>
                <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
                  Verificatiecode nodig
                </Typography>
                <Typography variant="body2">
                  e-Boekhouden heeft een code naar je e-mail gestuurd.
                  Vul deze hieronder in om de koppeling te voltooien.
                </Typography>
              </Alert>

              <TextField
                fullWidth
                label="Verificatiecode"
                value={mfaCode}
                onChange={(e) => setMfaCode(e.target.value)}
                inputRef={mfaRef}
                autoComplete="one-time-code"
                inputProps={{ inputMode: "numeric" }}
              />
            </>
          )}
        </DialogContent>

        <DialogActions sx={{ px: 3, pb: 2.5 }}>
          <Button onClick={handleClose} disabled={loading}>
            Annuleren
          </Button>
          <Button
            variant="contained"
            color="primary"
            type="submit"
            disabled={loading}
            sx={{ minWidth: 120 }}
          >
            {loading ? (
              <CircularProgress size={22} color="inherit" aria-label="Bezig" />
            ) : needsMFA ? (
              "Verifieer"
            ) : (
              "Verbinden"
            )}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
