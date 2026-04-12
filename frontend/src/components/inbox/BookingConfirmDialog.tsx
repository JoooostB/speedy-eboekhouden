import { useState, useEffect, useMemo, useRef } from "react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Typography,
  Alert,
  CircularProgress,
  Divider,
  TextField,
  FormHelperText,
} from "@mui/material";
import type { InboxClassification, LedgerAccount, OpenPost, VATCode } from "../../api/types";
import { api } from "../../api/client";
import { LedgerAccountPicker } from "../shared/LedgerAccountPicker";
import { VATCodePicker } from "../shared/VATCodePicker";

interface Props {
  open: boolean;
  onClose: () => void;
  items: InboxClassification[];
  ledgerAccounts: LedgerAccount[];
  vatCodes: VATCode[];
  /** Called with the edited item set when the user confirms. */
  onConfirm: (editedItems: InboxClassification[]) => Promise<void> | void;
  processing: boolean;
  /** When set, shown as an error alert at the top of the dialog so the
   *  user sees the failure without having to close the dialog first. */
  error?: string | null;
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

function fmtCurrency(n: number) {
  return new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(n);
}

function fmtDate(s: string) {
  if (!s) return "";
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return new Intl.DateTimeFormat("nl-NL", { day: "numeric", month: "short", year: "numeric" }).format(d);
}

/**
 * BookingConfirmDialog — quick confirmation modal for processing inbox items
 * without an invoice attached. Used by both the batch approve bar and the
 * single-item "Verwerken" button on InboxRow.
 *
 * Each item shows:
 * - Read-only metadata: datum, bedrag, soort
 * - Editable: tegenrekening (LedgerAccountPicker showing code + name),
 *   BTW (VATCodePicker), omschrijving (TextField)
 *
 * Local edits are kept in dialog state and passed back via onConfirm so the
 * caller can submit the user's final values rather than the original AI
 * suggestion. For positive amounts (potential refunds), it cross-references
 * open crediteuren posts and flags any matches inline.
 */
export function BookingConfirmDialog({
  open,
  onClose,
  items,
  ledgerAccounts,
  vatCodes,
  onConfirm,
  processing,
  error,
}: Props) {
  const [refundMatches, setRefundMatches] = useState<Map<number, OpenPost>>(new Map());
  const [matchingRefunds, setMatchingRefunds] = useState(false);
  const errorRef = useRef<HTMLDivElement | null>(null);

  // When an error appears, scroll it into view and move focus to the alert
  // wrapper so screen reader users hear the new status. Sighted users with a
  // long batch list might otherwise miss the alert above the fold.
  useEffect(() => {
    if (!error || !errorRef.current) return;
    errorRef.current.scrollIntoView({ block: "start", behavior: "smooth" });
    errorRef.current.focus();
  }, [error]);
  // Editable per-item state, keyed by inbox item id.
  const [edits, setEdits] = useState<
    Map<number, { ledger: LedgerAccount | null; btwCode: string; omschrijving: string }>
  >(new Map());

  const ledgerLookup = useMemo(() => {
    const map = new Map<string, LedgerAccount>();
    ledgerAccounts.forEach((a) => map.set(a.code, a));
    return map;
  }, [ledgerAccounts]);

  // Reset local edits whenever the dialog opens with a (possibly different)
  // set of items. Pre-fill from the items' current values.
  useEffect(() => {
    if (!open) return;
    const next = new Map<number, { ledger: LedgerAccount | null; btwCode: string; omschrijving: string }>();
    items.forEach((item) => {
      next.set(item.id, {
        ledger: ledgerLookup.get(item.grootboekcode) || null,
        btwCode: item.btwCode || "GEEN",
        omschrijving: item.aiOmschrijving || item.omschrijving,
      });
    });
    setEdits(next);
  }, [open, items, ledgerLookup]);

  // Positive amounts on a non-invoice booking type may actually be refunds
  // of an earlier supplier invoice. Try to match them against open crediteuren.
  const potentialRefundIds = useMemo(
    () =>
      items
        .filter((i) => i.bedrag > 0 && i.soort !== "FactuurbetalingOntvangen" && i.soort !== "FactuurOntvangen")
        .map((i) => i.id),
    [items],
  );

  useEffect(() => {
    if (!open || potentialRefundIds.length === 0) {
      setRefundMatches(new Map());
      return;
    }
    setMatchingRefunds(true);
    api
      .getOpenPosten("Crediteuren")
      .then((posts) => {
        const matches = new Map<number, OpenPost>();
        items.forEach((item) => {
          if (!potentialRefundIds.includes(item.id)) return;
          const m = posts.find(
            (p) =>
              Math.abs(p.openstaand - item.bedrag) < 0.01 ||
              Math.abs(p.bedrag - item.bedrag) < 0.01,
          );
          if (m) matches.set(item.id, m);
        });
        setRefundMatches(matches);
      })
      .catch(() => {
        /* non-fatal — refund hint is best-effort */
      })
      .finally(() => setMatchingRefunds(false));
  }, [open, items, potentialRefundIds]);

  const isSingle = items.length === 1;
  const incomplete = items.some((i) => {
    const e = edits.get(i.id);
    return !e || !e.ledger;
  });

  const updateEdit = (id: number, patch: Partial<{ ledger: LedgerAccount | null; btwCode: string; omschrijving: string }>) => {
    setEdits((prev) => {
      const next = new Map(prev);
      const existing = next.get(id) || { ledger: null, btwCode: "GEEN", omschrijving: "" };
      next.set(id, { ...existing, ...patch });
      return next;
    });
  };

  const handleConfirm = () => {
    // Merge edits back into the items so the caller submits the user's final values.
    const editedItems: InboxClassification[] = items.map((item) => {
      const e = edits.get(item.id);
      if (!e) return item;
      return {
        ...item,
        grootboekcode: e.ledger?.code || item.grootboekcode,
        btwCode: e.btwCode,
        aiOmschrijving: e.omschrijving,
        omschrijving: e.omschrijving,
      };
    });
    onConfirm(editedItems);
  };

  return (
    <Dialog
      open={open}
      onClose={processing ? undefined : onClose}
      maxWidth="md"
      fullWidth
      aria-labelledby="booking-confirm-title"
    >
      <DialogTitle id="booking-confirm-title" sx={{ fontWeight: 600 }}>
        {isSingle ? "Boeking bevestigen" : `${items.length} boekingen bevestigen`}
      </DialogTitle>
      <DialogContent dividers>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Controleer hoe {isSingle ? "deze regel wordt" : "deze regels worden"} geboekt. Pas indien nodig
          de tegenrekening, BTW of omschrijving aan en klik op Bevestigen.
        </Typography>

        {error && (
          <Box ref={errorRef} tabIndex={-1} sx={{ outline: "none", mb: 2 }}>
            <Alert severity="error" role="alert">
              {error}
            </Alert>
          </Box>
        )}

        {refundMatches.size > 0 && (
          <Alert severity="info" sx={{ mb: 2 }}>
            {refundMatches.size === 1
              ? "Een regel komt overeen met een openstaande crediteurenfactuur — dit kan een terugbetaling zijn. Controleer of de boeking klopt."
              : `${refundMatches.size} regels komen overeen met openstaande crediteurenfacturen — mogelijke terugbetalingen.`}
          </Alert>
        )}

        {incomplete && (
          <Alert severity="warning" sx={{ mb: 2 }}>
            Een of meer regels missen een tegenrekening. Vul deze eerst in voordat je bevestigt.
          </Alert>
        )}

        {/* One stacked card per item — read-only metadata on top, editable form below. */}
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
          {items.map((item) => {
            const edit = edits.get(item.id);
            const refund = refundMatches.get(item.id);
            const isNegative = item.bedrag < 0;
            return (
              <Box
                key={item.id}
                sx={{
                  border: "1px solid",
                  borderColor: edit?.ledger ? "divider" : "error.light",
                  borderRadius: 2,
                  p: 2,
                }}
              >
                {/* Read-only metadata row */}
                <Box
                  sx={{
                    display: "flex",
                    flexWrap: "wrap",
                    alignItems: "baseline",
                    gap: 2,
                    mb: 1.5,
                  }}
                >
                  <Typography
                    variant="body1"
                    sx={{
                      fontWeight: 700,
                      fontVariantNumeric: "tabular-nums",
                      color: isNegative ? "error.main" : "success.dark",
                    }}
                  >
                    {fmtCurrency(item.bedrag)}
                  </Typography>
                  <Typography variant="body2" color="text.secondary">
                    {fmtDate(item.datum)}
                  </Typography>
                  <Typography variant="body2" color="text.secondary">
                    · {SOORT_LABELS[item.soort] || item.soort}
                  </Typography>
                  {refund && (
                    <Typography variant="caption" sx={{ color: "info.main", flexBasis: "100%" }}>
                      ↳ Mogelijke terugbetaling: {refund.factuurnummer} — {refund.relatie}
                    </Typography>
                  )}
                </Box>

                <Divider sx={{ mb: 1.5 }} />

                {/* Editable form */}
                <Box
                  sx={{
                    display: "grid",
                    gap: 1.5,
                    gridTemplateColumns: { xs: "1fr", sm: "2fr 1fr" },
                  }}
                >
                  <Box>
                    <LedgerAccountPicker
                      accounts={ledgerAccounts}
                      value={edit?.ledger ?? null}
                      onChange={(ledger) => updateEdit(item.id, { ledger })}
                      label="Tegenrekening"
                    />
                    {!edit?.ledger && (
                      <FormHelperText error sx={{ ml: 1.5, mt: 0.5 }}>
                        Tegenrekening verplicht
                      </FormHelperText>
                    )}
                  </Box>
                  <VATCodePicker
                    codes={vatCodes}
                    value={edit?.btwCode ?? "GEEN"}
                    onChange={(btwCode) => updateEdit(item.id, { btwCode })}
                    label="BTW"
                  />
                  <TextField
                    label="Omschrijving"
                    value={edit?.omschrijving ?? ""}
                    onChange={(e) => updateEdit(item.id, { omschrijving: e.target.value })}
                    size="small"
                    fullWidth
                    multiline
                    maxRows={3}
                    sx={{ gridColumn: { sm: "1 / -1" } }}
                  />
                </Box>

                {/* Original bank line description for context */}
                {item.omschrijving && item.omschrijving !== edit?.omschrijving && (
                  <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: "block" }}>
                    Bankregel: {item.omschrijving}
                  </Typography>
                )}
              </Box>
            );
          })}
        </Box>

        {matchingRefunds && (
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, mt: 2 }}>
            <CircularProgress size={14} />
            <Typography variant="caption" color="text.secondary">
              Openstaande facturen worden gecontroleerd...
            </Typography>
          </Box>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={processing}>
          Annuleren
        </Button>
        <Button
          variant="contained"
          onClick={handleConfirm}
          disabled={processing || incomplete}
          sx={{ minWidth: 140, fontWeight: 600 }}
        >
          {processing ? (
            <CircularProgress size={18} color="inherit" aria-label="Wordt verwerkt" />
          ) : (
            "Bevestigen"
          )}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
