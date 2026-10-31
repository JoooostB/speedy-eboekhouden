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
  CircularProgress,
  Tooltip,
} from "@mui/material";
import type {
  InboxClassification,
  InboxCategory,
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
import { BankLineSplitDialog } from "./BankLineSplitDialog";
import { formatMoney } from "../../hooks/mailDocumentsModel";
import {
  VERIFICATION_CHIP_LABELS,
  VERIFICATION_CHIP_SX,
  VERIFICATION_CHIP_TOOLTIPS,
  amountAriaLabel,
  boekenVerificationLabel,
  documentMatchIndicator,
  koppelenLabel,
  toInboxProcessItem,
} from "./inboxBooking";
import { INVOICE_FILE_ACCEPT, isAcceptedInvoiceFile } from "./mailDocumentReview";
import { TruncatedText } from "./TruncatedText";

/**
 * Category config: color, label, and whether inline editing is shown.
 * Colors chosen for WCAG AA contrast on white backgrounds.
 */
const CATEGORY_CONFIG: Record<InboxCategory, { label: string; color: string; bgcolor: string }> = {
  auto: { label: "Auto", color: "#166534", bgcolor: "rgba(22, 163, 74, 0.1)" },
  review: { label: "Controleren", color: "#92400e", bgcolor: "rgba(245, 158, 11, 0.1)" },
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
  /** Called with the mail-folder file id when the user wants to book this
   *  line together with the document that matches it. Only when provided
   *  does a row with a documentMatch offer "Factuur koppelen en boeken"
   *  instead of the upload zone. */
  onOpenDocument?: (fileId: number) => void;
  /** Whether this row has already been processed (show success state). */
  processed?: boolean;
  /** Called when the user says an incoming line pays an open sales invoice,
   *  possibly together with other lines; the parent opens the group
   *  dialog with this line. Only offered on incoming lines. */
  onLinkToInvoice?: (item: InboxClassification) => void;
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
 *
 * Two overlays from the backend change the row:
 * - openPostMatch: the line pays an outstanding invoice in e-boekhouden. The
 *   row shows "Betaalt open factuur" and a "Koppelen" button that opens the
 *   confirm dialog prefilled; there is nothing to expand.
 * - documentMatch: the line is the payment of an analysed mail-folder
 *   document. The row shows "Document gevonden" and, when the parent passes
 *   onOpenDocument, "Factuur koppelen en boeken" replaces the upload zone.
 */
export function InboxRow({
  item,
  checked,
  onToggle,
  ledgerAccounts,
  vatCodes,
  onProcessed,
  onInvoiceUpload,
  onOpenDocument,
  processed = false,
  onLinkToInvoice,
}: Props) {
  const [expanded, setExpanded] = useState(false);
  const [processing, setProcessing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [splitOpen, setSplitOpen] = useState(false);
  const headerRef = useRef<HTMLDivElement>(null);

  // Editable fields (pre-filled from AI classification)
  const [soort, setSoort] = useState<MutatieSoort>(item.soort);
  const [selectedLedger, setSelectedLedger] = useState<LedgerAccount | null>(
    ledgerAccounts.find((a) => a.code === item.grootboekcode) || null,
  );
  const [relatie, setRelatie] = useState<Relation | null>(null);
  const [btwCode, setBtwCode] = useState(item.verification ? "GEEN" : item.btwCode);
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

  // An open-post row has nothing to edit inline: the booking is fixed by the
  // open invoice and confirmed through the dialog.
  const isOpenPost = !!item.openPostMatch;
  const documentMatch = item.documentMatch;
  // A verification payment (card or IBAN check) is booked on Bijzondere
  // baten straight from the header, like an open-post row; without a
  // ledger account the backend made it "review" and the row expands so the
  // user picks one (BTW stays GEEN).
  const verification = item.verification;
  const canExpand = item.category !== "auto" && !isOpenPost;
  const indicatorText = documentMatch ? documentMatchIndicator(documentMatch.title) : item.indicator;
  // "Splitsen…": a Geld uitgegeven / Geld ontvangen line can be booked over
  // several tegenrekeningen. Never a factuurbetaling, an open-post row or a
  // line of a payment group (those settle one invoice).
  const canSplit =
    !processed && !isOpenPost && !item.paymentGroupId && (soort === "GeldUitgegeven" || soort === "GeldOntvangen");

  const handleExpand = () => {
    if (canExpand) setExpanded((prev) => !prev);
  };

  // Process a single item — receives the (possibly edited) item from the
  // confirmation dialog so the user's last-minute tweaks are honored.
  const handleProcess = async (editedItems: InboxClassification[]) => {
    setProcessing(true);
    setError(null);

    const edited = editedItems[0] ?? item;
    const payload = toInboxProcessItem(edited, relatie?.id);

    try {
      const res = await api.processInboxBatch([payload]);
      if (res.results[0]?.status === "ok") {
        onProcessed(item.id);
        track("Inbox Process Single", { category: item.category });
        if (verification) track("inbox_verification_booked", { kind: verification.kind, count: 1, failed: 0 });
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
      if (file && isAcceptedInvoiceFile(file)) {
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
      {/* Collapsed row header; focusable by script only, so the split
          dialog can hand focus back after the row was marked processed. */}
      <Box
        id={headerId}
        ref={headerRef}
        tabIndex={-1}
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
      >
        {/* Checkbox — stops event propagation so clicking it doesn't toggle expand */}
        <Checkbox
          checked={checked}
          onChange={() => onToggle(item.id)}
          onClick={(e) => e.stopPropagation()}
          disabled={processed}
          slotProps={{
            input: {
              "aria-label": `Selecteer ${item.omschrijving}, ${formattedAmount}`,
            },
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

        {/* Amount — color paired with +/- prefix, never color alone; read
            out as "Afgeschreven/Bijgeschreven € 1,00" so the direction is
            words, not a glyph. */}
        <Typography
          variant="body2"
          aria-label={amountAriaLabel(item.bedrag)}
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
          <Box component="span" aria-hidden="true">
            {formattedAmount}
          </Box>
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
          <TruncatedText
            text={item.omschrijving}
            variant="body2"
            sx={{ fontWeight: 500 }}
          />
          {indicatorText && (
            <TruncatedText
              text={indicatorText}
              variant="caption"
              color="text.secondary"
            />
          )}
        </Box>

        {/* "Betaalt open factuur" — the line settles an outstanding invoice in
            e-boekhouden. Text label, never colour alone. */}
        {isOpenPost && !processed && (
          <Tooltip
            title="Het bedrag past bij een openstaande factuur in e-Boekhouden. Met Koppelen boek je de betaling op die factuur."
            arrow
          >
            <Chip
              label="Betaalt open factuur"
              size="small"
              sx={{
                fontWeight: 600,
                fontSize: "0.6875rem",
                height: 22,
                flexShrink: 0,
                bgcolor: "rgba(13, 148, 136, 0.12)",
                color: "#0f766e",
                cursor: "help",
              }}
            />
          </Tooltip>
        )}

        {/* "Kaartcontrole" / "Rekeningcontrole" — a verification payment,
            booked on Bijzondere baten. A button described by its tooltip, so
            the explanation reaches keyboard and screen-reader users too. */}
        {verification && !isOpenPost && !processed && (
          <Tooltip title={VERIFICATION_CHIP_TOOLTIPS[verification.kind]} arrow enterDelay={300} describeChild>
            <Chip component="button" type="button" label={VERIFICATION_CHIP_LABELS[verification.kind]} size="small" sx={VERIFICATION_CHIP_SX} />
          </Tooltip>
        )}

        {/* "Document gevonden" — an analysed document in the mail folder
            matches this line. */}
        {documentMatch && !isOpenPost && !processed && (
          <Tooltip title={documentMatchIndicator(documentMatch.title)} arrow>
            <Chip
              label="Document gevonden"
              size="small"
              sx={{
                fontWeight: 600,
                fontSize: "0.6875rem",
                height: 22,
                flexShrink: 0,
                bgcolor: "rgba(2, 132, 199, 0.12)",
                color: "#075985",
                cursor: "help",
              }}
            />
          </Tooltip>
        )}

        {/* "Eerder geboekt" badge — only shown when this row was auto-filled
            from the user's learned classification memory. Helps the user
            understand WHY a row is pre-filled with high confidence.
            The Tooltip works for keyboard users too (the title attribute
            previously only fired on mouse hover). */}
        {item.learned && !processed && (
          <Tooltip
            title="Dit voorstel komt uit je eerdere boekingen, niet van de AI"
            arrow
          >
            <Chip
              label={
                <Box component="span">
                  <Box component="span" aria-hidden="true" sx={{ mr: 0.25 }}>↺</Box>
                  eerder geboekt
                </Box>
              }
              size="small"
              sx={{
                fontWeight: 600,
                fontSize: "0.6875rem",
                height: 22,
                flexShrink: 0,
                bgcolor: "rgba(99, 102, 241, 0.1)",
                color: "#4338ca",
                cursor: "help",
              }}
              aria-label="Eerder geboekt: voorstel uit je eigen boekingen"
            />
          </Tooltip>
        )}

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

        {/* "Koppelen" — an open-post row is confirmed straight from the
            header; it takes the place of the expand arrow. */}
        {isOpenPost && !processed && (
          <Button
            variant="outlined"
            size="small"
            onClick={(e) => {
              e.stopPropagation();
              setConfirmOpen(true);
            }}
            disabled={processing}
            aria-label={koppelenLabel(item)}
            sx={{ flexShrink: 0, fontWeight: 600, minHeight: 32 }}
          >
            Koppelen
          </Button>
        )}

        {/* "Boeken" — a verification payment with a ledger account is
            confirmed straight from the header, one click like Koppelen. */}
        {verification && !isOpenPost && !processed && !!item.grootboekcode && (
          <Button
            variant="outlined"
            size="small"
            onClick={(e) => {
              e.stopPropagation();
              setConfirmOpen(true);
            }}
            disabled={processing}
            aria-label={boekenVerificationLabel(item, ledgerAccounts)}
            sx={{ flexShrink: 0, fontWeight: 600, minHeight: 32 }}
          >
            Boeken
          </Button>
        )}

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
            {/* Errors are surfaced inside the BookingConfirmDialog (via the
                error prop) so the user sees them while still in the action
                they were performing. We intentionally don't render a second
                error here to avoid duplication once the dialog closes. */}

            {/* Document match — the invoice is already in the mail folder, so
                offer to book it with this line instead of asking for an
                upload. Only when the parent can open the document. */}
            {item.category === "invoice" && documentMatch && onOpenDocument && (
              <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1.5 }}>
                <Button
                  variant="contained"
                  size="small"
                  onClick={() => onOpenDocument(documentMatch.fileId)}
                  disabled={processing || processed}
                  aria-label={`Factuur ${documentMatch.title} koppelen aan deze betaling en boeken`}
                  sx={{ fontWeight: 600 }}
                >
                  Factuur koppelen en boeken
                </Button>
                <Typography variant="caption" color="text.secondary">
                  {documentMatch.title}
                </Typography>
              </Box>
            )}

            {/* Invoice upload zone — for "invoice" category, delegates to parent review dialog */}
            {item.category === "invoice" && onInvoiceUpload && !(documentMatch && onOpenDocument) && (
              <Box
                onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
                onDragLeave={() => setDragOver(false)}
                onDrop={handleDrop}
                onClick={() => fileInputRef.current?.click()}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); fileInputRef.current?.click(); } }}
                role="button"
                tabIndex={0}
                aria-label="Upload een factuur (PDF, JPG of PNG)"
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
                  Sleep een PDF, JPG of PNG hierheen of klik om te uploaden
                </Typography>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept={INVOICE_FILE_ACCEPT}
                  onChange={handleFileInput}
                  style={{ display: "none" }}
                  aria-hidden="true"
                  tabIndex={-1}
                />
              </Box>
            )}

            {/* An incoming line may pay an open sales invoice in parts:
                hand it to the group dialog. */}
            {item.bedrag > 0 && onLinkToInvoice && (
              <Box sx={{ mb: 2 }}>
                <Button
                  variant="text"
                  size="small"
                  onClick={() => onLinkToInvoice(item)}
                  disabled={processing || processed}
                  sx={{ fontWeight: 600, px: 0 }}
                >
                  Hoort bij een openstaande factuur
                </Button>
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

            <Box sx={{ display: "flex", justifyContent: "flex-end", gap: 1.5, flexWrap: "wrap" }}>
              {canSplit && (
                <Button
                  size="small"
                  onClick={() => setSplitOpen(true)}
                  disabled={processing}
                  aria-label={`Splitsen: afschriftregel van ${formatMoney(Math.abs(item.bedrag))} over meerdere regels`}
                >
                  Splitsen…
                </Button>
              )}
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
        vatCodes={vatCodes}
        onConfirm={handleProcess}
        processing={processing}
        error={error}
      />
      {/* Mounted on demand so each opening starts from the row's current
          ledger and BTW code. */}
      {splitOpen && (
        <BankLineSplitDialog
          open
          onClose={() => setSplitOpen(false)}
          onExited={() => headerRef.current?.focus()}
          item={previewItem}
          relatieId={relatie?.id}
          soortLabel={SOORT_OPTIONS.find((o) => o.value === soort)?.label ?? soort}
          ledgerAccounts={ledgerAccounts}
          vatCodes={vatCodes}
          onProcessed={onProcessed}
        />
      )}
    </Box>
  );
}
