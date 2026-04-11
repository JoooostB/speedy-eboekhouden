import { useState, useRef, useEffect } from "react";
import {
  Box,
  Button,
  TextField,
  Typography,
  Paper,
  Alert,
  Link,
  Fade,
  CircularProgress,
} from "@mui/material";
import { startAuthentication, startRegistration } from "@simplewebauthn/browser";
import { api } from "../../api/client";
import { useAuth } from "../../context/AuthContext";
import { track } from "../../analytics";

/**
 * PasskeyLogin handles both login and registration.
 *
 * Design rationale:
 * - Login is the primary action (one button, zero fields) to reduce friction for returning users.
 * - Registration is the secondary flow, revealed on demand via progressive disclosure.
 * - Passkey concept is explained in plain Dutch for non-technical bookkeepers.
 * - Navy background matches the landing page footer for visual continuity (Stopwatch system).
 * - Trust signals: shield icon, explicit "geen wachtwoord nodig" messaging.
 *
 * Accessibility:
 * - <main> landmark wraps content for screen readers.
 * - Heading hierarchy: h1 for brand, h2 for mode-specific title.
 * - Error alerts use role="alert" via MUI Alert (auto).
 * - Form fields have visible labels and are grouped logically.
 * - Focus is moved to first input when switching to register mode.
 */
