import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Box,
  Card,
  CardContent,
  CardActionArea,
  Typography,
  Grid,
  Alert,
  AlertTitle,
  Skeleton,
  Button,
  Paper,
  Stepper,
  Step,
  StepLabel,
  StepContent,
  Chip,
  CircularProgress,
} from "@mui/material";
import { api } from "../api/client";
import { track } from "../analytics";
import { useAuth } from "../context/AuthContext";
import type { InboxSummary, InboxCategory } from "../api/types";

function getGreeting(): string {
  const hour = new Date().getHours();
  if (hour < 12) return "Goedemorgen";
  if (hour < 18) return "Goedemiddag";
  return "Goedenavond";
}

function getFirstName(name: string | undefined): string {
  if (!name) return "";
  return name.split(" ")[0];
}

const CATEGORY_COLORS: Record<InboxCategory, { color: string; bgcolor: string }> = {
  auto: { color: "#166534", bgcolor: "rgba(22, 163, 74, 0.1)" },
  review: { color: "#92400e", bgcolor: "rgba(245, 158, 11, 0.1)" },
  invoice: { color: "#1e40af", bgcolor: "rgba(59, 130, 246, 0.1)" },
  manual: { color: "#991b1b", bgcolor: "rgba(220, 38, 38, 0.1)" },
};

const CATEGORY_LABELS: Record<InboxCategory, string> = {
  auto: "auto",
  review: "controleer",
  invoice: "factuur",
  manual: "handmatig",
};

/**
 * Dashboard — task-oriented home for a ZZP'er / small business user.
 *
 * Shows:
 * - Greeting with first name + time of day
 * - Primary card: "Boekhoudtaken" with inbox summary counts
 * - Secondary cards: Vervallen facturen, Uren
 * - Setup guide for new users (not connected / no API key)
 *
 * Accessibility:
 * - h1 for greeting, h2 for card titles — logical heading hierarchy
 * - Cards use CardActionArea for keyboard navigation
 * - Summary badges pair color with text labels (not color alone)
 * - Loading skeletons use aria-busy
 * - Setup stepper labels and content are properly associated
 */
