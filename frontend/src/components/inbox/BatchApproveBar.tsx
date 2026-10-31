import { useState } from "react";
import { Box, Button, Typography, LinearProgress, Chip, Tooltip } from "@mui/material";
import type { InboxClassification, InboxProcessItem, InboxProcessResult, LedgerAccount, VATCode } from "../../api/types";
import { api } from "../../api/client";
import { track } from "../../analytics";
import { BookingConfirmDialog } from "./BookingConfirmDialog";
import { toInboxProcessItem } from "./inboxBooking";
import { allIncoming } from "./paymentGroups";

interface Props {
  selected: InboxClassification[];
  onProcessed: (results: Map<number, InboxProcessResult>) => void;
  onClear: () => void;
  ledgerAccounts: LedgerAccount[];
  vatCodes: VATCode[];
  /** Opens the payment-group dialog with the selection: several incoming
   *  lines that together pay one sales invoice. */
  onLinkToInvoice?: (selected: InboxClassification[]) => void;
}

const LINK_DISABLED_TOOLTIP = "Alleen ontvangsten kun je samen op één verkoopfactuur koppelen.";

/**
 * BatchApproveBar — sticky bottom bar that appears when inbox items
 * are selected. Shows count, processes all selected in one batch call,
 * and displays results summary.
 *
 * Accessibility:
 * - role="status" on the count so screen readers announce selection changes.
 * - aria-live="polite" on the results region for post-processing feedback.
 * - Button is disabled during processing to prevent double-submit.
 * - Focus is managed: after completion, results are announced via live region.
 */