export function PasskeyLogin() {
  const { refreshMe } = useAuth();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const emailRef = useRef<HTMLInputElement>(null);

  // Move focus to email field when switching to register
  useEffect(() => {
    if (mode === "register") {
      // Small delay to let the DOM update after the transition
      const t = setTimeout(() => emailRef.current?.focus(), 100);
      return () => clearTimeout(t);
    }
  }, [mode]);

  const handleLogin = async () => {
    setError("");
    setLoading(true);
    try {
      const { options, challengeId } = await api.loginBegin();
      const opts = (options as any).publicKey ?? options;
      const credential = await startAuthentication({ optionsJSON: opts as any });
      await api.loginFinish(challengeId, credential);
      track("Passkey Login");
      await refreshMe();
    } catch (err: any) {
      track("Passkey Login Error", { reason: err.message || "unknown" });
      setError(translateError(err, "login"));
    } finally {
      setLoading(false);
    }
  };

  const handleRegister = async () => {
    setError("");
    if (!email.trim()) {
      setError("Vul je e-mailadres in.");
      emailRef.current?.focus();
      return;
    }
    if (!name.trim()) {
      setError("Vul je naam in.");
      return;
    }
    setLoading(true);
    try {
      const { options, challengeId } = await api.registerBegin(email, name);
      const opts = (options as any).publicKey ?? options;
      const credential = await startRegistration({ optionsJSON: opts as any });
      await api.registerFinish(challengeId, credential);
      track("Passkey Register");
      await refreshMe();
    } catch (err: any) {
      track("Passkey Register Error", { reason: err.message || "unknown" });
      setError(translateError(err, "register"));
    } finally {
      setLoading(false);
    }
  };

  /** Translate WebAuthn and API errors to Dutch */
  const translateError = (err: any, context: "login" | "register"): string => {
    const name = err?.name || "";
    const msg = (err?.message || "").toLowerCase();

    // WebAuthn browser errors
    if (name === "NotAllowedError" || msg.includes("not allowed") || msg.includes("timed out")) {
      return context === "login"
        ? "Passkey-aanvraag geannuleerd of verlopen. Probeer het opnieuw."
        : "Passkey-registratie geannuleerd of verlopen. Probeer het opnieuw.";
    }
    if (name === "SecurityError" || msg.includes("security")) {
      return "Beveiligingsfout: controleer of je de app opent via het juiste adres (localhost of het eigen domein).";
    }
    if (name === "InvalidStateError" || msg.includes("already registered") || msg.includes("invalid state")) {
      return "Deze passkey is al geregistreerd op dit apparaat.";
    }
    if (name === "AbortError" || msg.includes("abort")) {
      return "Aanvraag afgebroken. Probeer het opnieuw.";
    }
    if (msg.includes("no credentials") || msg.includes("no passkey")) {
      return "Geen passkey gevonden. Heb je al een account aangemaakt?";
    }
    if (msg.includes("user handle") || msg.includes("user id") || msg.includes("do not match")) {
      return "Passkey komt niet overeen met je account. Verwijder de oude passkey uit je browser/apparaat en registreer opnieuw.";
    }
    if (msg.includes("credential not found") || msg.includes("finishing login") || msg.includes("not found")) {
      return "Deze passkey is niet meer geldig. Verwijder hem uit je browser (Instellingen → Wachtwoorden/Passkeys) en maak een nieuw account aan.";
    }

    // API errors (already in Dutch from our backend)
    if (msg.includes("e-mailadres") || msg.includes("geregistreerd") || msg.includes("challenge")) {
      return err.message;
    }

    // Fallback — generic Dutch message
    return context === "login"
      ? "Inloggen mislukt. Probeer het opnieuw."
      : "Registratie mislukt. Probeer het opnieuw.";
  };

  const switchMode = (newMode: "login" | "register") => {
    setMode(newMode);
    setError("");
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
      <Fade in timeout={400}>
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
          {/* Brand header */}
          <Box sx={{ textAlign: "center", mb: 4 }}>
            {/* Shield icon - trust signal for a financial tool */}
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
                width: 40,
                height: 40,
                color: "primary.main",
                mb: 2,
              }}
            >
              <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
              <path d="m9 12 2 2 4-4" />
            </Box>
            <Typography
              variant="h4"
              component="h1"
              sx={{ color: "primary.main", lineHeight: 1.2 }}
            >
              Speedy e-Boekhouden
            </Typography>
            <Typography
              variant="body2"
              sx={{ color: "text.secondary", mt: 1 }}
            >
              Sneller boekhouden, zonder gedoe
            </Typography>
          </Box>

          {mode === "login" ? (
            <>
              <Typography
                variant="h6"
                component="h2"
                sx={{ mb: 1, textAlign: "center" }}
              >
                Welkom terug
              </Typography>
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ mb: 3, textAlign: "center" }}
              >
                Log in met je passkey — geen wachtwoord nodig.
              </Typography>

              {error && (
                <Alert severity="error" sx={{ mb: 2 }}>
                  {error}
                </Alert>
              )}

              <Button
                fullWidth
                variant="contained"
                color="primary"
                size="large"
                onClick={handleLogin}
                disabled={loading}
                sx={{
                  py: 1.5,
                  fontSize: "1rem",
                }}
              >
                {loading ? (
                  <CircularProgress size={24} color="inherit" aria-label="Bezig met inloggen" />
                ) : (
                  "Inloggen"
                )}
              </Button>

              <Box sx={{ mt: 4, textAlign: "center" }}>
                <Typography variant="body2" color="text.secondary">
                  Nog geen account?{" "}
                  <Link
                    component="button"
                    variant="body2"
                    onClick={() => switchMode("register")}
                    sx={{ fontWeight: 600, color: "primary.main" }}
                  >
                    Maak er een aan
                  </Link>
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 1.5 }}>
                  <Link
                    href="/app/herstel"
                    variant="body2"
                    sx={{ color: "text.secondary" }}
                  >
                    Passkey kwijt?
                  </Link>
                </Typography>
              </Box>
            </>
          ) : (
            <>
              <Typography
                variant="h6"
                component="h2"
                sx={{ mb: 1, textAlign: "center" }}
              >
                Account aanmaken
              </Typography>
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ mb: 3, textAlign: "center" }}
              >
                Je apparaat wordt je sleutel. Geen wachtwoord om te onthouden.
              </Typography>

              {error && (
                <Alert severity="error" sx={{ mb: 2 }}>
                  {error}
                </Alert>
              )}

              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  handleRegister();
                }}
                noValidate
              >
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
                  label="Naam"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  autoComplete="name"
                  sx={{ mb: 3 }}
                />

                <Button
                  fullWidth
                  variant="contained"
                  color="primary"
                  size="large"
                  type="submit"
                  disabled={loading}
                  sx={{
                    py: 1.5,
                    fontSize: "1rem",
                  }}
                >
                  {loading ? (
                    <CircularProgress size={24} color="inherit" aria-label="Bezig met registreren" />
                  ) : (
                    "Registreren"
                  )}
                </Button>
              </form>

              {/* Passkey explainer - many bookkeepers won't know what a passkey is */}
              <Box
                sx={{
                  mt: 3,
                  p: 2,
                  bgcolor: "grey.50",
                  borderRadius: 2,
                }}
              >
                <Typography variant="body2" color="text.secondary" sx={{ lineHeight: 1.6 }}>
                  <strong>Wat is een passkey?</strong> Een passkey gebruikt de
                  beveiliging van je apparaat (vingerafdruk, gezichtsherkenning
                  of pincode) om je te identificeren. Veiliger dan een
                  wachtwoord en niets om te onthouden.
                </Typography>
              </Box>

              <Box sx={{ mt: 3, textAlign: "center" }}>
                <Typography variant="body2" color="text.secondary">
                  Al een account?{" "}
                  <Link
                    component="button"
                    variant="body2"
                    onClick={() => switchMode("login")}
                    sx={{ fontWeight: 600, color: "primary.main" }}
                  >
                    Log in
                  </Link>
                </Typography>
              </Box>
            </>
          )}
        </Paper>
      </Fade>
    </Box>
  );
}
