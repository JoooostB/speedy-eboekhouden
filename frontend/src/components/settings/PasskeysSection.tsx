import { useEffect, useState, useCallback } from "react";
import {
  Box,
  Paper,
  Typography,
  List,
  ListItem,
  IconButton,
  TextField,
  Button,
  Alert,
  Tooltip,
  Skeleton,
} from "@mui/material";
import { api } from "../../api/client";

interface Passkey {
  id: string;
  friendlyName: string;
  createdAt: string;
  transport: string[];
}

/** Inline SVG: Pencil edit icon */
function EditIcon() {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 16, height: 16 }}
    >
      <path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" />
      <path d="m15 5 4 4" />
    </Box>
  );
}

/** Inline SVG: Trash icon */
function TrashIcon() {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 16, height: 16 }}
    >
      <path d="M3 6h18" />
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
      <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
    </Box>
  );
}

/** Inline SVG: Key icon */
function KeyIcon() {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 20, height: 20 }}
    >
      <path d="m21 2-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0 3 3L22 7l-3-3m-3.5 3.5L19 4" />
    </Box>
  );
}

function formatDate(iso: string): string {
  try {
    return new Intl.DateTimeFormat("nl-NL", { day: "numeric", month: "short", year: "numeric" }).format(new Date(iso));
  } catch {
    return iso;
  }
}

/**
 * PasskeysSection — manage the user's stored passkeys: list them, rename for
 * recognizability ("MacBook iCloud", "Bitwarden", etc.) and delete the ones
 * that no longer exist on the user's authenticators. Refuses to delete the
 * last remaining passkey on the backend to avoid lockout.
 */
export function PasskeysSection() {
  const [passkeys, setPasskeys] = useState<Passkey[] | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.listPasskeys();
      setPasskeys(res.passkeys);
    } catch (err: any) {
      setError(err?.message || "Passkeys laden mislukt");
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const startEdit = (pk: Passkey) => {
    setEditingId(pk.id);
    setDraft(pk.friendlyName || "");
    setError(null);
  };

  const cancelEdit = () => {
    setEditingId(null);
    setDraft("");
  };

  const saveEdit = async (id: string) => {
    const trimmed = draft.trim();
    if (!trimmed) {
      setError("Naam mag niet leeg zijn");
      return;
    }
    setBusy(true);
    try {
      await api.renamePasskey(id, trimmed);
      setPasskeys((prev) => prev?.map((p) => (p.id === id ? { ...p, friendlyName: trimmed } : p)) || null);
      cancelEdit();
    } catch (err: any) {
      setError(err?.message || "Passkey hernoemen mislukt");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    if (!window.confirm("Weet je zeker dat je deze passkey wilt verwijderen?")) return;
    setBusy(true);
    setError(null);
    try {
      await api.deletePasskey(id);
      setPasskeys((prev) => prev?.filter((p) => p.id !== id) || null);
    } catch (err: any) {
      setError(err?.message || "Passkey verwijderen mislukt");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: "flex", alignItems: "center", mb: 2 }}>
        <Box sx={{ mr: 1, color: "primary.main", display: "flex", flexShrink: 0 }}>
          <KeyIcon />
        </Box>
        <Typography variant="h6" component="h2">Passkeys</Typography>
      </Box>

      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Geef je passkeys herkenbare namen — bijvoorbeeld waar ze opgeslagen zijn (iCloud, Bitwarden, YubiKey).
      </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {passkeys === null ? (
        <Box>
          <Skeleton height={40} />
          <Skeleton height={40} />
        </Box>
      ) : passkeys.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          Geen passkeys gevonden.
        </Typography>
      ) : (
        <List dense disablePadding>
          {passkeys.map((pk) => {
            const isEditing = editingId === pk.id;
            return (
              <ListItem
                key={pk.id}
                disableGutters
                sx={{
                  borderBottom: "1px solid",
                  borderColor: "divider",
                  py: 1.5,
                  display: "flex",
                  alignItems: "center",
                  gap: 1,
                  flexWrap: "wrap",
                }}
              >
                {isEditing ? (
                  <>
                    <TextField
                      value={draft}
                      onChange={(e) => setDraft(e.target.value)}
                      size="small"
                      autoFocus
                      placeholder="bv. MacBook iCloud"
                      sx={{ flex: "1 1 200px" }}
                      inputProps={{ maxLength: 64, "aria-label": "Passkey naam" }}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") saveEdit(pk.id);
                        if (e.key === "Escape") cancelEdit();
                      }}
                    />
                    <Button size="small" variant="contained" onClick={() => saveEdit(pk.id)} disabled={busy}>
                      Opslaan
                    </Button>
                    <Button size="small" onClick={cancelEdit} disabled={busy}>
                      Annuleren
                    </Button>
                  </>
                ) : (
                  <>
                    <Box sx={{ flex: "1 1 200px", minWidth: 0 }}>
                      <Typography variant="body2" fontWeight={600} sx={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        {pk.friendlyName || "Zonder naam"}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        Aangemaakt op {formatDate(pk.createdAt)}
                      </Typography>
                    </Box>
                    <Tooltip title="Hernoemen">
                      <IconButton size="small" onClick={() => startEdit(pk)} aria-label={`Hernoem ${pk.friendlyName}`}>
                        <EditIcon />
                      </IconButton>
                    </Tooltip>
                    <Tooltip title={passkeys.length <= 1 ? "Je laatste passkey kan niet verwijderd worden" : "Verwijderen"}>
                      <span>
                        <IconButton
                          size="small"
                          onClick={() => remove(pk.id)}
                          disabled={busy || passkeys.length <= 1}
                          aria-label={`Verwijder ${pk.friendlyName}`}
                        >
                          <TrashIcon />
                        </IconButton>
                      </span>
                    </Tooltip>
                  </>
                )}
              </ListItem>
            );
          })}
        </List>
      )}
    </Paper>
  );
}
