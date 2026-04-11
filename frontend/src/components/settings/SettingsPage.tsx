import { useEffect, useState } from "react";
import {
  Box,
  Typography,
  Paper,
  TextField,
  Button,
  Alert,
  List,
  ListItem,
  ListItemText,
  Chip,
} from "@mui/material";
import Avatar from "@mui/material/Avatar";
import { api } from "../../api/client";
import { useAuth } from "../../context/AuthContext";
import { track } from "../../analytics";

/** Inline SVG icon: Key (Lucide-style) */
function KeyIcon(props: { sx?: object }) {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 20, height: 20, ...props.sx }}
    >
      <path d="m21 2-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0 3 3L22 7l-3-3m-3.5 3.5L19 4" />
    </Box>
  );
}

/** Inline SVG icon: Fingerprint / user identity */
function FingerprintIcon(props: { sx?: object }) {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 20, height: 20, ...props.sx }}
    >
      <path d="M2 12C2 6.5 6.5 2 12 2a10 10 0 0 1 8 4" />
      <path d="M5 19.5C5.5 18 6 15 6 12c0-.7.12-1.37.34-2" />
      <path d="M17.29 21.02c.12-.6.43-2.3.5-3.02" />
      <path d="M12 10a2 2 0 0 0-2 2c0 1.02-.1 2.51-.26 4" />
      <path d="M8.65 22c.21-.66.45-1.32.57-2" />
      <path d="M14 13.12c0 2.38 0 6.38-1 8.88" />
      <path d="M2 16h.01" />
      <path d="M21.8 16c.2-2 .131-5.354 0-6" />
      <path d="M9 6.8a6 6 0 0 1 9 5.2c0 .47 0 1.17-.02 2" />
    </Box>
  );
}

/** Inline SVG icon: Group / people */
function GroupIcon(props: { sx?: object }) {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 20, height: 20, ...props.sx }}
    >
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
      <path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </Box>
  );
}

/** Inline SVG icon: Server / API */
function ServerIcon(props: { sx?: object }) {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 20, height: 20, ...props.sx }}
    >
      <rect width="20" height="8" x="2" y="2" rx="2" ry="2" />
      <rect width="20" height="8" x="2" y="14" rx="2" ry="2" />
      <line x1="6" x2="6.01" y1="6" y2="6" />
      <line x1="6" x2="6.01" y1="18" y2="18" />
    </Box>
  );
}

/** Inline SVG icon: Globe / REST API */
function GlobeIcon(props: { sx?: object }) {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 20, height: 20, ...props.sx }}
    >
      <circle cx="12" cy="12" r="10" />
      <path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20" />
      <path d="M2 12h20" />
    </Box>
  );
}

