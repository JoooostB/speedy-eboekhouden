import { useState, useRef, useEffect } from "react";
import {
  Box,
  Paper,
  Typography,
  Stepper,
  Step,
  StepLabel,
  Button,
  TextField,
  Alert,
  Fade,
  LinearProgress,
} from "@mui/material";
import { api } from "../../api/client";
import { useAuth } from "../../context/AuthContext";
import { track } from "../../analytics";

/**
 * OnboardingWizard guides new users through connecting e-Boekhouden
 * and optionally setting an API key.
 *
 * Design rationale:
 * - Two content steps + a completion step. The stepper only shows the two
 *   actionable steps to avoid the misleading "Klaar" appearing as a task.
 * - e-Boekhouden connection is the core value proposition, so step 1 leads
 *   with "why" before asking for credentials.
 * - Trust messaging is prominent: credentials are forwarded, not stored.
 * - MFA flow transitions smoothly with clear instructions.
 * - Skip is always available but de-emphasized (text link, not a button
 *   competing with the primary action).
 * - API key step explains what it enables in concrete terms.
 *
 * Accessibility:
 * - Proper heading hierarchy (h1 > h2 per step).
 * - LinearProgress has aria attributes for step indication.
 * - Focus moves to first input on each step transition.
 * - Form submits on Enter.
 */

const stepLabels = ["e-Boekhouden koppelen", "AI instellen"];

