import { useEffect, useState, useCallback } from "react";
import {
  Box,
  Paper,
  Typography,
  Table,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
  IconButton,
  Button,
  Alert,
  Tooltip,
  Skeleton,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  TextField,
} from "@mui/material";
import { api } from "../../api/client";

interface Learned {
  signal: string;
  grootboekcode: string;
  btwCode: string;
  soort: string;
  count: number;
  sampleOmschrijving: string;
  createdAt: string;
  updatedAt: string;
  confirmedAt?: string | null;
}

const SOORT_LABELS: Record<string, string> = {
  FactuurOntvangen: "Factuur ontvangen",
  FactuurVerstuurd: "Factuur verstuurd",
  FactuurbetalingOntvangen: "Factuurbetaling ontvangen",
  FactuurbetalingVerstuurd: "Factuurbetaling verstuurd",
  GeldOntvangen: "Geld ontvangen",
  GeldUitgegeven: "Geld uitgegeven",
  Memoriaal: "Memoriaal",
};

/** Trash icon */
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

/** Brain icon */
function BrainIcon() {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 20, height: 20 }}
    >
      <path d="M12 5a3 3 0 1 0-5.997.142 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z" />
      <path d="M12 5a3 3 0 1 1 5.997.142 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z" />
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
 * LearnedSection — manage Speedy's "memory" of how recurring transactions
 * should be booked. Each row is a normalized signal (description + IBAN)
 * that has been confirmed at least once via a successful booking. Confirmed
 * mappings (count >= 2) are auto-applied on the next inbox classification
 * without calling Claude.
 */
