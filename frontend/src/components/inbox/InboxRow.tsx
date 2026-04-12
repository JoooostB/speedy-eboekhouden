import { useState, useCallback, useRef } from "react";
import {
  Box,
  Checkbox,
  Chip,
  Collapse,
  Typography,
  Button,
  TextField,
  MenuItem,
  IconButton,
  Alert,
  CircularProgress,
} from "@mui/material";
import type {
  InboxClassification,
  InboxCategory,
  InboxProcessItem,
  LedgerAccount,
  MutatieSoort,
  Relation,
  VATCode,
} from "../../api/types";
import { api } from "../../api/client";
import { track } from "../../analytics";
import { LedgerAccountPicker } from "../shared/LedgerAccountPicker";
import { RelationPicker } from "../shared/RelationPicker";
import { VATCodePicker } from "../shared/VATCodePicker";
import { BookingConfirmDialog } from "./BookingConfirmDialog";

/**
 * Category config: color, label, and whether inline editing is shown.
 * Colors chosen for WCAG AA contrast on white backgrounds.
 */
const CATEGORY_CONFIG: Record<InboxCategory, { label: string; color: string; bgcolor: string }> = {
  auto: { label: "Auto", color: "#166534", bgcolor: "rgba(22, 163, 74, 0.1)" },
  review: { label: "Controleer", color: "#92400e", bgcolor: "rgba(245, 158, 11, 0.1)" },
  invoice: { label: "Factuur", color: "#1e40af", bgcolor: "rgba(59, 130, 246, 0.1)" },
  manual: { label: "Handmatig", color: "#991b1b", bgcolor: "rgba(220, 38, 38, 0.1)" },
};

/**
 * Mutation type options for the soort dropdown.
 * Dutch labels match e-boekhouden terminology.
 */
const SOORT_OPTIONS: { value: MutatieSoort; label: string }[] = [
  { value: "GeldOntvangen", label: "Geld ontvangen" },
  { value: "GeldUitgegeven", label: "Geld uitgegeven" },
  { value: "FactuurbetalingOntvangen", label: "Factuurbetaling ontvangen" },
  { value: "FactuurbetalingVerstuurd", label: "Factuurbetaling verstuurd" },
  { value: "FactuurOntvangen", label: "Factuur ontvangen" },
  { value: "FactuurVerstuurd", label: "Factuur verstuurd" },
  { value: "Memoriaal", label: "Memoriaal" },
];

interface Props {
  item: InboxClassification;
  checked: boolean;
  onToggle: (id: number) => void;
  ledgerAccounts: LedgerAccount[];
  vatCodes: VATCode[];
  onProcessed: (id: number) => void;
  /** Called when user uploads an invoice PDF on this row — parent opens review dialog */
  onInvoiceUpload?: (file: File, bankLineId: number) => void;
  /** Whether this row has already been processed (show success state). */
  processed?: boolean;
}

/**
 * InboxRow — a single inbox item with expand/collapse.
 *
 * Collapsed: checkbox, date, amount, description, category badge, indicator.
 * Expanded (review/manual): editable fields for soort, grootboek, relatie, BTW, omschrijving.
 * Expanded (invoice): file drop zone, match verification, pre-filled fields.
 *
 * Accessibility:
 * - The row itself is a <li> within the parent <ul>.
 * - Checkbox has an accessible label describing the transaction.
 * - Expand/collapse uses aria-expanded and aria-controls.
 * - The expanded panel has role="region" and aria-labelledby pointing to the row header.
 * - File drop zone is keyboard-accessible via a hidden <input type="file">.
 * - Amount color is always paired with +/- text prefix (not color alone).
 * - Category badge includes text label (not color alone).
 */