export function OnboardingWizard({ onComplete }: { onComplete: () => void }) {
  const { setEBConnected } = useAuth();
  const [activeStep, setActiveStep] = useState(0);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  // Step 0: e-Boekhouden credentials
  const [ebEmail, setEbEmail] = useState("");
  const [ebPassword, setEbPassword] = useState("");
  const [mfaCode, setMfaCode] = useState("");
  const [needsMFA, setNeedsMFA] = useState(false);

  // Step 1: API key
  const [apiKey, setApiKey] = useState("");

  // Refs for focus management
  const ebEmailRef = useRef<HTMLInputElement>(null);
  const mfaRef = useRef<HTMLInputElement>(null);
  const apiKeyRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (activeStep === 0 && !needsMFA) {
      ebEmailRef.current?.focus();
    } else if (activeStep === 0 && needsMFA) {
      mfaRef.current?.focus();
    } else if (activeStep === 1) {
      apiKeyRef.current?.focus();
    }
  }, [activeStep, needsMFA]);

  const handleEBLogin = async () => {
    setError("");
    setLoading(true);
    try {
      if (needsMFA) {
        await api.ebMfa(mfaCode);
        setEBConnected(true);
        track("Onboarding EB Connect", { mfa: "true" });
        goToStep(1);
      } else {
        const res = await api.ebLogin(ebEmail, ebPassword);
        if (res.status === "mfa_required") {
          setNeedsMFA(true);
        } else {
          setEBConnected(true);
          track("Onboarding EB Connect");
          goToStep(1);
        }
      }
    } catch (err: any) {
      setError(err.message || "Verbinding mislukt. Controleer je gegevens.");
    } finally {
      setLoading(false);
    }
  };

  const handleSaveApiKey = async () => {
    if (!apiKey.trim()) {
      finish();
      return;
    }
    setError("");
    setLoading(true);
    try {
      await api.setApiKey(apiKey);
      track("Onboarding API Key Set");
      finish();
    } catch (err: any) {
      setError(err.message || "Opslaan mislukt. Probeer het opnieuw.");
    } finally {
      setLoading(false);
    }
  };

  const goToStep = (step: number) => {
    setError("");
    setActiveStep(step);
  };

  const finish = () => {
    track("Onboarding Complete");
    onComplete();
  };

  // Progress: 0 = 0%, 1 = 50%, done = 100%
  const progress = activeStep >= 2 ? 100 : activeStep * 50;

  return (
    <Box
      component="main"
      sx={{
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        minHeight: "100vh",
        bgcolor: "grey.50",
        px: 2,
        py: 4,
      }}
    >
      <Box sx={{ maxWidth: 540, width: "100%" }}>
        {/* Progress bar across the top of the card */}
        <LinearProgress
          variant="determinate"
          value={progress}
          aria-label={`Stap ${activeStep + 1} van ${stepLabels.length}`}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={progress}
          sx={{
            height: 4,
            borderRadius: "12px 12px 0 0",
            bgcolor: "grey.200",
            "& .MuiLinearProgress-bar": {
              bgcolor: "primary.main",
              transition: "transform 0.4s ease",
            },
          }}
        />

        <Paper
          elevation={0}
          sx={{
            p: { xs: 3, sm: 5 },
            border: "1px solid",
            borderColor: "grey.200",
            borderTop: "none",
            borderRadius: "0 0 12px 12px",
          }}
        >
          {/* Header - consistent across steps */}
          <Typography
            variant="h4"
            component="h1"
            sx={{ textAlign: "center", mb: 0.5 }}
          >
            Welkom bij Speedy
          </Typography>

          <Stepper
            activeStep={activeStep}
            alternativeLabel
            sx={{ mb: 4, mt: 3 }}
          >
            {stepLabels.map((label) => (
              <Step key={label}>
                <StepLabel>{label}</StepLabel>
              </Step>
            ))}
          </Stepper>

          {/* Step 0: Connect e-Boekhouden */}
          {activeStep === 0 && (
            <Fade in timeout={300}>
              <Box>
                <Typography variant="h6" component="h2" gutterBottom>
                  Koppel je e-Boekhouden account
                </Typography>
                <Typography
                  variant="body2"
                  color="text.secondary"
                  sx={{ mb: 3, lineHeight: 1.7 }}
                >
                  Speedy logt namens jou in bij e-boekhouden.nl om
                  medewerkers, projecten en activiteiten op te halen. Zo kun
                  je in een keer uren boeken voor je hele team.
                </Typography>

                {error && (
                  <Alert severity="error" sx={{ mb: 2 }}>
                    {error}
                  </Alert>
                )}

                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    handleEBLogin();
                  }}
                  noValidate
                >
                  {!needsMFA ? (
                    <>
                      <TextField
                        fullWidth
                        label="E-mailadres e-Boekhouden"
                        type="email"
                        value={ebEmail}
                        onChange={(e) => setEbEmail(e.target.value)}
                        inputRef={ebEmailRef}
                        autoComplete="email"
                        sx={{ mb: 2 }}
                      />
                      <TextField
                        fullWidth
                        label="Wachtwoord"
                        type="password"
                        value={ebPassword}
                        onChange={(e) => setEbPassword(e.target.value)}
                        autoComplete="current-password"
                        sx={{ mb: 2 }}
                      />
                    </>
                  ) : (
                    <>
                      <Alert severity="info" sx={{ mb: 2 }} icon={false}>
                        <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
                          Verificatiecode nodig
                        </Typography>
                        <Typography variant="body2">
                          e-Boekhouden heeft een verificatiecode naar je e-mail
                          gestuurd. Vul deze hieronder in.
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
                        sx={{ mb: 2 }}
                      />
                    </>
                  )}

                  {/* Trust signal */}
                  <Box
                    sx={{
                      display: "flex",
                      alignItems: "flex-start",
                      gap: 1,
                      mb: 3,
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
                        width: 20,
                        height: 20,
                        color: "text.secondary",
                        flexShrink: 0,
                        mt: 0.25,
                      }}
                    >
                      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                    </Box>
                    <Typography variant="caption" color="text.secondary" sx={{ lineHeight: 1.5 }}>
                      Je wachtwoord wordt doorgestuurd naar e-boekhouden.nl
                      en direct daarna verwijderd. Speedy slaat geen
                      inloggegevens op.
                    </Typography>
                  </Box>

                  <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <Button
                      onClick={() => goToStep(1)}
                      size="small"
                      sx={{ color: "text.secondary" }}
                    >
                      Later koppelen
                    </Button>
                    <Button
                      variant="contained"
                      color="primary"
                      type="submit"
                      disabled={loading}
                      sx={{ minWidth: 120 }}
                    >
                      {loading ? "Bezig..." : needsMFA ? "Verifieer" : "Verbinden"}
                    </Button>
                  </Box>
                </form>
              </Box>
            </Fade>
          )}

          {/* Step 1: API key (optional) */}
          {activeStep === 1 && (
            <Fade in timeout={300}>
              <Box>
                <Typography variant="h6" component="h2" gutterBottom>
                  AI-functies activeren
                </Typography>
                <Typography
                  variant="body2"
                  color="text.secondary"
                  sx={{ mb: 1, lineHeight: 1.7 }}
                >
                  Met een Anthropic API-sleutel kan Speedy bankafschriften
                  automatisch categoriseren en inkoopfacturen herkennen.
                </Typography>
                <Typography
                  variant="body2"
                  color="text.secondary"
                  sx={{ mb: 3, lineHeight: 1.7 }}
                >
                  Dit is optioneel. Urenregistratie en afschriftverwerking
                  werken ook zonder.
                </Typography>

                {error && (
                  <Alert severity="error" sx={{ mb: 2 }}>
                    {error}
                  </Alert>
                )}

                <TextField
                  fullWidth
                  label="API-sleutel"
                  placeholder="sk-ant-..."
                  type="password"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  inputRef={apiKeyRef}
                  sx={{ mb: 3 }}
                  helperText="Je vindt je sleutel op console.anthropic.com."
                />

                <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                  <Button
                    onClick={finish}
                    size="small"
                    sx={{ color: "text.secondary" }}
                  >
                    Overslaan
                  </Button>
                  <Button
                    variant="contained"
                    color="primary"
                    onClick={handleSaveApiKey}
                    disabled={loading}
                    sx={{ minWidth: 120 }}
                  >
                    {loading ? "Bezig..." : apiKey.trim() ? "Opslaan en doorgaan" : "Doorgaan"}
                  </Button>
                </Box>
              </Box>
            </Fade>
          )}

          {/* Step 2: Done - this shouldn't normally show because finish()
              navigates away, but it's here as a safety net */}
          {activeStep === 2 && (
            <Fade in timeout={300}>
              <Box sx={{ textAlign: "center", py: 2 }}>
                <Typography variant="h6" component="h2" gutterBottom>
                  Alles klaar
                </Typography>
                <Typography color="text.secondary" sx={{ mb: 3 }}>
                  Je kunt aan de slag.
                </Typography>
                <Button
                  variant="contained"
                  color="primary"
                  size="large"
                  onClick={finish}
                >
                  Naar het dashboard
                </Button>
              </Box>
            </Fade>
          )}
        </Paper>
      </Box>
    </Box>
  );
}