export function LearnedSection() {
  const [items, setItems] = useState<Learned[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [wipeOpen, setWipeOpen] = useState(false);
  const [wipeConfirmText, setWipeConfirmText] = useState("");

  const load = useCallback(async () => {
    try {
      const res = await api.listLearned();
      setItems(res.learned || []);
    } catch (err: any) {
      setError(err?.message || "Geleerde boekingen laden mislukt");
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const remove = async (signal: string) => {
    if (!window.confirm("Deze geleerde boeking verwijderen? De AI zal de volgende keer opnieuw classificeren.")) return;
    setBusy(true);
    setError(null);
    try {
      await api.deleteLearned(signal);
      setItems((prev) => prev?.filter((p) => p.signal !== signal) || null);
    } catch (err: any) {
      setError(err?.message || "Verwijderen mislukt");
    } finally {
      setBusy(false);
    }
  };

  const clearAll = async () => {
    setBusy(true);
    setError(null);
    try {
      await api.deleteAllLearned();
      setItems([]);
      setWipeOpen(false);
      setWipeConfirmText("");
    } catch (err: any) {
      setError(err?.message || "Wissen mislukt");
    } finally {
      setBusy(false);
    }
  };

  const confirmedCount = items?.filter((i) => i.confirmedAt).length ?? 0;

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mb: 2, flexWrap: "wrap", gap: 1 }}>
        <Box sx={{ display: "flex", alignItems: "center" }}>
          <Box sx={{ mr: 1, color: "primary.main", display: "flex", flexShrink: 0 }}>
            <BrainIcon />
          </Box>
          <Typography variant="h6" component="h2">Geleerde boekingen</Typography>
        </Box>
        {items && items.length > 0 && (
          <Button
            size="small"
            color="error"
            variant="outlined"
            onClick={() => setWipeOpen(true)}
            disabled={busy}
          >
            Alles wissen
          </Button>
        )}
      </Box>

      {/* Wipe-all confirmation — typed confirmation prevents one-click loss
          of months of training data. window.confirm was the previous
          implementation but it's modal-but-untrustworthy and looks like a
          generic browser dialog. */}
      <Dialog
        open={wipeOpen}
        onClose={() => {
          if (!busy) {
            setWipeOpen(false);
            setWipeConfirmText("");
          }
        }}
        aria-labelledby="wipe-learned-title"
      >
        <DialogTitle id="wipe-learned-title" sx={{ fontWeight: 600 }}>
          Alle geleerde boekingen wissen?
        </DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ mb: 2 }}>
            Hiermee verwijder je {items?.length ?? 0} geleerde {items?.length === 1 ? "boeking" : "boekingen"}.
            Dit kan <strong>niet</strong> ongedaan gemaakt worden — Speedy moet daarna opnieuw leren hoe je
            terugkerende transacties boekt.
          </DialogContentText>
          <DialogContentText sx={{ mb: 1 }}>
            Typ <strong>WISSEN</strong> om te bevestigen:
          </DialogContentText>
          <TextField
            fullWidth
            size="small"
            value={wipeConfirmText}
            onChange={(e) => setWipeConfirmText(e.target.value)}
            placeholder="WISSEN"
            autoFocus
            disabled={busy}
            inputProps={{ "aria-label": "Bevestiging typen" }}
          />
        </DialogContent>
        <DialogActions>
          <Button
            onClick={() => {
              setWipeOpen(false);
              setWipeConfirmText("");
            }}
            disabled={busy}
          >
            Annuleren
          </Button>
          <Button
            onClick={clearAll}
            color="error"
            variant="contained"
            disabled={busy || wipeConfirmText !== "WISSEN"}
          >
            Definitief wissen
          </Button>
        </DialogActions>
      </Dialog>

      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Speedy onthoudt hoe je terugkerende transacties boekt. Na <strong>twee</strong> keer dezelfde
        keuze wordt de boeking automatisch toegepast op nieuwe afschriftregels — zonder de AI te
        raadplegen. Verwijder een regel hier als je hem wilt herzien.
      </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {items === null ? (
        <Box>
          <Skeleton height={40} />
          <Skeleton height={40} />
          <Skeleton height={40} />
        </Box>
      ) : items.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          Nog geen geleerde boekingen. Verwerk een paar afschriftregels om Speedy te trainen.
        </Typography>
      ) : (
        <>
          <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 1 }}>
            {items.length} {items.length === 1 ? "regel" : "regels"} • {confirmedCount} bevestigd (auto-toegepast)
          </Typography>
          <Box sx={{ overflow: "auto" }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Omschrijving</TableCell>
                  <TableCell>Tegenrekening</TableCell>
                  <TableCell>BTW</TableCell>
                  <TableCell>Soort</TableCell>
                  <TableCell align="right">Status</TableCell>
                  <TableCell align="right">Acties</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.signal}>
                    <TableCell sx={{ maxWidth: 240 }}>
                      <Typography
                        variant="body2"
                        sx={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 500 }}
                      >
                        {item.sampleOmschrijving || item.signal}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        Laatst bijgewerkt {formatDate(item.updatedAt)}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" sx={{ fontVariantNumeric: "tabular-nums" }}>
                        {item.grootboekcode}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Typography variant="caption">{item.btwCode || "GEEN"}</Typography>
                    </TableCell>
                    <TableCell>
                      <Typography variant="caption">{SOORT_LABELS[item.soort] || item.soort}</Typography>
                    </TableCell>
                    <TableCell align="right">
                      {item.confirmedAt ? (
                        <Chip
                          label={`Bevestigd (${item.count}×)`}
                          size="small"
                          sx={{
                            bgcolor: "rgba(22, 163, 74, 0.1)",
                            color: "#166534",
                            fontWeight: 600,
                            fontSize: "0.6875rem",
                            height: 22,
                          }}
                        />
                      ) : (
                        <Tooltip title="Wordt pas auto-toegepast na de tweede bevestiging">
                          <Chip
                            label="In afwachting"
                            size="small"
                            sx={{
                              bgcolor: "rgba(245, 158, 11, 0.1)",
                              color: "#92400e",
                              fontWeight: 600,
                              fontSize: "0.6875rem",
                              height: 22,
                            }}
                          />
                        </Tooltip>
                      )}
                    </TableCell>
                    <TableCell align="right">
                      <Tooltip title="Verwijderen">
                        <span>
                          <IconButton
                            size="small"
                            onClick={() => remove(item.signal)}
                            disabled={busy}
                            aria-label="Verwijderen"
                          >
                            <TrashIcon />
                          </IconButton>
                        </span>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
        </>
      )}
    </Paper>
  );
}
