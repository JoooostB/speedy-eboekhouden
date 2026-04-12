import { useState, useEffect, useMemo } from "react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Typography,
  Chip,
  Alert,
  CircularProgress,
  Table,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "@mui/material";
import type { InboxClassification, LedgerAccount, OpenPost } from "../../api/types";
import { api } from "../../api/client";

interface Props {
  open: boolean;
  onClose: () => void;
  items: InboxClassification[];
  ledgerAccounts: LedgerAccount[];
  onConfirm: () => Promise<void> | void;
  processing: boolean;
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
  return new Intl.DateTimeFormat("nl-NL", { day: "numeric", month: "short" }).format(d);
}

/**
 * BookingConfirmDialog — quick confirmation modal for processing inbox items
 * without an invoice attached. Used by both the batch approve bar and the
 * single-item "Verwerken" button on InboxRow.
 *
 * Shows a summary table of how each line will be booked (tegenrekening, BTW,
 * soort) so the user can sanity-check before committing. For positive amounts
 * (potential refunds), it cross-references open crediteuren posts and flags
 * any matches so the user can decide whether to couple them to an existing
 * invoice instead of booking as a generic income line.
 *
 * Pure presentation + side-effect: the parent owns the actual processing
 * logic and passes onConfirm to be called when the user confirms.
 */
export function BookingConfirmDialog({
  open,
  onClose,
  items,
  ledgerAccounts,
  onConfirm,
  processing,
}: Props) {
  const [refundMatches, setRefundMatches] = useState<Map<number, OpenPost>>(new Map());
  const [matchingRefunds, setMatchingRefunds] = useState(false);

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

  const ledgerLookup = useMemo(() => {
    const map = new Map<string, LedgerAccount>();
    ledgerAccounts.forEach((a) => map.set(a.code, a));
    return map;
  }, [ledgerAccounts]);

  const isSingle = items.length === 1;
  const incomplete = items.some((i) => !i.grootboekcode);

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
          Controleer hoe {isSingle ? "deze regel wordt" : "deze regels worden"} geboekt in
          e-Boekhouden. Klik op Bevestigen om de mutatie aan te maken.
        </Typography>

        {refundMatches.size > 0 && (
          <Alert severity="info" sx={{ mb: 2 }}>
            {refundMatches.size === 1
              ? "Een regel komt overeen met een openstaande crediteurenfactuur — dit kan een terugbetaling zijn. Controleer of de boeking klopt of koppel handmatig aan de factuur."
              : `${refundMatches.size} regels komen overeen met openstaande crediteurenfacturen — mogelijke terugbetalingen.`}
          </Alert>
        )}

        {incomplete && (
          <Alert severity="warning" sx={{ mb: 2 }}>
            Een of meer regels missen een tegenrekening. Vul deze eerst in voordat je bevestigt.
          </Alert>
        )}

        <Box sx={{ overflow: "auto" }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Datum</TableCell>
                <TableCell align="right">Bedrag</TableCell>
                <TableCell>Omschrijving</TableCell>
                <TableCell>Tegenrekening</TableCell>
                <TableCell>BTW</TableCell>
                <TableCell>Soort</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {items.map((item) => {
                const ledger = ledgerLookup.get(item.grootboekcode);
                const refund = refundMatches.get(item.id);
                const isNegative = item.bedrag < 0;
                return (
                  <TableRow key={item.id}>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>{fmtDate(item.datum)}</TableCell>
                    <TableCell
                      align="right"
                      sx={{
                        fontVariantNumeric: "tabular-nums",
                        color: isNegative ? "error.main" : "success.dark",
                        fontWeight: 600,
                        whiteSpace: "nowrap",
                      }}
                    >
                      {fmtCurrency(item.bedrag)}
                    </TableCell>
                    <TableCell sx={{ maxWidth: 240 }}>
                      <Typography
                        variant="body2"
                        sx={{
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                          whiteSpace: "nowrap",
                        }}
                      >
                        {item.aiOmschrijving || item.omschrijving}
                      </Typography>
                      {refund && (
                        <Typography
                          variant="caption"
                          sx={{ color: "info.main", display: "block", mt: 0.25 }}
                        >
                          ↳ Mogelijke terugbetaling: {refund.factuurnummer} — {refund.relatie}
                        </Typography>
                      )}
                    </TableCell>
                    <TableCell>
                      {item.grootboekcode ? (
                        <Box>
                          <Typography variant="body2" fontWeight={600}>
                            {item.grootboekcode}
                          </Typography>
                          {ledger && (
                            <Typography variant="caption" color="text.secondary">
                              {ledger.omschrijving}
                            </Typography>
                          )}
                        </Box>
                      ) : (
                        <Chip
                          size="small"
                          label="ontbreekt"
                          color="error"
                          sx={{ height: 20, fontSize: "0.6875rem" }}
                        />
                      )}
                    </TableCell>
                    <TableCell>
                      <Typography variant="caption">{item.btwCode || "GEEN"}</Typography>
                    </TableCell>
                    <TableCell>
                      <Typography variant="caption">
                        {SOORT_LABELS[item.soort] || item.soort}
                      </Typography>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
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
          onClick={() => onConfirm()}
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
