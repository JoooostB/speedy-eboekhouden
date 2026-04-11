import { useState, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import {
  Box,
  Paper,
  Typography,
  TextField,
  Button,
  Alert,
  CircularProgress,
} from "@mui/material";
import { startRegistration } from "@simplewebauthn/browser";
import { api } from "../../api/client";
import { useAuth } from "../../context/AuthContext";
import { track } from "../../analytics";

export function PasskeyRecovery() {
  const { refreshMe } = useAuth();
  const [searchParams] = useSearchParams();
  const token = searchParams.get("token");

  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [registering, setRegistering] = useState(false);

  // If token is in the URL, go straight to passkey registration
  useEffect(() => {
    if (token) {
      handleTokenFlow(token);
    }
  }, [token]);

  const handleRequestRecovery = async () => {
    setError("");
    if (!email.trim()) {
      setError("Vul je e-mailadres in.");
      return;
    }
    setLoading(true);
    try {
      await api.recoverRequest(email);
      track("Recovery Requested");
      setSent(true);
    } catch (err: any) {
      setError(err.message || "Verzoek mislukt");
    } finally {
      setLoading(false);
    }
  };

  const handleTokenFlow = async (recoveryToken: string) => {
    setRegistering(true);
    setError("");
    try {
      const { options, challengeId, userId } = await api.recoverBegin(recoveryToken);
      const opts = (options as any).publicKey ?? options;
      const credential = await startRegistration({ optionsJSON: opts as any });
      await api.recoverFinish(challengeId, userId, credential);
      track("Recovery Complete");
      await refreshMe();
    } catch (err: any) {
      const name = err?.name || "";
      const msg = (err?.message || "").toLowerCase();
      if (name === "NotAllowedError" || msg.includes("not allowed") || msg.includes("timed out")) {
        setError("Passkey-registratie geannuleerd of verlopen. Probeer het opnieuw.");
      } else if (name === "SecurityError") {
        setError("Beveiligingsfout: controleer of je de app opent via het juiste adres.");
      } else if (msg.includes("already registered") || name === "InvalidStateError") {
        setError("Deze passkey is al geregistreerd op dit apparaat.");
      } else {
        setError("Herstel mislukt. Vraag een nieuwe link aan.");
      }
      setRegistering(false);
    }
  };

  return (
    <Box
      component="main"
      sx={{
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        minHeight: "100vh",
        bgcolor: "#0f172a",
        px: 2,
      }}
    >
      <Paper
        elevation={0}
        sx={{
          p: { xs: 3, sm: 5 },
          maxWidth: 460,
          width: "100%",
          border: "1px solid",
          borderColor: "grey.200",
        }}
      >
        <Typography variant="h4" component="h1" align="center" sx={{ mb: 1 }}>
          Passkey herstellen
        </Typography>

        {registering ? (
          <Box sx={{ textAlign: "center", py: 4 }}>
            <CircularProgress sx={{ mb: 2 }} />
            <Typography>Je nieuwe passkey wordt aangemaakt...</Typography>
            {error && <Alert severity="error" sx={{ mt: 2 }}>{error}</Alert>}
          </Box>
        ) : sent ? (
          <Box sx={{ textAlign: "center", py: 2 }}>
            <Alert severity="success" sx={{ mb: 2 }}>
              Als er een account bestaat voor {email}, ontvang je een e-mail met een herstellink. Check ook je spammap.
            </Alert>
            <Typography variant="body2" color="text.secondary">
              De link is 15 minuten geldig.
            </Typography>
          </Box>
        ) : (
          <>
            <Typography variant="body2" color="text.secondary" align="center" sx={{ mb: 3 }}>
              Vul het e-mailadres van je account in. Je ontvangt een link om een nieuwe passkey te registreren.
            </Typography>

            {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

            <form onSubmit={(e) => { e.preventDefault(); handleRequestRecovery(); }}>
              <TextField
                fullWidth
                label="E-mailadres"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoFocus
                autoComplete="email"
                sx={{ mb: 2 }}
              />
              <Button
                fullWidth
                variant="contained"
                type="submit"
                disabled={loading}
                sx={{ py: 1.5 }}
              >
                {loading ? <CircularProgress size={24} color="inherit" /> : "Herstelmail versturen"}
              </Button>
            </form>
          </>
        )}

        <Box sx={{ mt: 3, textAlign: "center" }}>
          <Button href="/app/" variant="text" size="small">
            Terug naar inloggen
          </Button>
        </Box>
      </Paper>
    </Box>
  );
}