export function BatchApproveBar({ selected, onProcessed, onClear, ledgerAccounts, vatCodes, onLinkToInvoice }: Props) {
  const [processing, setProcessing] = useState(false);
  const [progress, setProgress] = useState(0);
  const [results, setResults] = useState<{ ok: number; error: number } | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [dialogError, setDialogError] = useState<string | null>(null);

  // Receives the items as edited by the user in the confirm dialog.
  const handleApprove = async (editedItems: InboxClassification[]) => {
    setProcessing(true);
    setProgress(0);
    setResults(null);
    setDialogError(null);

    // Shared payload builder: a factuurbetaling row (open-post match) carries
    // its relation and invoice number through to the backend.
    const items: InboxProcessItem[] = editedItems.map((item) => toInboxProcessItem(item));

    try {
      // Process in chunks of 20 to show meaningful progress
      const chunkSize = 20;
      const allResults = new Map<number, InboxProcessResult>();
      let ok = 0;
      let error = 0;

      for (let i = 0; i < items.length; i += chunkSize) {
        const chunk = items.slice(i, i + chunkSize);
        const res = await api.processInboxBatch(chunk);
        res.results.forEach((r, idx) => {
          const id = chunk[idx].id;
          allResults.set(id, r);
          if (r.status === "ok") ok++;
          else error++;
        });
        setProgress(Math.round(((i + chunk.length) / items.length) * 100));
      }

      setResults({ ok, error });
      onProcessed(allResults);
      track("Inbox Batch Approve", { count: String(editedItems.length), ok: String(ok), error: String(error) });

      if (error > 0) {
        // Surface the first error inside the dialog instead of silently
        // closing it. The user gets to see what went wrong and try again.
        const firstError = Array.from(allResults.values()).find((r) => r.status === "error");
        setDialogError(
          ok === 0
            ? `Boeken mislukt: ${firstError?.error || "onbekende fout"}`
            : `${error} van de ${editedItems.length} boekingen mislukt. Eerste fout: ${firstError?.error || "onbekend"}`,
        );
        // Keep the dialog open so the user can adjust and retry.
      } else {
        setConfirmOpen(false);
      }
    } catch (err: any) {
      setResults({ ok: 0, error: editedItems.length });
      setDialogError(err?.message || "Verwerking mislukt");
    } finally {
      setProcessing(false);
    }
  };

  return (
    <Box
      sx={{
        position: "fixed",
        bottom: 0,
        left: 0,
        right: 0,
        zIndex: 1200,
        bgcolor: "background.paper",
        borderTop: "1px solid",
        borderColor: "divider",
        boxShadow: "0 -4px 24px rgba(0,0,0,0.08)",
        px: 3,
        py: 2,
        /* Slide up animation that respects motion preferences */
        animation: "inboxBarSlideUp 200ms ease-out",
        "@media (prefers-reduced-motion: reduce)": {
          animation: "none",
        },
        "@keyframes inboxBarSlideUp": {
          from: { transform: "translateY(100%)" },
          to: { transform: "translateY(0)" },
        },
      }}
      role="region"
      aria-label="Batchverwerking"
    >
      {processing && (
        <LinearProgress
          variant="determinate"
          value={progress}
          sx={{
            position: "absolute",
            top: 0,
            left: 0,
            right: 0,
            height: 3,
          }}
          aria-label={`Verwerking ${progress}% voltooid`}
        />
      )}

      <Box
        sx={{
          maxWidth: "lg",
          mx: "auto",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          flexWrap: "wrap",
          gap: 2,
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
          {/* Selection count — announced to screen readers on change */}
          <Typography variant="body2" sx={{ fontWeight: 600 }} role="status">
            {selected.length} geselecteerd
          </Typography>

          {/* Results summary — announced after processing completes */}
          {results && (
            <Box aria-live="polite" sx={{ display: "flex", gap: 1 }}>
              {results.ok > 0 && (
                <Chip
                  label={`${results.ok} verwerkt`}
                  size="small"
                  sx={{
                    bgcolor: "rgba(22, 163, 74, 0.1)",
                    color: "success.dark",
                    fontWeight: 600,
                    fontSize: "0.75rem",
                  }}
                />
              )}
              {results.error > 0 && (
                <Chip
                  label={`${results.error} mislukt`}
                  size="small"
                  sx={{
                    bgcolor: "rgba(220, 38, 38, 0.1)",
                    color: "error.dark",
                    fontWeight: 600,
                    fontSize: "0.75rem",
                  }}
                />
              )}
            </Box>
          )}
        </Box>

        <Box sx={{ display: "flex", gap: 1.5, flexWrap: "wrap" }}>
          <Button
            variant="text"
            size="small"
            onClick={onClear}
            disabled={processing}
          >
            Deselecteren
          </Button>
          {onLinkToInvoice && (
            allIncoming(selected) ? (
              <Button
                variant="outlined"
                size="small"
                onClick={() => onLinkToInvoice(selected)}
                disabled={processing}
                sx={{ fontWeight: 600 }}
              >
                Koppelen aan één factuur
              </Button>
            ) : (
              <Tooltip title={LINK_DISABLED_TOOLTIP} arrow>
                {/* A disabled button fires no events, so the tooltip hangs on the span. */}
                <Box component="span" sx={{ display: "inline-flex" }} tabIndex={0} aria-label={LINK_DISABLED_TOOLTIP}>
                  <Button variant="outlined" size="small" disabled sx={{ fontWeight: 600, pointerEvents: "none" }}>
                    Koppelen aan één factuur
                  </Button>
                </Box>
              </Tooltip>
            )
          )}
          <Button
            variant="contained"
            size="small"
            onClick={() => setConfirmOpen(true)}
            disabled={processing || selected.length === 0}
            sx={{ minWidth: 140, fontWeight: 600 }}
          >
            {processing ? "Verwerken…" : "Goedkeuren"}
          </Button>
        </Box>
      </Box>

      <BookingConfirmDialog
        open={confirmOpen}
        onClose={() => {
          if (!processing) {
            setConfirmOpen(false);
            setDialogError(null);
          }
        }}
        items={selected}
        ledgerAccounts={ledgerAccounts}
        vatCodes={vatCodes}
        onConfirm={handleApprove}
        processing={processing}
        error={dialogError}
      />
    </Box>
  );
}
