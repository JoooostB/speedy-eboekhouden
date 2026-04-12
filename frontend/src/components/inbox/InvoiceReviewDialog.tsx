import { useState, useCallback } from "react";
import {
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  LinearProgress,
  Link,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
  Alert,
  AlertTitle,
} from "@mui/material";
import { api } from "../../api/client";
import { track } from "../../analytics";
import { RelationPicker } from "../shared/RelationPicker";
import { LedgerAccountPicker } from "../shared/LedgerAccountPicker";
import { VATCodePicker } from "../shared/VATCodePicker";
import type {
  InvoiceAnalyzeResponse,
  LedgerAccount,
  Relation,
  VATCode,
} from "../../api/types";

/** Mutable state per invoice in the review list */
interface InvoiceEdit {
  /** Original analyze response — holds uploadKey (R2 reference) and filename */
  source: InvoiceAnalyzeResponse;
  /** Editable fields */
  leverancier: string;
  factuurnummer: string;
  datum: string;
  bedragExcl: string;
  bedragIncl: string;
  btwBedrag: string;
  btwCode: string;
  omschrijving: string;
  /** Selected relation (pre-filled from matchedRelation if available) */
  relation: Relation | null;
  /** Selected ledger account (pre-filled from grootboekcode) */
  ledgerAccount: LedgerAccount | null;
  /** Linked bank statement line ID (marks it as processed on submit) */
  importId: number;
  /** Bonnetje mode: book as "Geld uitgegeven" without a relation. */
  isReceipt: boolean;
}

interface SubmitResult {
  filename: string;
  status: "ok" | "error";
  error?: string;
  details?: string;
}

interface Props {
  open: boolean;
  onClose: () => void;
  analyzed: InvoiceAnalyzeResponse[];
  ledgerAccounts: LedgerAccount[];
  vatCodes: VATCode[];
  /** Called after dialog completes so the inbox can refresh */
  onComplete: () => void;
}

/**
 * InvoiceReviewDialog — multi-invoice review before booking.
 *
 * After the user selects PDFs and they are analyzed by AI, this dialog
 * shows all extracted data for review. Each invoice is an editable card.
 * The user can adjust any field, pick a different relation or ledger
 * account, then submit all at once.
 *
 * Accessibility:
 * - Dialog uses aria-labelledby pointing to the title
 * - Each invoice card is a <fieldset> with <legend> for screen readers
 * - Confidence is conveyed with both color and text (not color-alone)
 * - Progress bar uses aria-valuenow / aria-label
 * - Results are announced via aria-live region
 * - All interactive elements are keyboard accessible (native MUI)
 */