export function InboxRow({
  item,
  checked,
  onToggle,
  ledgerAccounts,
  vatCodes,
  onProcessed,
  onInvoiceUpload,
  processed = false,
}: Props) {
  const [expanded, setExpanded] = useState(false);
  const [processing, setProcessing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);

  // Editable fields (pre-filled from AI classification)
  const [soort, setSoort] = useState<MutatieSoort>(item.soort);
  const [selectedLedger, setSelectedLedger] = useState<LedgerAccount | null>(
    ledgerAccounts.find((a) => a.code === item.grootboekcode) || null,
  );
  const [relatie, setRelatie] = useState<Relation | null>(null);
  const [btwCode, setBtwCode] = useState(item.btwCode);
  const [omschrijving, setOmschrijving] = useState(item.aiOmschrijving || item.omschrijving);

  // Invoice upload state
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const panelId = `inbox-row-panel-${item.id}`;
  const headerId = `inbox-row-header-${item.id}`;
  const cat = CATEGORY_CONFIG[item.category];

  const isNegative = item.bedrag < 0;
  const formattedAmount = new Intl.NumberFormat("nl-NL", {
    style: "currency",
    currency: "EUR",
  }).format(item.bedrag);

  const formattedDate = new Intl.DateTimeFormat("nl-NL", {
    day: "numeric",
    month: "short",
  }).format(new Date(item.datum));

  const canExpand = item.category !== "auto";

  const handleExpand = () => {
    if (canExpand) setExpanded((prev) => !prev);
  };

  // Process a single item (from expanded form)
  const handleProcess = async () => {
    setProcessing(true);
    setError(null);

    const soortCodes: Record<string, number> = {
      FactuurOntvangen: 1, FactuurVerstuurd: 2,
      FactuurbetalingOntvangen: 3, FactuurbetalingVerstuurd: 4,
      GeldOntvangen: 5, GeldUitgegeven: 6, Memoriaal: 7,
    };

    const payload: InboxProcessItem = {
      id: item.id,
      grootboekId: item.grootboekId,
      soort: soortCodes[soort] || 6,
      grootboekcode: selectedLedger?.code || item.grootboekcode,
      btwCode,
      omschrijving,
      relatieId: relatie?.id,
      bedrag: Math.abs(item.bedrag),
    };

    try {
      const res = await api.processInboxBatch([payload]);
      if (res.results[0]?.status === "ok") {
        onProcessed(item.id);
        track("Inbox Process Single", { category: item.category });
        setConfirmOpen(false);
      } else {
        setError(res.results[0]?.error || "Verwerking mislukt");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Verwerking mislukt");
    } finally {
      setProcessing(false);
    }
  };

  // Build the InboxClassification preview the dialog expects (with the user's
  // current edits applied) so the confirmation summary reflects what will
  // actually be booked.
  const previewItem: InboxClassification = {
    ...item,
    grootboekcode: selectedLedger?.code || item.grootboekcode,
    btwCode,
    soort,
    aiOmschrijving: omschrijving,
  };

  // Invoice file upload — delegate to parent to use the shared review dialog
  const handleFileSelect = useCallback(
    (file: File) => {
      if (onInvoiceUpload) {
        onInvoiceUpload(file, item.id);
      }
    },
    [item.id, onInvoiceUpload],
  );

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragOver(false);
      const file = e.dataTransfer.files[0];
      if (file && file.type === "application/pdf") {
        handleFileSelect(file);
      }
    },
    [handleFileSelect],
  );

  const handleFileInput = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (file) handleFileSelect(file);
    },
    [handleFileSelect],
  );

  return (
    <Box
      component="li"
      sx={{
        listStyle: "none",
        borderBottom: "1px solid",
        borderColor: "divider",
        bgcolor: processed ? "rgba(22, 163, 74, 0.04)" : "background.paper",
        transition: "background-color 0.2s",
        "&:last-child": { borderBottom: 0 },
      }}
    >
      {/* Collapsed row header */}
      <Box
        id={headerId}
        sx={{
          display: "flex",
          alignItems: "center",
          gap: { xs: 1, sm: 2 },
          px: { xs: 1.5, sm: 2.5 },
          py: 1.5,
          cursor: canExpand ? "pointer" : "default",
          "&:hover": canExpand
            ? { bgcolor: "rgba(0,0,0,0.02)" }
            : undefined,
        }}
        role="row"
      >
        {/* Checkbox — stops event propagation so clicking it doesn't toggle expand */}
        <Checkbox
          checked={checked}
          onChange={() => onToggle(item.id)}
          onClick={(e) => e.stopPropagation()}
          disabled={processed}
          inputProps={{
            "aria-label": `Selecteer ${item.omschrijving}, ${formattedAmount}`,
          }}
          size="small"
          sx={{ p: 0.5 }}
        />

        {/* Date */}
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{
            minWidth: 52,
            fontSize: "0.8125rem",
            flexShrink: 0,
          }}
        >
          {formattedDate}
        </Typography>

        {/* Amount — color paired with +/- prefix, never color alone */}
        <Typography
          variant="body2"
          sx={{
            minWidth: 80,
            textAlign: "right",
            fontWeight: 600,
            fontVariantNumeric: "tabular-nums",
            fontSize: "0.875rem",
            color: isNegative ? "error.main" : "success.dark",
            flexShrink: 0,
          }}
        >
          {formattedAmount}
        </Typography>

        {/* Description + indicator */}
        <Box
          sx={{
            flex: 1,
            minWidth: 0,
            cursor: canExpand ? "pointer" : "default",
          }}
          onClick={handleExpand}
          role={canExpand ? "button" : undefined}
          tabIndex={canExpand ? 0 : undefined}
          aria-expanded={canExpand ? expanded : undefined}
          aria-controls={canExpand ? panelId : undefined}
          onKeyDown={(e) => {
            if (canExpand && (e.key === "Enter" || e.key === " ")) {
              e.preventDefault();
              handleExpand();
            }
          }}
        >
          <Typography
            variant="body2"
            sx={{
              fontWeight: 500,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {item.omschrijving}
          </Typography>
          {item.indicator && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                display: "block",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {item.indicator}
            </Typography>
          )}
        </Box>

        {/* Category badge — text label ensures info is not color-only */}
        <Chip
          label={processed ? "Verwerkt" : cat.label}
          size="small"
          sx={{
            fontWeight: 600,
            fontSize: "0.6875rem",
            height: 24,
            flexShrink: 0,
            bgcolor: processed ? "rgba(22, 163, 74, 0.1)" : cat.bgcolor,
            color: processed ? "#166534" : cat.color,
          }}
        />

        {/* Expand arrow — only for expandable categories */}
        {canExpand && (
          <IconButton
            size="small"
            onClick={(e) => {
              e.stopPropagation();
              handleExpand();
            }}
            aria-expanded={expanded}
            aria-controls={panelId}
            aria-label={expanded ? "Inklappen" : "Uitklappen"}
            sx={{
              p: 0.5,
              transition: "transform 0.2s",
              transform: expanded ? "rotate(180deg)" : "rotate(0deg)",
              "@media (prefers-reduced-motion: reduce)": {
                transition: "none",
              },
            }}
          >
            {/* Chevron down SVG */}
            <Box
              component="svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth={2}
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
              sx={{ width: 18, height: 18 }}
            >
              <polyline points="6 9 12 15 18 9" />
            </Box>
          </IconButton>
        )}
      </Box>

      {/* Expanded panel */}
      {canExpand && (
        <Collapse in={expanded}>
          <Box
            id={panelId}
            role="region"
            aria-labelledby={headerId}
            sx={{
              px: { xs: 2, sm: 3 },
              pb: 2.5,
              pt: 0.5,
              ml: { xs: 0, sm: 5.5 },
            }}
          >
            {error && (
              <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
                {error}
              </Alert>
            )}

            {/* Invoice upload zone — for "invoice" category, delegates to parent review dialog */}
            {item.category === "invoice" && onInvoiceUpload && (
              <Box
                onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
                onDragLeave={() => setDragOver(false)}
                onDrop={handleDrop}
                onClick={() => fileInputRef.current?.click()}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); fileInputRef.current?.click(); } }}
                role="button"
                tabIndex={0}
                aria-label="Upload een PDF-factuur"
                sx={{
                  border: "2px dashed",
                  borderColor: dragOver ? "primary.main" : "grey.300",
                  borderRadius: 2,
                  p: 2.5,
                  mb: 2,
                  textAlign: "center",
                  cursor: "pointer",
                  bgcolor: dragOver ? "rgba(21, 101, 192, 0.04)" : "transparent",
                  "&:hover": { borderColor: "primary.light" },
                }}
              >
                <Typography variant="body2" color="text.secondary">
                  Sleep een PDF hierheen of klik om te uploaden
                </Typography>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="application/pdf"
                  onChange={handleFileInput}
                  style={{ display: "none" }}
                  aria-hidden="true"
                  tabIndex={-1}
                />
              </Box>
            )}

            {/* Editable fields — shown for review, manual, and invoice categories */}
            {(
              <Box
                sx={{
                  display: "grid",
                  gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
                  gap: 2,
                  mb: 2,
                }}
              >
                <TextField
                  select
                  label="Soort"
                  value={soort}
                  onChange={(e) => setSoort(e.target.value as MutatieSoort)}
                  size="small"
                  fullWidth
                >
                  {SOORT_OPTIONS.map((opt) => (
                    <MenuItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </MenuItem>
                  ))}
                </TextField>

                <LedgerAccountPicker
                  accounts={ledgerAccounts}
                  value={selectedLedger}
                  onChange={setSelectedLedger}
                />

                <RelationPicker
                  value={relatie}
                  onChange={setRelatie}
                />

                <VATCodePicker
                  codes={vatCodes}
                  value={btwCode}
                  onChange={setBtwCode}
                />

                <TextField
                  label="Omschrijving"
                  value={omschrijving}
                  onChange={(e) => setOmschrijving(e.target.value)}
                  size="small"
                  fullWidth
                  multiline
                  maxRows={3}
                  sx={{ gridColumn: { sm: "1 / -1" } }}
                />
              </Box>
            )}

            <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
              <Button
                variant="contained"
                size="small"
                onClick={() => setConfirmOpen(true)}
                disabled={processing || processed}
                sx={{ fontWeight: 600, minWidth: 120 }}
              >
                {processing ? (
                  <CircularProgress size={18} color="inherit" aria-label="Wordt verwerkt" />
                ) : (
                  "Verwerken"
                )}
              </Button>
            </Box>
          </Box>
        </Collapse>
      )}

      <BookingConfirmDialog
        open={confirmOpen}
        onClose={() => !processing && setConfirmOpen(false)}
        items={[previewItem]}
        ledgerAccounts={ledgerAccounts}
        onConfirm={handleProcess}
        processing={processing}
      />
    </Box>
  );
}
