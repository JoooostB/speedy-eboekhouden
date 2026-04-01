import { useState } from "react";
import {
  Box,
  Button,
  TextField,
  Typography,
  Alert,
  Paper,
  CircularProgress,
  Divider,
} from "@mui/material";
import LockOutlinedIcon from "@mui/icons-material/LockOutlined";
import { useAuth } from "../context/AuthContext";
import { track } from "../analytics";

export function LoginForm() {
  const { login } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await login(email, password);
      track("Login");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Login failed";
      track("Login Error", { reason: msg.includes("new_ip") ? "new_ip" : "credentials" });
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Box
      sx={{
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        minHeight: "100vh",
        bgcolor: "grey.100",
      }}
    >
      <Box sx={{ maxWidth: 440, width: "100%", px: 2 }}>
        <Paper sx={{ p: 4 }}>
          <Typography variant="h5" gutterBottom>
            Speedy e-Boekhouden
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
            Log in met je e-boekhouden.nl account
          </Typography>
          {error && error.includes("new_ip") ? (
            <Alert severity="warning" sx={{ mb: 2 }}>
              <strong>Nieuw IP-adres gedetecteerd</strong>
              <br />
              e-boekhouden.nl heeft een inlogpoging vanaf een onbekend IP-adres
              gedetecteerd. Je hebt een e-mail ontvangen van e-boekhouden om dit
              IP-adres goed te keuren. Open de link in de e-mail en probeer
              daarna opnieuw in te loggen.
            </Alert>
          ) : error ? (
            <Alert severity="error" sx={{ mb: 2 }}>
              {error}
            </Alert>
          ) : null}
          <form onSubmit={handleSubmit}>
            <TextField
              fullWidth
              label="E-mailadres"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              margin="normal"
              required
              autoFocus
            />
            <TextField
              fullWidth
              label="Wachtwoord"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              margin="normal"
              required
            />
            <Button
              fullWidth
              type="submit"
              variant="contained"
              disabled={loading}
              sx={{ mt: 2 }}
            >
              {loading ? <CircularProgress size={24} /> : "Inloggen"}
            </Button>
          </form>
        </Paper>

        <Paper sx={{ p: 3, mt: 2, bgcolor: "grey.50" }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1.5 }}>
            <LockOutlinedIcon fontSize="small" color="action" />
            <Typography variant="subtitle2" color="text.secondary">
              Beveiliging van je gegevens
            </Typography>
          </Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1, lineHeight: 1.7 }}>
            Je inloggegevens worden <strong>nooit opgeslagen</strong>. Ze worden direct
            doorgestuurd naar e-boekhouden.nl en daarna verwijderd. Alleen je
            sessie-token wordt tijdelijk in het geheugen bewaard (niet op schijf).
            Na 30 minuten inactiviteit wordt je sessie automatisch beëindigd.
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ lineHeight: 1.7 }}>
            Alle communicatie verloopt via een versleutelde HTTPS-verbinding.
            Twee-factor-authenticatie (MFA) wordt volledig ondersteund.
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1.5, lineHeight: 1.7, fontStyle: "italic" }}>
            Tip: maak in e-boekhouden een apart account aan dat alleen uren mag
            invoeren. Zo beperk je de toegang tot het absolute minimum.
          </Typography>
        </Paper>

        <Divider sx={{ my: 2 }} />

        <Typography variant="caption" color="text.disabled" sx={{ display: "block", textAlign: "center", lineHeight: 1.6 }}>
          Speedy e-Boekhouden is een onafhankelijk project en is op geen enkele
          wijze gelieerd aan, goedgekeurd door, of verbonden met e-Boekhouden.nl
          of e-Boekhouden B.V. Gebruik op eigen risico.
          Zie onze{" "}
          <a href="/disclaimer" style={{ color: "inherit", textDecoration: "underline" }}>
            volledige disclaimer
          </a>.
        </Typography>
      </Box>
    </Box>
  );
}
