import { useEffect, useRef, useState } from "react";
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Typography } from "@mui/material";
import { api } from "../../api/client";
import { track } from "../../analytics";
import type { InboxClassification, LedgerAccount, VATCode } from "../../api/types";
import { formatMoney } from "../../hooks/mailDocumentsModel";
import { RegelsEditor } from "../shared/RegelsEditor";
import { toInboxProcessItem } from "./inboxBooking";
import { formatAmountInput } from "./reviewAmounts";
import { splitFromSingle, toCents, toRegelPayload, validateRegels, type RegelEdit } from "./regels";

interface Props {
  open: boolean;
  onClose: () => void;
  /** Called after the dialog has closed, so the opener can put focus on an
   *  element that survives the row being marked processed. */
  onExited?: () => void;
  /** The bank line to split, with the row's current ledger, BTW code and
   *  booking description (aiOmschrijving). */
  item: InboxClassification;
  /** The relation the user picked on the row, like the single booking. */
  relatieId?: number;
  /** "Geld uitgegeven" / "Geld ontvangen", as the row shows it. */
  soortLabel: string;
  ledgerAccounts: LedgerAccount[];
  vatCodes: VATCode[];
  /** Called once the line is booked; the row then leaves the list. */
  onProcessed: (id: number) => void;
}

const dateFormat = new Intl.DateTimeFormat("nl-NL", { day: "numeric", month: "long", year: "numeric" });

/**
 * BankLineSplitDialog — "Afschriftregel splitsen": books one bank line
 * (Geld uitgegeven / Geld ontvangen) as a mutation with several regels,
 * each on its own tegenrekening with its own BTW code. The total is the
 * bank amount, so there is no total field; the editor opens with the row's
 * suggested ledger and BTW code as line 1 and focus on line 2's amount. The
 * backend checks the sum against the grid itself and resolves each line by
 * grootboekcode.
 *
 * The dialog stays open on an error; the reason sits above the actions,
 * always in view, and takes focus. The booking button is not disabled
 * while the request runs, so focus stays on it through the round-trip.
 */
export function BankLineSplitDialog({
  open,
  onClose,
  onExited,
  item,
  relatieId,
  soortLabel,
  ledgerAccounts,
  vatCodes,
  onProcessed,
}: Props) {
  const totalCents = toCents(Math.abs(item.bedrag));
  const [regels, setRegels] = useState<RegelEdit[]>(() =>
    splitFromSingle({
      ledgerAccount: ledgerAccounts.find((a) => a.code === item.grootboekcode) ?? null,
      btwCode: item.btwCode,
      bedragIncl: formatAmountInput(Math.abs(item.bedrag)),
    }),
  );
  const initialFocusKey = useRef(regels[1]?.key);
  const [processing, setProcessing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const errorRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  const vatRate = (code: string) => vatCodes.find((v) => v.code === code)?.percentage ?? 0;
  const blocker = validateRegels(regels, totalCents);
  const bookingOmschrijving = item.aiOmschrijving || item.omschrijving;

  const handleBook = async () => {
    if (processing || blocker !== null) return;
    setProcessing(true);
    setError(null);
    try {
      const res = await api.processInboxBatch([
        { ...toInboxProcessItem(item, relatieId), regels: toRegelPayload(regels, "code") },
      ]);
      const result = res.results[0];
      if (result?.status === "ok") {
        track("booking_split", { path: "afschrift", regels: regels.length });
        onClose();
        onProcessed(item.id);
      } else {
        setError(result?.error || "Boeken mislukt");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Boeken mislukt");
    } finally {
      setProcessing(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={processing ? undefined : onClose}
      fullWidth
      maxWidth="md"
      aria-labelledby="bank-line-split-title"
      aria-describedby="bank-line-split-summary"
      slotProps={{ transition: { onExited } }}
    >
      <DialogTitle id="bank-line-split-title">Afschriftregel splitsen</DialogTitle>
      <DialogContent>
        <Box
          component="dl"
          id="bank-line-split-summary"
          sx={{
            m: 0,
            mb: 2,
            display: "grid",
            gridTemplateColumns: "auto 1fr",
            columnGap: 2,
            rowGap: 0.5,
            "& dt": { color: "text.secondary" },
            "& dd": { m: 0 },
          }}
        >
          <Typography component="dt" variant="body2">Datum</Typography>
          <Typography component="dd" variant="body2">{dateFormat.format(new Date(item.datum))}</Typography>
          <Typography component="dt" variant="body2">Bedrag</Typography>
          <Typography component="dd" variant="body2">
            {formatMoney(Math.abs(item.bedrag))} · {soortLabel}
          </Typography>
          <Typography component="dt" variant="body2">Afschrift</Typography>
          <Typography component="dd" variant="body2">{item.omschrijving}</Typography>
          <Typography component="dt" variant="body2">Omschrijving boeking</Typography>
          <Typography component="dd" variant="body2">{bookingOmschrijving}</Typography>
        </Box>
        <RegelsEditor
          regels={regels}
          totalCents={totalCents}
          totalSource="afschrift"
          ledgerAccounts={ledgerAccounts}
          vatCodes={vatCodes}
          vatRate={vatRate}
          onChange={setRegels}
          disabled={processing}
          initialFocusKey={initialFocusKey.current}
        />
      </DialogContent>
      {error && (
        <Box sx={{ px: 3 }}>
          <Alert severity="error" ref={errorRef} tabIndex={-1}>
            {error}
          </Alert>
        </Box>
      )}
      <DialogActions sx={{ px: 3, py: 2 }}>
        <Button onClick={onClose} disabled={processing}>
          Annuleren
        </Button>
        <Button
          variant="contained"
          onClick={handleBook}
          aria-disabled={processing || blocker !== null}
          disabled={!processing && blocker !== null}
          sx={{ fontWeight: 600 }}
        >
          {processing ? "Bezig met boeken…" : `Boeken (${regels.length} regels)`}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