export function Dashboard() {
  const { user, eboekhoudenConnected } = useAuth();
  const navigate = useNavigate();
  const [inboxSummary, setInboxSummary] = useState<InboxSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [autoProcessing, setAutoProcessing] = useState(false);

  useEffect(() => {
    const load = async () => {
      try {
        if (eboekhoudenConnected) {
          const summary = await api.getInboxSummary();
          setInboxSummary(summary);
        }
      } finally {
        setLoading(false);
      }
    };
    load();
  }, [eboekhoudenConnected]);

  const firstName = getFirstName(user?.name);
  const isNewUser = !eboekhoudenConnected && !inboxSummary?.hasApiKey;
  const setupStep = eboekhoudenConnected ? (inboxSummary?.hasApiKey ? 2 : 1) : 0;

  const totalInbox = inboxSummary
    ? (inboxSummary.classificationSummary.auto || 0) +
      (inboxSummary.classificationSummary.review || 0) +
      (inboxSummary.classificationSummary.invoice || 0) +
      (inboxSummary.classificationSummary.manual || 0)
    : 0;

  // Process all "auto" items directly from dashboard
  const handleAutoProcess = async () => {
    if (!inboxSummary || !inboxSummary.classificationSummary.auto) return;

    setAutoProcessing(true);
    try {
      // First classify to get the actual items
      const res = await api.classifyInbox();
      const autoItems = res.classifications.filter((c) => c.category === "auto");

      if (autoItems.length > 0) {
        const items = autoItems.map((item) => ({
          id: item.id,
          soort: item.soort,
          grootboekcode: item.grootboekcode,
          btwCode: item.btwCode,
          omschrijving: item.aiOmschrijving || item.omschrijving,
          bedrag: Math.abs(item.bedrag),
        }));

        await api.processInboxBatch(items);
        track("Inbox Auto Process", { count: String(autoItems.length) });

        // Reload summary
        const newSummary = await api.getInboxSummary();
        setInboxSummary(newSummary);
      }
    } catch {
      // Error handling — user can retry or go to inbox
    } finally {
      setAutoProcessing(false);
    }
  };

  return (
    <Box>
      {/* Greeting */}
      <Typography variant="h4" component="h1" sx={{ mb: 0.5 }}>
        {getGreeting()}{firstName ? `, ${firstName}` : ""}
      </Typography>
      <Typography variant="body1" color="text.secondary" sx={{ mb: 4 }}>
        {eboekhoudenConnected
          ? "Hier is je overzicht voor vandaag."
          : "Welkom bij Speedy e-Boekhouden. Laten we aan de slag gaan."}
      </Typography>

      {/* First-time setup stepper — only for brand new users */}
      {isNewUser && (
        <Paper variant="outlined" sx={{ p: 3, mb: 4 }}>
          <Typography variant="h6" component="h2" gutterBottom>
            Aan de slag
          </Typography>

          <Stepper activeStep={setupStep} orientation="vertical" sx={{ mt: 2 }}>
            <Step completed={eboekhoudenConnected}>
              <StepLabel>
                <Typography fontWeight={600}>Verbind met e-Boekhouden</Typography>
              </StepLabel>
              <StepContent>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                  Log in met je e-boekhouden.nl account om medewerkers, projecten
                  en afschriften op te halen. Deze verbinding is per sessie —
                  je wachtwoord wordt nooit opgeslagen.
                </Typography>
                <Button
                  variant="contained"
                  size="small"
                  onClick={() => window.dispatchEvent(new CustomEvent("eb:connect"))}
                >
                  Verbinden
                </Button>
              </StepContent>
            </Step>

            <Step completed={!!inboxSummary?.hasApiKey}>
              <StepLabel
                optional={<Typography variant="caption" color="text.secondary">Optioneel</Typography>}
              >
                <Typography fontWeight={600}>Stel een AI-sleutel in</Typography>
              </StepLabel>
              <StepContent>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                  Met een Anthropic API-sleutel herkent Speedy automatisch
                  facturen en stelt boekingsuggesties voor bij bankafschriften.
                  Zonder sleutel werkt alles — alleen zonder AI-hulp.
                </Typography>
                <Button
                  variant="outlined"
                  size="small"
                  onClick={() => navigate("/instellingen")}
                >
                  Instellen
                </Button>
              </StepContent>
            </Step>
          </Stepper>
        </Paper>
      )}

      {/* Returning user with expired e-boekhouden session — reconnect banner */}
      {!eboekhoudenConnected && !isNewUser && (
        <Alert
          severity="info"
          sx={{
            mb: 4,
            alignItems: "flex-start",
            "& .MuiAlert-message": { width: "100%" },
          }}
          action={
            <Button
              color="inherit"
              size="small"
              onClick={() => window.dispatchEvent(new CustomEvent("eb:connect"))}
              sx={{ fontWeight: 600, whiteSpace: "nowrap" }}
            >
              Verbinden
            </Button>
          }
        >
          <AlertTitle sx={{ fontWeight: 600 }}>
            e-Boekhouden is niet verbonden
          </AlertTitle>
          Log in met je e-boekhouden.nl account om verder te gaan.
          Je wachtwoord wordt niet opgeslagen.
        </Alert>
      )}

      {/* Task cards */}
      <Grid container spacing={3} role="list" aria-label="Taken">
        {/* Primary: Boekhoudtaken (Inbox summary) */}
        <Grid size={{ xs: 12, md: 8 }} role="listitem">
          <Card
            sx={{
              height: "100%",
              opacity: eboekhoudenConnected ? 1 : 0.5,
              transition: "opacity 0.2s",
            }}
          >
            <CardContent sx={{ p: 3 }}>
              <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mb: 2 }}>
                <Typography variant="h6" component="h2">
                  Boekhoudtaken
                </Typography>
                {loading ? (
                  <Skeleton width={80} height={32} aria-busy="true" />
                ) : totalInbox > 0 ? (
                  <Chip
                    label={`${totalInbox} open`}
                    size="small"
                    color="primary"
                    sx={{ fontWeight: 600 }}
                  />
                ) : (
                  <Chip
                    label="Alles verwerkt"
                    size="small"
                    sx={{
                      fontWeight: 600,
                      bgcolor: "rgba(22, 163, 74, 0.1)",
                      color: "#166534",
                    }}
                  />
                )}
              </Box>

              {/* Category breakdown */}
              {loading ? (
                <Box sx={{ display: "flex", gap: 1, mb: 2 }}>
                  {[1, 2, 3, 4].map((i) => (
                    <Skeleton key={i} variant="rounded" width={80} height={28} sx={{ borderRadius: 3 }} />
                  ))}
                </Box>
              ) : inboxSummary && totalInbox > 0 ? (
                <Box
                  sx={{ display: "flex", gap: 1, flexWrap: "wrap", mb: 2.5 }}
                  role="status"
                  aria-label="Inboxverdeling"
                >
                  {(Object.keys(CATEGORY_COLORS) as InboxCategory[]).map((cat) => {
                    const count = inboxSummary.classificationSummary[cat] || 0;
                    if (count === 0) return null;
                    return (
                      <Chip
                        key={cat}
                        label={`${count} ${CATEGORY_LABELS[cat]}`}
                        size="small"
                        sx={{
                          fontWeight: 600,
                          fontSize: "0.75rem",
                          height: 28,
                          color: CATEGORY_COLORS[cat].color,
                          bgcolor: CATEGORY_COLORS[cat].bgcolor,
                        }}
                      />
                    );
                  })}
                </Box>
              ) : !loading ? (
                <Typography variant="body2" color="text.secondary" sx={{ mb: 2.5 }}>
                  Geen openstaande boekhoudtaken.
                </Typography>
              ) : null}

              {/* Action buttons */}
              <Box sx={{ display: "flex", gap: 1.5, flexWrap: "wrap" }}>
                <Button
                  variant="contained"
                  size="small"
                  onClick={() => navigate("/")}
                  disabled={!eboekhoudenConnected}
                  sx={{ fontWeight: 600 }}
                >
                  Alles bekijken
                </Button>

                {inboxSummary && (inboxSummary.classificationSummary.auto || 0) > 0 && (
                  <Button
                    variant="outlined"
                    size="small"
                    onClick={handleAutoProcess}
                    disabled={autoProcessing || !eboekhoudenConnected}
                    startIcon={
                      autoProcessing ? (
                        <CircularProgress size={16} color="inherit" aria-hidden="true" />
                      ) : null
                    }
                    sx={{ fontWeight: 600 }}
                  >
                    {autoProcessing
                      ? "Verwerken..."
                      : `${inboxSummary.classificationSummary.auto} auto verwerken`}
                  </Button>
                )}
              </Box>
            </CardContent>
          </Card>
        </Grid>

        {/* Vervallen facturen */}
        <Grid size={{ xs: 12, sm: 6, md: 4 }} role="listitem">
          <Card
            sx={{
              height: "100%",
              opacity: eboekhoudenConnected ? 1 : 0.5,
              transition: "opacity 0.2s",
            }}
          >
            <CardActionArea
              onClick={() => navigate("/openposten")}
              disabled={!eboekhoudenConnected}
              aria-disabled={!eboekhoudenConnected}
              sx={{ height: "100%" }}
            >
              <CardContent sx={{ p: 3 }}>
                {/* Warning triangle icon */}
                <Box
                  component="svg"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={1.5}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                  sx={{ width: 32, height: 32, color: "warning.main", mb: 1.5 }}
                >
                  <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
                  <line x1="12" y1="9" x2="12" y2="13" />
                  <line x1="12" y1="17" x2="12.01" y2="17" />
                </Box>
                <Typography variant="subtitle1" component="h2" fontWeight={600} gutterBottom>
                  Vervallen facturen
                </Typography>
                {loading ? (
                  <Skeleton width="60%" aria-busy="true" />
                ) : inboxSummary && inboxSummary.overdueCount > 0 ? (
                  <>
                    <Typography variant="h5" component="p" fontWeight={700} color="warning.dark">
                      {inboxSummary.overdueCount}
                    </Typography>
                    <Typography variant="body2" color="text.secondary">
                      Totaal: {new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(inboxSummary.overdueTotal)}
                    </Typography>
                  </>
                ) : (
                  <Typography variant="body2" color="text.secondary">
                    Geen vervallen facturen
                  </Typography>
                )}
              </CardContent>
            </CardActionArea>
          </Card>
        </Grid>

        {/* Uren */}
        <Grid size={{ xs: 12, sm: 6, md: 4 }} role="listitem">
          <Card
            sx={{
              height: "100%",
              opacity: eboekhoudenConnected ? 1 : 0.5,
              transition: "opacity 0.2s",
            }}
          >
            <CardActionArea
              onClick={() => navigate("/uren")}
              disabled={!eboekhoudenConnected}
              aria-disabled={!eboekhoudenConnected}
              sx={{ height: "100%" }}
            >
              <CardContent sx={{ p: 3 }}>
                {/* Clock icon */}
                <Box
                  component="svg"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={1.5}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                  sx={{ width: 32, height: 32, color: "primary.main", mb: 1.5 }}
                >
                  <circle cx="12" cy="12" r="10" />
                  <polyline points="12 6 12 12 16 14" />
                </Box>
                <Typography variant="subtitle1" component="h2" fontWeight={600} gutterBottom>
                  Urenregistratie
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  Boek uren voor meerdere medewerkers en dagen tegelijk.
                </Typography>
              </CardContent>
            </CardActionArea>
          </Card>
        </Grid>
      </Grid>

      {/* AI nudge — connected but no API key */}
      {eboekhoudenConnected && inboxSummary && !inboxSummary.hasApiKey && (
        <Box
          sx={{
            mt: 4,
            p: 2.5,
            bgcolor: "background.paper",
            border: "1px solid",
            borderColor: "grey.200",
            borderRadius: 2,
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            flexWrap: "wrap",
            gap: 2,
          }}
        >
          <Box>
            <Typography variant="body2" fontWeight={600}>
              AI-functies zijn nog niet actief
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Stel een Anthropic API-sleutel in om facturen te herkennen
              en boekingsuggesties te krijgen.
            </Typography>
          </Box>
          <Button
            variant="outlined"
            size="small"
            onClick={() => navigate("/instellingen")}
            sx={{ flexShrink: 0 }}
          >
            Instellen
          </Button>
        </Box>
      )}
    </Box>
  );
}