export function InvoiceReviewDialog({
  open,
  onClose,
  analyzed,
  ledgerAccounts,
  vatCodes,
  onComplete,
}: Props) {
  // Build editable state from analyzed responses
  const [invoices, setInvoices] = useState<InvoiceEdit[]>(() =>
    analyzed.map((a) => buildEdit(a, ledgerAccounts)),
  );
  const [submitting, setSubmitting] = useState(false);
  const [progress, setProgress] = useState(0);
  const [results, setResults] = useState<SubmitResult[] | null>(null);

  /** Update a single invoice field, keeping the bedrag/btw fields internally
   *  consistent. The invariant is: bedragExcl + btwBedrag = bedragIncl. When
   *  the BTW code is GEEN we force btwBedrag to 0 and bedragExcl to match
   *  bedragIncl, because non-deductible taxes (assurantiebelasting, bank fees,
   *  payroll taxes) are part of the cost and must not be split out.
   *
   *  When btwBedrag changes, we update bedragExcl = bedragIncl - btwBedrag.
   *  When bedragIncl changes, we update bedragExcl from the current btw.
   *  When the user explicitly edits bedragExcl, we leave btw and incl alone
   *  (escape hatch for power users). */
  const updateField = useCallback(
    <K extends keyof InvoiceEdit>(index: number, field: K, value: InvoiceEdit[K]) => {
      setInvoices((prev) => {
        const next = [...prev];
        const updated: InvoiceEdit = { ...next[index], [field]: value };

        // Reconcile bedragen depending on which field changed.
        if (field === "btwCode" && value === "GEEN") {
          // No-BTW: zero out btw and force excl to equal incl.
          updated.btwBedrag = "0.00";
          updated.bedragExcl = updated.bedragIncl;
        } else if (field === "btwBedrag") {
          const incl = parseFloat(updated.bedragIncl) || 0;
          const btw = parseFloat(updated.btwBedrag) || 0;
          updated.bedragExcl = (incl - btw).toFixed(2);
        } else if (field === "bedragIncl") {
          const incl = parseFloat(updated.bedragIncl) || 0;
          const btw = parseFloat(updated.btwBedrag) || 0;
          // If the row is currently no-BTW, keep excl=incl. Otherwise derive
          // excl from the new incl minus the existing btw (the user can
          // re-enter btw afterwards if needed).
          if (updated.btwCode === "GEEN" || btw === 0) {
            updated.bedragExcl = updated.bedragIncl;
            updated.btwBedrag = "0.00";
          } else {
            updated.bedragExcl = (incl - btw).toFixed(2);
          }
        }

        next[index] = updated;
        return next;
      });
    },
    [],
  );

  /** Submit all invoices sequentially */
  const handleSubmitAll = useCallback(async () => {
    setSubmitting(true);
    setProgress(0);
    setResults(null);

    const submitResults: SubmitResult[] = [];

    for (let i = 0; i < invoices.length; i++) {
      const inv = invoices[i];
      setProgress(i);

      try {
        if (!inv.ledgerAccount) {
          throw new Error("Geen grootboekrekening geselecteerd");
        }

        if (inv.isReceipt) {
          // Bonnetje flow — no relation, "Geld uitgegeven" mutation.
          if (!inv.importId) {
            throw new Error("Koppel een afschriftregel om het bonnetje te boeken");
          }
          await api.submitReceipt({
            datum: inv.datum,
            leverancier: inv.leverancier,
            omschrijving: inv.omschrijving,
            bedragExcl: parseFloat(inv.bedragExcl) || 0,
            bedragIncl: parseFloat(inv.bedragIncl) || 0,
            btwBedrag: parseFloat(inv.btwBedrag) || 0,
            btwCode: inv.btwCode,
            tegenRekeningId: inv.ledgerAccount.id,
            uploadKey: inv.source.uploadKey,
            filename: inv.source.filename,
            importId: inv.importId,
          });
        } else {
          if (!inv.relation) {
            throw new Error("Geen relatie geselecteerd");
          }
          await api.submitInvoiceFull({
            datum: inv.datum,
            leverancier: inv.leverancier,
            factuurnummer: inv.factuurnummer,
            omschrijving: inv.omschrijving,
            bedragExcl: parseFloat(inv.bedragExcl) || 0,
            bedragIncl: parseFloat(inv.bedragIncl) || 0,
            btwBedrag: parseFloat(inv.btwBedrag) || 0,
            btwCode: inv.btwCode,
            inEx: "EX",
            relatieId: inv.relation.id,
            tegenRekeningId: inv.ledgerAccount.id,
            rekeningId: inv.source.crediteurenId || 0,
            uploadKey: inv.source.uploadKey,
            filename: inv.source.filename,
            ...(inv.importId ? { importId: inv.importId } : {}),
          });
        }

        submitResults.push({
          filename: inv.source.filename,
          status: "ok",
          details: inv.isReceipt
            ? `Bonnetje: ${inv.leverancier || "onbekend"} — ${formatEuro(inv.bedragIncl)}`
            : `${inv.leverancier} — ${formatEuro(inv.bedragIncl)} — ${inv.factuurnummer}`,
        });

        track("Invoice Review Submitted", {
          confidence: String(Math.round(inv.source.invoice.confidence * 100)),
          mode: inv.isReceipt ? "receipt" : "invoice",
        });
      } catch (err: any) {
        submitResults.push({
          filename: inv.source.filename,
          status: "error",
          error: err.message,
        });
      }
    }

    setProgress(invoices.length);
    setResults(submitResults);
    setSubmitting(false);

    const successCount = submitResults.filter((r) => r.status === "ok").length;
    track("Invoice Review Batch Complete", {
      total: String(invoices.length),
      success: String(successCount),
    });
  }, [invoices]);

  /** Close and trigger refresh if anything was submitted */
  const handleClose = useCallback(() => {
    if (results && results.some((r) => r.status === "ok")) {
      onComplete();
    }
    onClose();
  }, [results, onComplete, onClose]);

  const allDone = results !== null;
  const successCount = results?.filter((r) => r.status === "ok").length ?? 0;
  const errorCount = results?.filter((r) => r.status === "error").length ?? 0;

  return (
    <Dialog
      open={open}
      onClose={submitting ? undefined : handleClose}
      fullWidth
      maxWidth="xl"
      aria-labelledby="invoice-review-title"
      /* Prevent closing during submission */
      disableEscapeKeyDown={submitting}
    >
      <DialogTitle id="invoice-review-title" sx={{ pb: 1 }}>
        <Typography variant="h6" component="span">
          Facturen controleren
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          {invoices.length === 1
            ? "Controleer de gegevens en pas aan waar nodig."
            : `${invoices.length} facturen gevonden. Controleer de gegevens en pas aan waar nodig.`}
        </Typography>
      </DialogTitle>

      {/* Progress bar during submission */}
      {submitting && (
        <LinearProgress
          variant="determinate"
          value={(progress / invoices.length) * 100}
          aria-label={`${progress} van ${invoices.length} facturen verwerkt`}
          aria-valuenow={progress}
          aria-valuemin={0}
          aria-valuemax={invoices.length}
        />
      )}

      <DialogContent dividers sx={{ p: { xs: 2, sm: 3 } }}>
        {/* Results display */}
        {allDone && (
          <Alert
            severity={errorCount === 0 ? "success" : "warning"}
            sx={{ mb: 3 }}
            role="status"
            aria-live="polite"
          >
            <AlertTitle sx={{ fontWeight: 600 }}>
              {successCount} van {invoices.length} facturen geboekt
            </AlertTitle>
            {results!.map((r, i) => (
              <Typography key={i} variant="body2" sx={{ mt: 0.5 }}>
                {r.status === "ok" ? (
                  <Box component="span" sx={{ color: "success.main", fontWeight: 600 }} aria-label="Geslaagd">V</Box>
                ) : (
                  <Box component="span" sx={{ color: "error.main", fontWeight: 600 }} aria-label="Mislukt">X</Box>
                )}{" "}
                {r.filename}
                {r.details ? ` — ${r.details}` : ""}
                {r.error ? ` — ${r.error}` : ""}
              </Typography>
            ))}
          </Alert>
        )}

        {/* Invoice cards */}
        {!allDone && (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
            {invoices.map((inv, index) => (
              <Box
                key={index}
                component="fieldset"
                sx={{
                  border: "1px solid",
                  borderColor:
                    (!inv.ledgerAccount || (!inv.isReceipt && !inv.relation) || (inv.isReceipt && !inv.importId))
                      ? "error.main"
                      : "divider",
                  borderRadius: 2,
                  p: { xs: 2, sm: 3 },
                  m: 0,
                }}
              >
                {/* Legend: filename + confidence badge */}
                <Typography
                  component="legend"
                  sx={{
                    px: 1,
                    fontWeight: 600,
                    fontSize: "0.875rem",
                    display: "flex",
                    alignItems: "center",
                    gap: 1,
                    flexWrap: "wrap",
                  }}
                >
                  {inv.source.filename}
                  <ConfidenceBadge confidence={inv.source.invoice.confidence} />
                </Typography>

                {/* Bonnetje / Factuur mode toggle — a real ToggleButtonGroup
                    rather than a Chip. ToggleButtonGroup gives proper
                    aria-pressed semantics, keyboard navigation, and a
                    visually unambiguous selected state. Pre-filled from the
                    AI's isReceipt detection but always overridable. */}
                <Box sx={{ mt: 1.5, display: "flex", alignItems: "center", gap: 2, flexWrap: "wrap" }}>
                  <ToggleButtonGroup
                    value={inv.isReceipt ? "receipt" : "invoice"}
                    exclusive
                    size="small"
                    onChange={(_, val) => {
                      if (val !== null) updateField(index, "isReceipt", val === "receipt");
                    }}
                    disabled={submitting}
                    aria-label="Type document"
                  >
                    <ToggleButton value="invoice" sx={{ textTransform: "none", fontWeight: 600 }}>
                      Factuur
                    </ToggleButton>
                    <ToggleButton value="receipt" sx={{ textTransform: "none", fontWeight: 600 }}>
                      Bonnetje
                    </ToggleButton>
                  </ToggleButtonGroup>
                  {inv.isReceipt && inv.source.invoice.receiptReason && (
                    <Typography variant="caption" color="text.secondary">
                      Reden: {inv.source.invoice.receiptReason}
                    </Typography>
                  )}
                </Box>

                {/*
                  Side-by-side layout: PDF preview (left) + form fields (right).
                  On mobile (xs): stacks vertically with PDF on top.
                  On desktop (md+): two equal columns.
                */}
                <Box
                  sx={{
                    display: "grid",
                    gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
                    gap: { xs: 2, md: 3 },
                    mt: 2,
                  }}
                >
                  {/* PDF preview pane */}
                  <PdfPreview
                    pdfUrl={inv.source.pdfUrl}
                    filename={inv.source.filename}
                  />

                  {/* Form fields pane */}
                  <Box>
                    {/* Row 1: Leverancier + Relatie (relatie verbergen voor bonnetjes) */}
                    <Box
                      sx={{
                        display: "grid",
                        gridTemplateColumns: { xs: "1fr", sm: inv.isReceipt ? "1fr" : "1fr 1fr" },
                        gap: 2,
                      }}
                    >
                      <TextField
                        label={inv.isReceipt ? "Leverancier (alleen omschrijving)" : "Leverancier"}
                        value={inv.leverancier}
                        onChange={(e) => updateField(index, "leverancier", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        helperText={inv.isReceipt ? "Wordt alleen gebruikt in de omschrijving — geen relatie aangemaakt." : undefined}
                      />
                      {!inv.isReceipt && (
                        <RelationPicker
                          value={inv.relation}
                          onChange={(r) => updateField(index, "relation", r)}
                          label="Relatie (crediteur)"
                          disabled={submitting}
                          grootboekrekeningId={inv.source.crediteurenId || 0}
                        />
                      )}
                    </Box>

                    {/* Row 2: Factuurnummer (factuur only) + Datum */}
                    <Box
                      sx={{
                        display: "grid",
                        gridTemplateColumns: { xs: "1fr", sm: inv.isReceipt ? "1fr" : "1fr 1fr" },
                        gap: 2,
                        mt: 2,
                      }}
                    >
                      {!inv.isReceipt && (
                        <TextField
                          label="Factuurnummer"
                          value={inv.factuurnummer}
                          onChange={(e) => updateField(index, "factuurnummer", e.target.value)}
                          size="small"
                          fullWidth
                          disabled={submitting}
                        />
                      )}
                      <TextField
                        label="Datum"
                        type="date"
                        value={inv.datum}
                        onChange={(e) => updateField(index, "datum", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        slotProps={{ inputLabel: { shrink: true } }}
                      />
                    </Box>

                    {/* Row 3: Bedragen */}
                    <Box
                      sx={{
                        display: "grid",
                        gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr 1fr" },
                        gap: 2,
                        mt: 2,
                      }}
                    >
                      <TextField
                        label="Bedrag excl. BTW"
                        value={inv.bedragExcl}
                        onChange={(e) => updateField(index, "bedragExcl", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        slotProps={{
                          input: {
                            startAdornment: (
                              <Typography variant="body2" color="text.secondary" sx={{ mr: 0.5 }}>
                                &euro;
                              </Typography>
                            ),
                          },
                        }}
                      />
                      <TextField
                        label="BTW-bedrag"
                        value={inv.btwBedrag}
                        onChange={(e) => updateField(index, "btwBedrag", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        slotProps={{
                          input: {
                            startAdornment: (
                              <Typography variant="body2" color="text.secondary" sx={{ mr: 0.5 }}>
                                &euro;
                              </Typography>
                            ),
                          },
                        }}
                      />
                      <TextField
                        label="Bedrag incl. BTW"
                        value={inv.bedragIncl}
                        onChange={(e) => updateField(index, "bedragIncl", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        slotProps={{
                          input: {
                            startAdornment: (
                              <Typography variant="body2" color="text.secondary" sx={{ mr: 0.5 }}>
                                &euro;
                              </Typography>
                            ),
                          },
                        }}
                      />
                    </Box>

                    {/* Row 4: BTW-code + Grootboekrekening */}
                    <Box
                      sx={{
                        display: "grid",
                        gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
                        gap: 2,
                        mt: 2,
                      }}
                    >
                      <VATCodePicker
                        codes={vatCodes}
                        value={inv.btwCode}
                        onChange={(code) => updateField(index, "btwCode", code)}
                        disabled={submitting}
                      />
                      <LedgerAccountPicker
                        accounts={ledgerAccounts}
                        value={inv.ledgerAccount}
                        onChange={(acc) => updateField(index, "ledgerAccount", acc)}
                        label="Tegenrekening"
                        disabled={submitting}
                      />
                    </Box>

                    {/* AI reasoning for grootboekrekening */}
                    {inv.source.invoice.redenering && (
                      <Box sx={{ mt: 1, display: "flex", alignItems: "center", gap: 1 }}>
                        <Box
                          component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                          strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
                          sx={{ width: 16, height: 16, color: "text.secondary", flexShrink: 0 }}
                        >
                          <circle cx="12" cy="12" r="10" />
                          <path d="M12 16v-4" /><path d="M12 8h.01" />
                        </Box>
                        <Typography variant="caption" color="text.secondary" sx={{ fontStyle: "italic" }}>
                          {inv.source.invoice.redenering}
                        </Typography>
                      </Box>
                    )}

                    {/* Reverse charge warning — prominent */}
                    {inv.source.invoice.isReverseCharge && (
                      <Alert severity="warning" sx={{ mt: 2 }}>
                        <Typography variant="body2" fontWeight={600} gutterBottom>
                          Verlegde BTW (reverse charge)
                        </Typography>
                        <Typography variant="body2">
                          Dit is een buitenlandse factuur zonder Nederlandse BTW. De BTW wordt automatisch
                          verlegd via BTW-code <strong>{inv.btwCode}</strong>. Controleer of deze correct is
                          — de BTW moet je zelf aangeven in je BTW-aangifte.
                        </Typography>
                      </Alert>
                    )}

                    {/* Belastingadvies from Claude */}
                    {inv.source.invoice.belastingAdvies?.length > 0 && (
                      <Alert severity="info" sx={{ mt: 2 }} icon={false}>
                        <Typography variant="body2" fontWeight={600} gutterBottom>
                          Belastingadvies
                        </Typography>
                        {inv.source.invoice.belastingAdvies.map((tip, i) => (
                          <Typography key={i} variant="body2" sx={{ mt: 0.5, display: "flex", alignItems: "flex-start", gap: 0.5 }}>
                            <Box
                              component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                              strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
                              sx={{ width: 14, height: 14, color: "info.main", flexShrink: 0, mt: 0.3 }}
                            >
                              <circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path d="M12 8h.01" />
                            </Box>
                            {tip.tekst}
                          </Typography>
                        ))}
                      </Alert>
                    )}

                    {/* Matched bank line */}
                    {inv.source.matchedBankLine && (
                      <Alert
                        severity="success"
                        sx={{ mt: 2 }}
                        action={
                          inv.importId ? (
                            <Button
                              color="inherit"
                              size="small"
                              onClick={() => updateField(index, "importId", 0)}
                            >
                              Loskoppelen
                            </Button>
                          ) : (
                            <Button
                              color="inherit"
                              size="small"
                              onClick={() => updateField(index, "importId", inv.source.matchedBankLine!.id)}
                            >
                              Koppelen
                            </Button>
                          )
                        }
                      >
                        <Typography variant="body2" fontWeight={600}>
                          {inv.importId ? "Gekoppeld aan afschriftregel:" : "Gevonden afschriftregel:"}
                        </Typography>
                        <Typography variant="body2">
                          {new Date(inv.source.matchedBankLine.datum).toLocaleDateString("nl-NL")} — {new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(inv.source.matchedBankLine.bedrag)} — {inv.source.matchedBankLine.omschrijving.slice(0, 80)}
                        </Typography>
                      </Alert>
                    )}

                    {/* Row 5: Omschrijving */}
                    <Box sx={{ mt: 2 }}>
                      <TextField
                        label="Omschrijving"
                        value={inv.omschrijving}
                        onChange={(e) => updateField(index, "omschrijving", e.target.value)}
                        size="small"
                        fullWidth
                        multiline
                        minRows={2}
                        disabled={submitting}
                      />
                    </Box>
                  </Box>
                </Box>

                {/* Separator between cards (except last) */}
                {index < invoices.length - 1 && <Divider sx={{ mt: 2 }} />}
              </Box>
            ))}
          </Box>
        )}
      </DialogContent>

      <DialogActions sx={{ px: 3, py: 2 }}>
        {allDone ? (
          <Button onClick={handleClose} variant="contained">
            Sluiten
          </Button>
        ) : (
          <>
            {invoices.some((inv) => !inv.ledgerAccount || (!inv.isReceipt && !inv.relation) || (inv.isReceipt && !inv.importId)) && !submitting && (
              <Typography variant="body2" color="error" sx={{ mr: "auto", pl: 1 }}>
                {(() => {
                  const missingRel = invoices.filter((inv) => !inv.isReceipt && !inv.relation).length;
                  const missingLedger = invoices.filter((inv) => !inv.ledgerAccount).length;
                  const missingBank = invoices.filter((inv) => inv.isReceipt && !inv.importId).length;
                  const parts = [];
                  if (missingRel) parts.push(`${missingRel} zonder relatie`);
                  if (missingLedger) parts.push(`${missingLedger} zonder tegenrekening`);
                  if (missingBank) parts.push(`${missingBank} bonnetje${missingBank > 1 ? "s" : ""} zonder afschriftregel`);
                  return `Kan niet boeken: ${parts.join(", ")}`;
                })()}
              </Typography>
            )}
            <Button onClick={handleClose} disabled={submitting}>
              Annuleren
            </Button>
            <Button
              variant="contained"
              onClick={handleSubmitAll}
              disabled={
                submitting ||
                invoices.some(
                  (inv) =>
                    !inv.ledgerAccount ||
                    (!inv.isReceipt && !inv.relation) ||
                    (inv.isReceipt && !inv.importId),
                )
              }
              startIcon={
                submitting ? (
                  /* Spinner icon — inline SVG spinning via CSS */
                  <Box
                    component="svg"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                    sx={{
                      width: 16,
                      height: 16,
                      animation: "spin 1s linear infinite",
                      "@keyframes spin": {
                        from: { transform: "rotate(0deg)" },
                        to: { transform: "rotate(360deg)" },
                      },
                    }}
                  >
                    <path d="M21 12a9 9 0 1 1-6.219-8.56" />
                  </Box>
                ) : (
                  /* Checkmark icon */
                  <Box
                    component="svg"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                    sx={{ width: 16, height: 16 }}
                  >
                    <polyline points="20 6 9 17 4 12" />
                  </Box>
                )
              }
            >
              {submitting
                ? `Verwerken (${progress}/${invoices.length})...`
                : invoices.length === 1
                  ? "Boeken"
                  : `Alles boeken (${invoices.length})`}
            </Button>
          </>
        )}
      </DialogActions>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build editable state from an analyze response */
function buildEdit(a: InvoiceAnalyzeResponse, ledgerAccounts: LedgerAccount[]): InvoiceEdit {
  const inv = a.invoice;

  // Try to find ledger account by grootboekcode
  const matchedLedger = inv.grootboekcode
    ? ledgerAccounts.find(
        (la) => la.code === inv.grootboekcode || la.omschrijving.toLowerCase() === inv.grootboekcode.toLowerCase(),
      ) ?? null
    : null;

  // Build relation from matchedRelation if present
  const matchedRelation: Relation | null = a.matchedRelation
    ? {
        id: a.matchedRelation.id,
        code: a.matchedRelation.code,
        bedrijf: a.matchedRelation.bedrijf,
        grootboekrekeningId: 0,
        iban: "",
      }
    : null;

  // Normalize the bedragen on load. If the AI returned btwCode=GEEN with a
  // non-zero btwBedrag (or with bedragExcl < bedragIncl), force excl to
  // equal incl and zero the btw — non-deductible taxes are part of the
  // cost and must not be split out, regardless of what the PDF showed.
  const btwCode = inv.btwCode || "HOOG_INK_21";
  let bedragIncl = inv.bedragInclBtw ?? 0;
  let bedragExcl = inv.bedragExclBtw ?? 0;
  let btwBedrag = inv.btwBedrag ?? 0;
  if (btwCode === "GEEN") {
    bedragExcl = bedragIncl;
    btwBedrag = 0;
  } else if (Math.abs(bedragExcl + btwBedrag - bedragIncl) > 0.01) {
    // Numbers don't add up — trust incl + btw, derive excl.
    bedragExcl = bedragIncl - btwBedrag;
  }

  return {
    source: a,
    leverancier: inv.leverancier ?? "",
    factuurnummer: inv.factuurnummer ?? "",
    datum: inv.datum ?? new Date().toISOString().slice(0, 10),
    bedragExcl: bedragExcl.toFixed(2),
    bedragIncl: bedragIncl.toFixed(2),
    btwBedrag: btwBedrag.toFixed(2),
    btwCode,
    omschrijving: inv.omschrijving ?? "",
    relation: matchedRelation,
    ledgerAccount: matchedLedger,
    importId: a.matchedBankLine?.id ?? 0,
    isReceipt: inv.isReceipt ?? false,
  };
}

/** Format a string amount as euros */
function formatEuro(amount: string): string {
  const num = parseFloat(amount);
  if (isNaN(num)) return amount;
  return `\u20AC${num.toFixed(2)}`;
}

/**
 * PdfPreview — renders an inline PDF viewer with a fallback link.
 *
 * Uses <object type="application/pdf"> which has better cross-browser PDF
 * rendering than <iframe> (Safari, Firefox handle it natively). The fallback
 * content inside <object> is shown when the browser cannot render PDFs inline
 * (e.g. mobile browsers), providing a direct download/open link.
 *
 * Accessibility:
 * - The <object> has aria-label describing its content
 * - The fallback link is keyboard-accessible and clearly labeled
 * - The "open in new tab" link uses aria-describedby to clarify it opens externally
 * - role="document" on the object tells screen readers this is an embedded document
 */
function PdfPreview({ pdfUrl, filename }: { pdfUrl: string; filename: string }) {
  if (!pdfUrl) {
    return (
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          minHeight: { xs: 200, md: 500 },
          bgcolor: "action.hover",
          borderRadius: 1,
          border: "1px solid",
          borderColor: "divider",
        }}
      >
        <Typography variant="body2" color="text.secondary">
          Geen voorbeeld beschikbaar
        </Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Box
        component="object"
        data={pdfUrl}
        type="application/pdf"
        role="document"
        aria-label={`PDF-voorbeeld van ${filename}`}
        sx={{
          width: "100%",
          minHeight: { xs: 300, md: 500 },
          border: "1px solid",
          borderColor: "divider",
          borderRadius: 1,
          bgcolor: "grey.50",
        }}
      >
        {/*
          Fallback content: shown when the browser cannot render PDFs inline.
          This is not a duplicate — it only renders when <object> fails.
        */}
        <Box
          sx={{
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            minHeight: { xs: 200, md: 400 },
            p: 3,
            textAlign: "center",
          }}
        >
          {/* Document icon — inline SVG, stroke style */}
          <Box
            component="svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
            sx={{ width: 48, height: 48, color: "text.secondary", mb: 2 }}
          >
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
            <polyline points="14 2 14 8 20 8" />
            <line x1="16" y1="13" x2="8" y2="13" />
            <line x1="16" y1="17" x2="8" y2="17" />
            <polyline points="10 9 9 9 8 9" />
          </Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            Je browser kan dit PDF-bestand niet inline weergeven.
          </Typography>
          <Link
            href={pdfUrl}
            target="_blank"
            rel="noopener noreferrer"
            sx={{ fontWeight: 600, fontSize: "0.875rem" }}
          >
            PDF openen in nieuw tabblad
          </Link>
        </Box>
      </Box>

      {/* "Open in new tab" link — always visible below the preview */}
      <Link
        href={pdfUrl}
        target="_blank"
        rel="noopener noreferrer"
        sx={{
          display: "inline-flex",
          alignItems: "center",
          gap: 0.5,
          fontSize: "0.8125rem",
          color: "text.secondary",
          textDecorationColor: "currentcolor",
          "&:hover": { color: "primary.main" },
        }}
      >
        {/* External link icon — inline SVG */}
        <Box
          component="svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
          sx={{ width: 14, height: 14 }}
        >
          <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
          <polyline points="15 3 21 3 21 9" />
          <line x1="10" y1="14" x2="21" y2="3" />
        </Box>
        Openen in nieuw tabblad
      </Link>
    </Box>
  );
}

/**
 * ConfidenceBadge — shows AI confidence as a colored chip.
 * Uses both color AND text to convey confidence level (WCAG: no color-alone).
 */
function ConfidenceBadge({ confidence }: { confidence: number }) {
  const pct = Math.round(confidence * 100);
  let color: string;
  let bgcolor: string;
  let label: string;

  if (pct >= 80) {
    color = "#166534";
    bgcolor = "rgba(22, 163, 74, 0.1)";
    label = `${pct}% — hoog vertrouwen`;
  } else if (pct >= 50) {
    color = "#92400e";
    bgcolor = "rgba(245, 158, 11, 0.1)";
    label = `${pct}% — matig vertrouwen`;
  } else {
    color = "#991b1b";
    bgcolor = "rgba(220, 38, 38, 0.1)";
    label = `${pct}% — laag vertrouwen`;
  }

  return (
    <Chip
      label={label}
      size="small"
      sx={{
        fontWeight: 600,
        fontSize: "0.7rem",
        color,
        bgcolor,
        height: 22,
      }}
    />
  );
}