export function SettingsPage() {
  const { user, team, avatarUrl, setAvatarUrl } = useAuth();
  const [hasApiKey, setHasApiKey] = useState(false);
  const [hasSoapCredentials, setHasSoapCredentials] = useState(false);
  const [hasRestAccessToken, setHasRestAccessToken] = useState(false);
  const [apiKeyInput, setApiKeyInput] = useState("");
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  // SOAP credential fields
  const [soapUsername, setSoapUsername] = useState("");
  const [soapCode1, setSoapCode1] = useState("");
  const [soapCode2, setSoapCode2] = useState("");
  const [soapSaving, setSoapSaving] = useState(false);
  const [soapMessage, setSoapMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  // REST token field
  const [restToken, setRestToken] = useState("");
  const [restSaving, setRestSaving] = useState(false);
  const [restMessage, setRestMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  useEffect(() => {
    api.getSettings().then((s) => {
      setHasApiKey(s.hasApiKey);
      setHasSoapCredentials(s.hasSoapCredentials);
      setHasRestAccessToken(s.hasRestAccessToken);
    }).catch(() => {});
  }, []);

  const handleSaveKey = async () => {
    setSaving(true);
    setMessage(null);
    try {
      await api.setApiKey(apiKeyInput);
      track("API Key Set");
      setHasApiKey(true);
      setApiKeyInput("");
      setMessage({ type: "success", text: "API-sleutel opgeslagen" });
    } catch (err: any) {
      setMessage({ type: "error", text: err.message });
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteKey = async () => {
    try {
      await api.deleteApiKey();
      track("API Key Deleted");
      setHasApiKey(false);
      setMessage({ type: "success", text: "API-sleutel verwijderd" });
    } catch (err: any) {
      setMessage({ type: "error", text: err.message });
    }
  };

  const handleSaveSoap = async () => {
    setSoapSaving(true);
    setSoapMessage(null);
    try {
      await api.setSoapCredentials(soapUsername, soapCode1, soapCode2);
      track("SOAP Credentials Set");
      setHasSoapCredentials(true);
      setSoapUsername("");
      setSoapCode1("");
      setSoapCode2("");
      setSoapMessage({ type: "success", text: "SOAP-gegevens opgeslagen" });
    } catch (err: any) {
      setSoapMessage({ type: "error", text: err.message });
    } finally {
      setSoapSaving(false);
    }
  };

  const handleDeleteSoap = async () => {
    try {
      await api.deleteSoapCredentials();
      track("SOAP Credentials Deleted");
      setHasSoapCredentials(false);
      setSoapMessage({ type: "success", text: "SOAP-gegevens verwijderd" });
    } catch (err: any) {
      setSoapMessage({ type: "error", text: err.message });
    }
  };

  const handleSaveRest = async () => {
    setRestSaving(true);
    setRestMessage(null);
    try {
      await api.setRestToken(restToken);
      track("REST Token Set");
      setHasRestAccessToken(true);
      setRestToken("");
      setRestMessage({ type: "success", text: "Access token opgeslagen" });
    } catch (err: any) {
      setRestMessage({ type: "error", text: err.message });
    } finally {
      setRestSaving(false);
    }
  };

  const handleDeleteRest = async () => {
    try {
      await api.deleteRestToken();
      track("REST Token Deleted");
      setHasRestAccessToken(false);
      setRestMessage({ type: "success", text: "Access token verwijderd" });
    } catch (err: any) {
      setRestMessage({ type: "error", text: err.message });
    }
  };

  return (
    <Box>
      <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
        Instellingen
      </Typography>

      {/* API Key */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", mb: 2 }}>
          <KeyIcon sx={{ mr: 1, color: "primary.main", flexShrink: 0 }} />
          <Typography variant="h6" component="h2">Anthropic API-sleutel</Typography>
          {hasApiKey && <Chip label="Ingesteld" color="success" size="small" sx={{ ml: 1 }} />}
        </Box>

        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Nodig voor AI-functies: factuurherkenning en boekingsuggesties.
          Je sleutel wordt versleuteld opgeslagen en nooit gedeeld.
          Nog geen sleutel?{" "}
          <Typography
            component="a"
            variant="body2"
            href="https://console.anthropic.com/settings/keys"
            target="_blank"
            rel="noopener noreferrer"
            sx={{ color: "primary.main", fontWeight: 600 }}
          >
            Maak er een aan bij Anthropic
          </Typography>.
        </Typography>

        {message && <Alert severity={message.type} sx={{ mb: 2 }}>{message.text}</Alert>}

        <Box sx={{ display: "flex", gap: 1 }}>
          <TextField
            fullWidth
            label="API-sleutel"
            placeholder="sk-ant-..."
            type="password"
            value={apiKeyInput}
            onChange={(e) => setApiKeyInput(e.target.value)}
            size="small"
          />
          <Button variant="contained" onClick={handleSaveKey} disabled={saving || !apiKeyInput}>
            Opslaan
          </Button>
          {hasApiKey && (
            <Button color="error" onClick={handleDeleteKey}>
              Verwijderen
            </Button>
          )}
        </Box>
      </Paper>

      {/* SOAP API Credentials */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", mb: 2 }}>
          <ServerIcon sx={{ mr: 1, color: "primary.main" }} />
          <Typography variant="h6" component="h2">SOAP API-gegevens</Typography>
          {hasSoapCredentials && <Chip label="Ingesteld" color="success" size="small" sx={{ ml: 1 }} />}
        </Box>

        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Nodig voor relaties, saldi, openstaande posten en mutaties.
          Je vindt deze gegevens in e-Boekhouden onder Beheer &gt; Instellingen &gt; API/SOAP.
        </Typography>

        {soapMessage && (
          <Alert severity={soapMessage.type} sx={{ mb: 2 }} role="status">
            {soapMessage.text}
          </Alert>
        )}

        <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <TextField
            fullWidth
            label="Gebruikersnaam"
            value={soapUsername}
            onChange={(e) => setSoapUsername(e.target.value)}
            size="small"
            autoComplete="username"
          />
          <TextField
            fullWidth
            label="SecurityCode1"
            type="password"
            value={soapCode1}
            onChange={(e) => setSoapCode1(e.target.value)}
            size="small"
            autoComplete="off"
          />
          <TextField
            fullWidth
            label="SecurityCode2"
            type="password"
            value={soapCode2}
            onChange={(e) => setSoapCode2(e.target.value)}
            size="small"
            autoComplete="off"
          />
          <Box sx={{ display: "flex", gap: 1 }}>
            <Button
              variant="contained"
              onClick={handleSaveSoap}
              disabled={soapSaving || !soapUsername || !soapCode1 || !soapCode2}
            >
              Opslaan
            </Button>
            {hasSoapCredentials && (
              <Button color="error" onClick={handleDeleteSoap}>
                Verwijderen
              </Button>
            )}
          </Box>
        </Box>
      </Paper>

      {/* REST API Token */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", mb: 2 }}>
          <GlobeIcon sx={{ mr: 1, color: "primary.main" }} />
          <Typography variant="h6" component="h2">REST API-token</Typography>
          {hasRestAccessToken && <Chip label="Ingesteld" color="success" size="small" sx={{ ml: 1 }} />}
        </Box>

        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Nodig voor verkoopfacturen en kostenplaatsen.
          Je vindt je access token in e-Boekhouden onder Beheer &gt; Instellingen &gt; API/SOAP.
        </Typography>

        {restMessage && (
          <Alert severity={restMessage.type} sx={{ mb: 2 }} role="status">
            {restMessage.text}
          </Alert>
        )}

        <Box sx={{ display: "flex", gap: 1 }}>
          <TextField
            fullWidth
            label="Access token"
            type="password"
            value={restToken}
            onChange={(e) => setRestToken(e.target.value)}
            size="small"
            autoComplete="off"
          />
          <Button
            variant="contained"
            onClick={handleSaveRest}
            disabled={restSaving || !restToken}
          >
            Opslaan
          </Button>
          {hasRestAccessToken && (
            <Button color="error" onClick={handleDeleteRest}>
              Verwijderen
            </Button>
          )}
        </Box>
      </Paper>

      {/* Account Info + Avatar */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", mb: 2 }}>
          <FingerprintIcon sx={{ mr: 1, color: "primary.main", flexShrink: 0 }} />
          <Typography variant="h6" component="h2">Account</Typography>
        </Box>

        <Box sx={{ display: "flex", alignItems: "center", gap: 3, mb: 2 }}>
          <Avatar
            src={avatarUrl || undefined}
            sx={{ width: 72, height: 72, fontSize: "1.5rem", bgcolor: "primary.main" }}
          >
            {(user?.name || "?").split(" ").map((w) => w[0]).join("").slice(0, 2).toUpperCase()}
          </Avatar>
          <Box>
            <Button variant="outlined" size="small" component="label" sx={{ mr: 1 }}>
              Foto uploaden
              <input
                type="file"
                accept="image/jpeg,image/png,image/webp"
                hidden
                onChange={async (e) => {
                  const file = e.target.files?.[0];
                  if (!file) return;
                  try {
                    const result = await api.uploadAvatar(file);
                    setAvatarUrl(result.avatarUrl);
                  } catch (err: any) {
                    setMessage({ type: "error", text: err.message });
                  }
                }}
              />
            </Button>
            {avatarUrl && (
              <Button
                size="small"
                color="error"
                onClick={async () => {
                  try {
                    await api.deleteAvatar();
                    setAvatarUrl("");
                  } catch (err: any) {
                    setMessage({ type: "error", text: err.message });
                  }
                }}
              >
                Verwijderen
              </Button>
            )}
            <Typography variant="caption" display="block" color="text.secondary" sx={{ mt: 0.5 }}>
              Max 2 MB. JPEG, PNG of WebP.
            </Typography>
          </Box>
        </Box>

        <List dense>
          <ListItem>
            <ListItemText primary="Naam" secondary={user?.name} />
          </ListItem>
          <ListItem>
            <ListItemText primary="E-mail" secondary={user?.email} />
          </ListItem>
          <ListItem>
            <ListItemText primary="Authenticatie" secondary="Passkey" />
          </ListItem>
        </List>
      </Paper>

      {/* Team Info */}
      {team && (
        <Paper sx={{ p: 3 }}>
          <Box sx={{ display: "flex", alignItems: "center", mb: 2 }}>
            <GroupIcon sx={{ mr: 1, color: "primary.main", flexShrink: 0 }} />
            <Typography variant="h6" component="h2">Team</Typography>
          </Box>

          <List dense>
            <ListItem>
              <ListItemText primary="Teamnaam" secondary={team.name} />
            </ListItem>
          </List>
        </Paper>
      )}
    </Box>
  );
}
