import { useState, useCallback, useMemo, useEffect } from "react";
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
  MenuItem,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
  Alert,
  AlertTitle,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import { api } from "../../api/client";
import { track } from "../../analytics";
import { RelationPicker } from "../shared/RelationPicker";
import { LedgerAccountPicker } from "../shared/LedgerAccountPicker";
import { VATCodePicker } from "../shared/VATCodePicker";
import { ArchiveFolderPicker } from "../shared/ArchiveFolderPicker";
import {
  findArchiveFolderForSupplier,
  useArchiveFolders,
  type ArchiveFolderWithPath,
} from "../../hooks/useArchiveFolders";
import { dedupePurchaseVatCodes, isReverseChargeCode } from "./vatCodeFilters";
import { invoiceBlocker as evaluateInvoiceBlocker } from "./invoiceValidation";
import { buildFullSubmitPayload, buildReceiptSubmitPayload, isArchiveSource } from "./invoiceSubmitPayload";
import { RegelsEditor } from "../shared/RegelsEditor";
import { proposeRegels, undoProposal, type SingleLineValues, type SplitProposal } from "./proposeRegels";
import {
  regelsTotals,
  splitFromSingle,
  splitWithTip,
  toCents,
  toRegelPayload,
  validateRegels,
  type RegelEdit,
} from "./regels";
import {
  NO_MATCH_ALERT_TEXT,
  archiveFolderHelperText,
  archiveResultLine,
  bankLineChoices,
  bankLineOptionLabel,
  isOpenBooking,
  previewKind,
  submitButtonLabel,
} from "./mailDocumentReview";
import { isBooked, submitOutcome } from "./submitOutcome";
import { initialLedger, ledgerAfterRelationChange, type LedgerSource } from "./invoiceLedger";
import { AMOUNT_HINT, formatAmountInput, formatFormAmount, fxInvoiceNote, invalidAmountField, parseAmount } from "./reviewAmounts";
import { formatMoney } from "../../hooks/mailDocumentsModel";
import type {
  AnalyzedInvoice,
  InvoiceSubmitFullResponse,
  InvoiceSubmitReceiptResponse,
  LedgerAccount,
  MatchedBankLine,
  ReceiptExtraLine,
  Relation,
  SubmitArchiveOutcome,
  VATCode,
} from "../../api/types";

/** Mutable state per invoice in the review list */
interface InvoiceEdit {
  /** Original analyze response — holds uploadKey (R2 reference) and filename */
  source: AnalyzedInvoice;
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
  /** Selected ledger account (pre-filled from the relation's default,
   *  else from Claude's grootboekcode) */
  ledgerAccount: LedgerAccount | null;
  /** Where ledgerAccount came from; drives the caption under the picker
   *  and whether a relation change may replace it. */
  ledgerSource: LedgerSource;
  /** Linked bank statement line ID (marks it as processed on submit) */
  importId: number;
  /** The bank line shown as the payment: the analysis' match, or the
   *  candidate the user picked when several fit (mail-folder documents).
   *  importId follows it; a user who unlinks keeps it on screen behind a
   *  "Koppelen" button. */
  bankLine: MatchedBankLine | null;
  /** Bonnetje mode: book as "Geld uitgegeven" without a relation. */
  isReceipt: boolean;
  /** The lines of a split booking ("Splitsen over meerdere regels"), or
   *  null for a single-line booking. In split mode ledgerAccount and
   *  btwCode mirror line 1, so the relation's default ledger and the
   *  captions keep working; the amounts come from the lines. */
  regels: RegelEdit[] | null;
  /** Claude's split proposal the dialog opened with, until the user undoes
   *  it or unsplits; null when the card was never proposed split. */
  proposal: SplitProposal | null;
  /** True once the user changed the proposed lines: "Ongedaan maken" then
   *  makes way for "Niet splitsen", so an edit is never thrown away by one
   *  click, and analytics can tell a booked proposal from an edited one. */
  proposalEdited: boolean;
  /** The single-line values of the analysis, kept for "Ongedaan maken". */
  original: SingleLineValues | null;
  /** Picked archive folder; null means do not upload this PDF to the
   *  digitaal archief. */
  archiveFolder: ArchiveFolderWithPath | null;
}

/** The amount a split booking's lines must add up to, in cents: the bank
 *  line for a receipt booked against one (the backend checks the same
 *  amount), otherwise the total the user typed. */
function splitTotalCents(inv: Pick<InvoiceEdit, "isReceipt" | "importId" | "bankLine" | "bedragIncl">): number {
  if (splitTotalFromBankLine(inv)) return toCents(Math.abs(inv.bankLine!.bedrag));
  return toCents(parseAmount(inv.bedragIncl) ?? 0);
}

function splitTotalFromBankLine(inv: Pick<InvoiceEdit, "isReceipt" | "importId" | "bankLine">): boolean {
  return inv.isReceipt && inv.importId !== 0 && inv.bankLine !== null;
}

/** A single-line booking again, keeping line 1's tegenrekening and BTW
 *  code; the total stays and its BTW is recomputed for that code. */
function unsplit(inv: InvoiceEdit, first: RegelEdit | undefined, vatRate: (code: string) => number): InvoiceEdit {
  const updated: InvoiceEdit = { ...inv, regels: null, proposal: null };
  if (first && first.ledgerAccount?.id !== inv.ledgerAccount?.id) {
    // Line 1 as the user left it, also when they cleared its account.
    updated.ledgerAccount = first.ledgerAccount;
    updated.ledgerSource = first.ledgerAccount ? "manual" : null;
  }
  if (first) updated.btwCode = first.btwCode;
  const incl = parseAmount(updated.bedragIncl);
  const rate = vatRate(updated.btwCode);
  if (incl !== null) {
    const btw = rate === 0 ? 0 : +((incl * rate) / (100 + rate)).toFixed(2);
    updated.btwBedrag = formatAmountInput(btw);
    updated.bedragExcl = formatAmountInput(incl - btw);
  }
  return updated;
}

interface SubmitResult {
  filename: string;
  status: "ok" | "error" | "skipped" | "warning";
  error?: string;
  details?: string;
}

/** Adapter — convert the dialog's editable shape into the validation
 *  helper's input. The helper itself lives in invoiceValidation.ts so it
 *  can be unit-tested without dragging the whole InvoiceEdit / Relation /
 *  LedgerAccount type web into a test file. */
function invoiceBlocker(inv: InvoiceEdit): string | null {
  return evaluateInvoiceBlocker({
    // A split booking carries its tegenrekeningen per line (splitBlocker).
    hasLedgerAccount: inv.regels !== null || inv.ledgerAccount !== null,
    hasRelation: inv.relation !== null,
    hasImportId: inv.importId !== 0,
    isReceipt: inv.isReceipt,
    isCreditnota: inv.source.invoice.documentType === "creditnota",
    amountsValid: amountsValid(inv),
    splitBlocker: splitBlocker(inv),
  });
}

/** Why the lines of a split booking cannot be booked yet; null for a
 *  single-line booking or when they add up. */
function splitBlocker(inv: InvoiceEdit): string | null {
  return inv.regels === null ? null : validateRegels(inv.regels, splitTotalCents(inv));
}

/** Whether every amount of a row reads as an amount and the total is above
 *  zero. For a receipt split against its bank line the total is the bank
 *  line's, so only the lines count (splitBlocker). */
function amountsValid(inv: InvoiceEdit): boolean {
  if (inv.regels !== null && splitTotalFromBankLine(inv)) return true;
  const incl = parseAmount(inv.bedragIncl);
  return incl !== null && incl > 0 && parseAmount(inv.bedragExcl) !== null && parseAmount(inv.btwBedrag) !== null;
}

interface Props {
  open: boolean;
  onClose: () => void;
  analyzed: AnalyzedInvoice[];
  ledgerAccounts: LedgerAccount[];
  vatCodes: VATCode[];
  /** Called after dialog completes so the inbox can refresh */
  onComplete: () => void;
  /** Folders that are never suggested or, for a mail-folder document,
   *  offered as a target, together with everything nested under them: the
   *  Mail & herken folder. A file archived there would still be in the
   *  inbox. */
  excludeFolderIds?: readonly number[];
}

const NO_FOLDERS: readonly number[] = [];

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
 *
 * Two document sources share the dialog. An upload (the "Facturen uploaden"
 * button or a bank line's upload zone) is sent inline with the booking when
 * a folder is picked. A Mail & herken document (source.archiveFileId) is
 * already in e-boekhouden: the booking sends its id and the folder to move
 * it to ("Verplaatsen naar"; none means Facturen/<jaar>/<maand>), and the
 * backend links, renames and moves it. For such a document the card is
 * headed by its title, several candidate bank lines can be chosen from, an
 * invoice without a bank line is booked as an open invoice, and on phones
 * the dialog is full screen.
 */
export function InvoiceReviewDialog({
  open,
  onClose,
  analyzed,
  ledgerAccounts,
  vatCodes,
  onComplete,
  excludeFolderIds = NO_FOLDERS,
}: Props) {
  // Build editable state from analyzed responses
  const [invoices, setInvoices] = useState<InvoiceEdit[]>(() =>
    analyzed.map((a) => buildEdit(a, ledgerAccounts, vatCodes)),
  );
  const theme = useTheme();
  const isXs = useMediaQuery(theme.breakpoints.down("sm"));
  const hasMailDocuments = invoices.some((inv) => isArchiveSource(inv.source));

  // Purchase-only VAT codes for the supplier invoice picker. Two layers
  // of filtering:
  //   1. Drop sales (*_VERK_*) codes — a supplier factuur is always a
  //      purchase.
  //   2. Dedupe by display label. e-boekhouden returns multiple inkoop
  //      codes that render identically in the dropdown (legacy / historical
  //      tariff variants like LAAG_INK_6 alongside the current LAAG_INK_9,
  //      and a few duplicate "Btw verlegd" entries). The user can't
  //      distinguish them visually so we keep the first occurrence per
  //      `omschrijving + percentage`. Tested in InvoiceReviewDialog.test.tsx.
  const purchaseVatCodes = useMemo(
    () => dedupePurchaseVatCodes(vatCodes),
    [vatCodes],
  );
  const [submitting, setSubmitting] = useState(false);
  const [progress, setProgress] = useState(0);
  const [results, setResults] = useState<SubmitResult[] | null>(null);

  // Archive folders are fetched once at the dialog level so we can both
  // pass them to each ArchiveFolderPicker AND pre-fill suggestions based on
  // the invoice supplier. The picker still calls useArchiveFolders itself
  // for the empty-state / error handling — the extra fetch is cheap and
  // keeps the picker usable in isolation.
  const { folders: archiveFolders } = useArchiveFolders();

  // When folders arrive, fill in any invoice whose archiveFolder is still
  // null with the best matching folder for its supplier, never one in the
  // mail folder's subtree. We deliberately depend only on the folder list
  // and the exclusions: this runs once per change of those so a user who
  // deselects a suggested folder afterwards isn't re-surprised by the
  // suggestion popping back in on every render.
  useEffect(() => {
    if (archiveFolders.length === 0) return;
    setInvoices((prev) =>
      prev.map((inv) =>
        inv.archiveFolder == null
          ? { ...inv, archiveFolder: findArchiveFolderForSupplier(archiveFolders, inv.leverancier, excludeFolderIds) }
          : inv,
      ),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [archiveFolders, excludeFolderIds]);

  /** Fetch the default ledger account of the relation on invoice `index`
   *  when the search hit or a cached analysis did not carry it, and apply
   *  it unless the user picked the ledger account by hand or another
   *  relation meanwhile. Best effort: on failure the current account stays. */
  const resolveRelationLedger = useCallback(
    (index: number, relationId: number) => {
      api.getRelation(relationId).then(
        (rel) => {
          setInvoices((prev) => {
            const inv = prev[index];
            if (!inv || inv.relation?.id !== relationId) return prev;
            // In split mode the lines keep their own ledgers; only record
            // the relation's default for after "Niet splitsen".
            if (inv.regels) {
              const next = [...prev];
              next[index] = { ...inv, relation: { ...inv.relation, grootboekrekeningId: rel.grootboekrekeningId ?? 0 } };
              return next;
            }
            const ledger = ledgerAfterRelationChange(
              { account: inv.ledgerAccount, source: inv.ledgerSource },
              rel,
              ledgerAccounts,
            );
            const next = [...prev];
            next[index] = {
              ...inv,
              relation: { ...inv.relation, grootboekrekeningId: rel.grootboekrekeningId ?? 0 },
              ledgerAccount: ledger.account,
              ledgerSource: ledger.source,
            };
            return next;
          });
        },
        () => {
          // Without the lookup the current ledger account stays.
        },
      );
    },
    [ledgerAccounts],
  );

  // A matched relation whose default is unknown (analyses cached before
  // the field existed, or a failed lookup at analysis time) is looked up
  // once when the dialog opens, without a new Claude analysis.
  useEffect(() => {
    invoices.forEach((inv, index) => {
      if (inv.relation && inv.source.matchedRelation?.grootboekrekeningId == null) {
        resolveRelationLedger(index, inv.relation.id);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /** Flip every invoice to bonnetje- or factuur-mode in one call. Saves the
   *  user from clicking the per-card toggle N times when uploading a stack
   *  of receipts that should all be booked as bonnetjes. */
  const setAllReceiptMode = useCallback((isReceipt: boolean) => {
    setInvoices((prev) => prev.map((inv) => (inv.isReceipt === isReceipt ? inv : { ...inv, isReceipt })));
  }, []);

  /** Look up a VAT code's percentage from the vatCodes list. Used by
   *  updateField's reconciliation so the "zero-rate" logic works for every
   *  0% code (GEEN, VERL_INK, VERL_INK_L9, BU_EU_INK, BI_EU_INK, …) and
   *  not just for GEEN. Returns 0 when the code isn't found — safe default. */
  const vatRate = useCallback(
    (code: string): number => {
      const match = vatCodes.find((v) => v.code === code);
      return match?.percentage ?? 0;
    },
    [vatCodes],
  );

  /** Update a single invoice field, keeping the bedrag/btw fields internally
   *  consistent. The invariant is: bedragExcl + btwBedrag = bedragIncl. For
   *  any zero-rate BTW code (GEEN or a reverse-charge variant) we force
   *  btwBedrag to 0 and bedragExcl to match bedragIncl, because non-deductible
   *  or shifted taxes are part of the cost and must not be split out. */
  const updateField = useCallback(
    <K extends keyof InvoiceEdit>(index: number, field: K, value: InvoiceEdit[K]) => {
      setInvoices((prev) => {
        const next = [...prev];
        const updated: InvoiceEdit = { ...next[index], [field]: value };

        const rate = vatRate(updated.btwCode);

        if (field === "ledgerAccount") {
          updated.ledgerSource = "manual";
        } else if (field === "relation") {
          const ledger = ledgerAfterRelationChange(
            { account: updated.ledgerAccount, source: updated.ledgerSource },
            updated.relation,
            ledgerAccounts,
          );
          // In split mode the lines keep their own ledgers (a proposal's or
          // the user's); the relation's default only applies to the single
          // line, so it takes effect after "Niet splitsen" or without a split.
          if (!updated.regels) {
            updated.ledgerAccount = ledger.account;
            updated.ledgerSource = ledger.source;
          }
        }

        // Reconcile bedragen depending on which field changed.
        if (field === "btwCode") {
          if (rate === 0) {
            // Zero-rate code (GEEN or reverse charge): force excl to equal
            // incl and zero out the btw amount.
            updated.btwBedrag = formatAmountInput(0);
            updated.bedragExcl = updated.bedragIncl;
          } else {
            // Non-zero rate: recompute btw from incl using the real rate.
            // Text that is not an amount (yet) leaves the other fields alone.
            const incl = parseAmount(updated.bedragIncl);
            if (incl !== null) {
              const btw = +(incl * rate / (100 + rate)).toFixed(2);
              updated.btwBedrag = formatAmountInput(btw);
              updated.bedragExcl = formatAmountInput(incl - btw);
            }
          }
        } else if (field === "btwBedrag") {
          const incl = parseAmount(updated.bedragIncl);
          const btw = parseAmount(updated.btwBedrag);
          if (incl !== null && btw !== null) {
            updated.bedragExcl = formatAmountInput(incl - btw);
          }
        } else if (field === "bedragIncl") {
          const incl = parseAmount(updated.bedragIncl);
          if (rate === 0) {
            updated.bedragExcl = updated.bedragIncl;
            updated.btwBedrag = formatAmountInput(0);
          } else if (incl !== null) {
            const btw = +(incl * rate / (100 + rate)).toFixed(2);
            updated.btwBedrag = formatAmountInput(btw);
            updated.bedragExcl = formatAmountInput(incl - btw);
          }
        }

        next[index] = updated;
        return next;
      });
    },
    [vatRate, ledgerAccounts],
  );

  /** Replace the lines of the booking at `index`. null, or fewer than two
   *  lines, is a single-line booking again (unsplit keeps line 1). A
   *  tegenrekening picked on line 1 is the booking's, so a later relation
   *  change does not overwrite it. */
  const setRegels = useCallback(
    (index: number, regels: RegelEdit[] | null) => {
      setInvoices((prev) => {
        const next = [...prev];
        const inv = next[index];
        if (regels === null || regels.length < 2) {
          next[index] = unsplit(inv, regels?.[0], vatRate);
          return next;
        }
        const updated: InvoiceEdit = { ...inv, regels, proposalEdited: inv.proposalEdited || inv.proposal !== null };
        const first = regels[0].ledgerAccount;
        if (first?.id !== inv.ledgerAccount?.id) {
          // ledgerAccount mirrors line 1, cleared included, so no caption
          // or payload refers to an account the user removed.
          updated.ledgerAccount = first;
          updated.ledgerSource = first ? "manual" : null;
        }
        updated.btwCode = regels[0].btwCode;
        next[index] = updated;
        return next;
      });
    },
    [vatRate],
  );

  /** "Ongedaan maken" on a proposed split: back to one line with the
   *  values the analysis gave, before any edit to the lines. */
  const undoSplitProposal = useCallback((index: number) => {
    setInvoices((prev) => {
      const next = [...prev];
      const inv = next[index];
      if (inv.original) next[index] = undoProposal(inv, inv.original);
      return next;
    });
  }, []);

  /** "Splitsen over meerdere regels": the current values become line 1. */
  const splitInvoice = useCallback((index: number) => {
    setInvoices((prev) => {
      const next = [...prev];
      const inv = next[index];
      next[index] = { ...inv, regels: splitFromSingle(inv) };
      return next;
    });
  }, []);

  /** "Splits in fooi": the receipt as line 1, the difference with the bank
   *  line as a tip without BTW. */
  const splitTip = useCallback((index: number, tipCents: number) => {
    setInvoices((prev) => {
      const next = [...prev];
      const inv = next[index];
      next[index] = { ...inv, regels: splitWithTip(inv, tipCents) };
      return next;
    });
  }, []);

  /** Show another candidate bank line as the payment and link it. */
  const selectBankLine = useCallback((index: number, line: MatchedBankLine | null) => {
    setInvoices((prev) => {
      const next = [...prev];
      next[index] = { ...next[index], bankLine: line, importId: line?.id ?? 0 };
      return next;
    });
  }, []);

  /** Submit all invoices sequentially */
  const handleSubmitAll = useCallback(async () => {
    setSubmitting(true);
    setProgress(0);
    setResults(null);

    const submitResults: SubmitResult[] = [];

    for (let i = 0; i < invoices.length; i++) {
      const inv = invoices[i];
      setProgress(i);

      // A mail-folder document is listed by its title, an upload by its name.
      const label = inv.source.documentTitle ?? inv.source.filename;
      const archiveSource = isArchiveSource(inv.source);
      const openBooking = isOpenBooking({
        archiveFileId: inv.source.archiveFileId,
        isReceipt: inv.isReceipt,
        importId: inv.importId,
      });

      // Skip incomplete rows instead of failing the whole batch. The user
      // gets a "skipped" result entry so they know which ones still need
      // attention; the dialog stays open so they can fix and retry.
      const blocker = invoiceBlocker(inv);
      if (blocker) {
        submitResults.push({
          filename: label,
          status: "skipped",
          error: blocker,
        });
        continue;
      }

      // Encode the original File as base64 once per invoice: the analyser
      // keeps no copy of an upload (nothing goes to R2), so the inline bytes
      // are the only way the backend can archive it. Always sent for an
      // upload; whether it is archived, and where, follows folderId. A
      // mail-folder document is never inlined: the backend already has the
      // file and only needs its id.
      let pdfBase64: string | undefined;
      if (!archiveSource && inv.source.localFile) {
        try {
          pdfBase64 = await fileToBase64(inv.source.localFile);
        } catch (err: any) {
          submitResults.push({
            filename: label,
            status: "error",
            error: `Bestand lezen mislukt: ${err?.message ?? "onbekende fout"}`,
          });
          continue;
        }
      }
      const folderId = inv.archiveFolder?.id ?? 0;

      // The user expected the PDF to be archived if and only if both a folder
      // and a PDF were available. We use this below to detect a silent
      // archive failure (mutation booked but `archived: false`) and surface
      // it as a warning instead of letting the user assume everything is OK.
      const archiveExpected = !!pdfBase64 && !!folderId;

      // A split booking sends its lines and the totals they add up to (the
      // BTW is the sum of the per-line BTW); invoiceBlocker refused the row
      // above unless the lines add up, or, single line, all three parse.
      const totals = inv.regels
        ? regelsTotals(inv.regels, splitTotalCents(inv), vatRate)
        : {
            bedragExcl: parseAmount(inv.bedragExcl) ?? 0,
            bedragIncl: parseAmount(inv.bedragIncl) ?? 0,
            btwBedrag: parseAmount(inv.btwBedrag) ?? 0,
          };
      const payloadInput = {
        source: inv.source,
        datum: inv.datum,
        leverancier: inv.leverancier,
        factuurnummer: inv.factuurnummer,
        omschrijving: inv.omschrijving,
        ...totals,
        btwCode: inv.btwCode,
        relatieId: inv.relation?.id ?? 0,
        tegenRekeningId: (inv.regels ? inv.regels[0].ledgerAccount?.id : inv.ledgerAccount?.id) ?? 0,
        importId: inv.importId,
        folderId,
        pdfBase64,
        extraLines: [] as ReceiptExtraLine[],
        ...(inv.regels ? { regels: toRegelPayload(inv.regels) } : {}),
      };

      // The result line: for a mail-folder document what happened to the
      // file ("Geboekt · verplaatst naar …"), for an upload the booking.
      const receiptDetails = `Bonnetje ${inv.leverancier || "onbekend"} · ${formatFormAmount(inv.bedragIncl)}`;
      const invoiceDetails = `${inv.leverancier} · ${formatFormAmount(inv.bedragIncl)} · ${inv.factuurnummer}`;
      const detailsFor = (resp: SubmitArchiveOutcome) =>
        archiveSource ? archiveResultLine(resp, openBooking) : inv.isReceipt ? receiptDetails : invoiceDetails;
      const confidence = String(Math.round(inv.source.invoice.confidence * 100));

      try {
        // Bonnetje flow — no relation, "Geld uitgegeven" mutation. An
        // invoice is a FactuurOntvangen, plus the payment when a bank line
        // is linked. Both carry `regels` when the user split the booking.
        const resp: InvoiceSubmitFullResponse | InvoiceSubmitReceiptResponse = inv.isReceipt
          ? await api.submitReceipt(buildReceiptSubmitPayload(payloadInput))
          : await api.submitInvoiceFull(buildFullSubmitPayload(payloadInput));
        if (inv.regels) {
          track("booking_split", {
            path: inv.isReceipt ? "bonnetje" : "factuur",
            regels: inv.regels.length,
            proposed: inv.proposal !== null,
            edited: inv.proposalEdited,
            reden: inv.proposal?.advies.reden ?? "none",
          });
        }

        // The booking stands. A payment that failed (the bank line stays
        // open) and a document that could not be archived, linked, renamed
        // or moved are separate problems, so every warning is shown.
        const outcome = submitOutcome({ isReceipt: inv.isReceipt, archiveExpected, resp });
        submitResults.push({
          filename: label,
          status: outcome.status,
          details: detailsFor(resp),
          error: outcome.warnings.length > 0 ? outcome.warnings.join(" ") : undefined,
        });

        const mode = inv.isReceipt ? "receipt" : "invoice";
        track(
          "Invoice Review Submitted",
          outcome.outcome ? { confidence, mode, outcome: outcome.outcome } : { confidence, mode },
        );
      } catch (err: any) {
        submitResults.push({
          filename: label,
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
  }, [invoices, vatRate]);

  /** Close and trigger refresh if anything was booked; a booking with a
   *  warning is booked too, so the lists behind the dialog must reload. */
  const handleClose = useCallback(() => {
    if (results && results.some((r) => isBooked(r.status))) {
      onComplete();
    }
    onClose();
  }, [results, onComplete, onClose]);

  const allDone = results !== null;
  const bookedCount = results?.filter((r) => isBooked(r.status)).length ?? 0;
  const errorCount = results?.filter((r) => r.status === "error").length ?? 0;
  const skippedCount = results?.filter((r) => r.status === "skipped").length ?? 0;
  const warningCount = results?.filter((r) => r.status === "warning").length ?? 0;

  // Pre-flight blocker counts for the action bar — drives the button label
  // ("Boek N van M" when partial) and the inline hint underneath.
  const blockedCount = invoices.filter((inv) => invoiceBlocker(inv) !== null).length;
  const submittableCount = invoices.length - blockedCount;

  return (
    <Dialog
      open={open}
      onClose={submitting ? undefined : handleClose}
      fullWidth
      maxWidth="xl"
      /* Mail-folder documents are reviewed full screen on phones. */
      fullScreen={isXs && hasMailDocuments}
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
            ? "Controleer de gegevens en pas ze aan waar nodig."
            : `${invoices.length} facturen. Controleer de gegevens en pas ze aan waar nodig.`}
        </Typography>
      </DialogTitle>

      {/* Bulk type toggle — only visible for multi-invoice batches.
          Lives OUTSIDE DialogTitle: MUI renders the title as <h2>, and an
          interactive <Box>/<Button> tree inside an <h2> is invalid HTML
          which browsers recover from by closing the <h2> early, ejecting
          this control from the layout entirely. That's how this control
          went silently missing in production. */}
      {invoices.length > 1 && !allDone && (
        <Box
          data-testid="bulk-receipt-toggle"
          sx={{
            px: 3,
            pb: 1,
            display: "flex",
            alignItems: "center",
            gap: 1.5,
            flexWrap: "wrap",
          }}
        >
          <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 600 }}>
            Allemaal als:
          </Typography>
          <Button
            size="small"
            variant={invoices.every((i) => !i.isReceipt) ? "contained" : "outlined"}
            onClick={() => setAllReceiptMode(false)}
            disabled={submitting}
            sx={{ textTransform: "none", py: 0.25 }}
          >
            Factuur
          </Button>
          <Button
            size="small"
            variant={invoices.every((i) => i.isReceipt) ? "contained" : "outlined"}
            color={invoices.every((i) => i.isReceipt) ? "warning" : "primary"}
            onClick={() => setAllReceiptMode(true)}
            disabled={submitting}
            sx={{ textTransform: "none", py: 0.25 }}
          >
            Bonnetje
          </Button>
        </Box>
      )}

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
            severity={errorCount === 0 && skippedCount === 0 && warningCount === 0 ? "success" : "warning"}
            sx={{ mb: 3 }}
            role="status"
            aria-live="polite"
          >
            <AlertTitle sx={{ fontWeight: 600 }}>
              {bookedCount} van {invoices.length} facturen geboekt
              {warningCount > 0 && ` · ${warningCount} met waarschuwing`}
              {skippedCount > 0 && ` · ${skippedCount} overgeslagen`}
              {errorCount > 0 && ` · ${errorCount} mislukt`}
            </AlertTitle>
            {results!.map((r, i) => (
              <Typography key={i} variant="body2" sx={{ mt: 0.5 }}>
                {r.status === "ok" && (
                  <Box component="span" sx={{ color: "success.main", fontWeight: 600 }} aria-label="Geslaagd">V</Box>
                )}
                {r.status === "error" && (
                  <Box component="span" sx={{ color: "error.main", fontWeight: 600 }} aria-label="Mislukt">X</Box>
                )}
                {r.status === "warning" && (
                  <Box component="span" sx={{ color: "warning.main", fontWeight: 600 }} aria-label="Waarschuwing">!</Box>
                )}
                {r.status === "skipped" && (
                  <Box component="span" sx={{ color: "warning.main", fontWeight: 600 }} aria-label="Overgeslagen">~</Box>
                )}{" "}
                {r.filename}
                {r.details ? ` · ${r.details}` : ""}
                {r.error ? ` · ${r.error}` : ""}
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
                  borderColor: invoiceBlocker(inv) !== null ? "error.main" : "divider",
                  borderRadius: 2,
                  p: { xs: 2, sm: 3 },
                  m: 0,
                }}
              >
                {/* Legend: the document title (mail folder) or file name,
                    the original file name small and grey next to a title,
                    a "Creditnota" chip, and the confidence badge */}
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
                  {inv.source.documentTitle ?? inv.source.filename}
                  {inv.source.documentTitle && (
                    <Typography
                      component="span"
                      variant="caption"
                      color="text.secondary"
                      sx={{ fontWeight: 400 }}
                    >
                      {inv.source.filename}
                    </Typography>
                  )}
                  {inv.source.invoice.documentType === "creditnota" && (
                    <Chip
                      label="Creditnota"
                      size="small"
                      sx={{ fontWeight: 600, fontSize: "0.7rem", height: 22, color: "#1e40af", bgcolor: "rgba(59, 130, 246, 0.1)" }}
                    />
                  )}
                  <ConfidenceBadge confidence={inv.source.invoice.confidence} />
                </Typography>

                {/* Duplicate-upload warning — shown when the same PDF
                    (by content hash) was previously booked successfully.
                    Loud orange alert because re-booking a duplicate creates
                    a phantom mutation in e-boekhouden that's awkward to
                    unwind. The user can still proceed if they really want
                    to (e.g. a credit note follow-up that happens to share
                    a PDF) — we just make sure they made an active choice. */}
                {inv.source.alreadySubmitted && (
                  <Alert severity="warning" sx={{ mt: 1.5 }}>
                    <AlertTitle sx={{ fontWeight: 700 }}>
                      Mogelijk dubbele factuur
                    </AlertTitle>
                    <Typography variant="body2">
                      Dit bestand is op{" "}
                      <strong>
                        {new Date(inv.source.alreadySubmitted.submittedAt).toLocaleDateString("nl-NL")}
                      </strong>{" "}
                      via Speedy geboekt als mutatie <strong>{inv.source.alreadySubmitted.mutNr}</strong>.
                      Boek je het opnieuw, dan staat het dubbel in e-Boekhouden.
                    </Typography>
                  </Alert>
                )}

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
                    aria-label="Soort document"
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
                  {/* Preview pane. Prefer the R2 presigned URL when
                      available, fall back to the client-side blob: URL so the
                      preview works in self-hosted deployments without R2 and
                      for mail-folder documents. Images render as <img>. */}
                  <DocumentPreview
                    url={inv.source.pdfUrl || inv.source.localPdfUrl || ""}
                    filename={inv.source.filename}
                    mediaType={inv.source.localFile?.type}
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
                        label="Leverancier"
                        value={inv.leverancier}
                        onChange={(e) => updateField(index, "leverancier", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        helperText={inv.isReceipt ? "Voor de omschrijving en de archiefnaam. Speedy maakt geen relatie aan." : undefined}
                      />
                      {!inv.isReceipt && (
                        <RelationPicker
                          value={inv.relation}
                          onChange={(r) => {
                            updateField(index, "relation", r);
                            if (r && r.grootboekrekeningId == null) resolveRelationLedger(index, r.id);
                          }}
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

                    {inv.regels === null ? (<>
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
                        error={invalidAmountField(inv.bedragExcl)}
                        helperText={invalidAmountField(inv.bedragExcl) ? AMOUNT_HINT : undefined}
                        onChange={(e) => updateField(index, "bedragExcl", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        slotProps={{
                          htmlInput: { inputMode: "decimal" },
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
                        error={invalidAmountField(inv.btwBedrag)}
                        helperText={invalidAmountField(inv.btwBedrag) ? AMOUNT_HINT : undefined}
                        onChange={(e) => updateField(index, "btwBedrag", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        slotProps={{
                          htmlInput: { inputMode: "decimal" },
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
                        error={invalidAmountField(inv.bedragIncl, { required: true })}
                        helperText={invalidAmountField(inv.bedragIncl, { required: true }) ? AMOUNT_HINT : undefined}
                        onChange={(e) => updateField(index, "bedragIncl", e.target.value)}
                        size="small"
                        fullWidth
                        disabled={submitting}
                        slotProps={{
                          htmlInput: { inputMode: "decimal" },
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
                        codes={purchaseVatCodes}
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
                    {/* A booking on several tegenrekeningen or BTW codes
                        (a gift plus shipping, a restaurant receipt with
                        21/9/0): the lines replace the amount fields. */}
                    {inv.source.invoice.documentType !== "creditnota" && (
                      <Box sx={{ mt: 1 }}>
                        <Button size="small" onClick={() => splitInvoice(index)} disabled={submitting}>
                          Splitsen over meerdere regels
                        </Button>
                      </Box>
                    )}
                    </>) : (
                    <Box sx={{ mt: 2 }}>
                      <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 1, mb: 1 }}>
                        <Typography variant="subtitle2" component="h3">
                          Regels
                        </Typography>
                        {/* One way back at a time: an untouched proposal is
                            undone in the note below; a split the user made
                            or edited keeps line 1 through "Niet splitsen". */}
                        {(!inv.proposal || inv.proposalEdited) && (
                          <Button size="small" onClick={() => setRegels(index, null)} disabled={submitting}>
                            Niet splitsen
                          </Button>
                        )}
                      </Box>
                      {/* Speedy proposed this split; the user books or undoes
                          it, nothing is booked on its own. A note, not an
                          alert: several cards may open with one. */}
                      {inv.proposal && !inv.proposalEdited && (
                        <Alert
                          severity="info"
                          role="note"
                          sx={{ mb: 2 }}
                          action={
                            <Button
                              color="inherit"
                              size="small"
                              onClick={() => undoSplitProposal(index)}
                              disabled={submitting}
                              sx={{ fontWeight: 600, whiteSpace: "nowrap" }}
                              aria-label="Splitsing ongedaan maken"
                            >
                              Ongedaan maken
                            </Button>
                          }
                        >
                          <Typography variant="body2">
                            Speedy stelt voor {inv.isReceipt ? "dit bonnetje" : "deze factuur"} te splitsen: {inv.proposal.advies.toelichting}
                          </Typography>
                        </Alert>
                      )}
                      {/* The total the lines add up to: the bank line for a
                          receipt booked against one, else typed here. */}
                      {splitTotalFromBankLine(inv) ? null : (
                        <TextField
                          label="Totaal incl. BTW"
                          value={inv.bedragIncl}
                          error={invalidAmountField(inv.bedragIncl, { required: true })}
                          helperText={invalidAmountField(inv.bedragIncl, { required: true }) ? AMOUNT_HINT : undefined}
                          onChange={(e) => updateField(index, "bedragIncl", e.target.value)}
                          size="small"
                          disabled={submitting}
                          sx={{ mb: 1.5, maxWidth: { sm: 240 } }}
                          slotProps={{
                            htmlInput: { inputMode: "decimal" },
                            input: {
                              startAdornment: (
                                <Typography variant="body2" color="text.secondary" sx={{ mr: 0.5 }}>
                                  &euro;
                                </Typography>
                              ),
                            },
                          }}
                        />
                      )}
                      <RegelsEditor
                        regels={inv.regels}
                        totalCents={splitTotalCents(inv)}
                        totalSource={splitTotalFromBankLine(inv) ? "afschrift" : undefined}
                        ledgerAccounts={ledgerAccounts}
                        vatCodes={purchaseVatCodes}
                        vatRate={vatRate}
                        onChange={(regels) => setRegels(index, regels)}
                        disabled={submitting}
                      />
                    </Box>
                    )}

                    {/* Row 4b: Archive folder picker. For an upload it is
                        optional — leaving it empty means the PDF is not
                        uploaded to the digitaal archief (mutation is still
                        booked). For a mail-folder document the file always
                        leaves the mail folder: the picker says where it goes,
                        empty meaning Facturen/<jaar>/<maand>, and the mail
                        folder's subtree is not on offer. */}
                    <Box sx={{ mt: 2 }}>
                      {isArchiveSource(inv.source) ? (
                        <ArchiveFolderPicker
                          value={inv.archiveFolder}
                          onChange={(f) => updateField(index, "archiveFolder", f)}
                          disabled={submitting}
                          label="Verplaatsen naar"
                          helperText={archiveFolderHelperText({
                            folderPicked: inv.archiveFolder !== null,
                            datum: inv.datum,
                            mailFolderName: inv.source.mailFolderName ?? "",
                          })}
                          excludeFolderIds={excludeFolderIds}
                        />
                      ) : (
                        <ArchiveFolderPicker
                          value={inv.archiveFolder}
                          onChange={(f) => updateField(index, "archiveFolder", f)}
                          disabled={submitting}
                          helperText="Optioneel. Kies een map om het bestand in je digitale archief te bewaren."
                        />
                      )}
                    </Box>

                    {/* Why this grootboekrekening: the relation's default or Claude's reasoning */}
                    {inv.ledgerSource === "relation" && inv.relation && (
                      <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 1 }}>
                        Standaardrekening van {inv.relation.bedrijf}
                      </Typography>
                    )}
                    {inv.ledgerSource === "ai" && inv.source.invoice.redenering && (
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

                    {/* Reverse charge warning — only shown when the AI flagged
                        the invoice as reverse charge AND the user hasn't
                        overridden the dropdown to a non-reverse-charge code.
                        Previously the alert kept rendering with the new
                        selection's label spliced in, producing nonsense like
                        "verlegd via BTW-code Geen btw" when the user picked
                        GEEN. The user's explicit override wins. */}
                    {inv.source.invoice.isReverseCharge && isReverseChargeCode(inv.btwCode) && (
                      <Alert severity="warning" sx={{ mt: 2 }}>
                        <Typography variant="body2" fontWeight={600} gutterBottom>
                          Verlegde BTW (reverse charge)
                        </Typography>
                        <Typography variant="body2">
                          Op deze factuur is de BTW naar jou verlegd. Speedy gebruikt BTW-code{" "}
                          <strong>{formatBTWCode(inv.btwCode, vatCodes)}</strong>. Controleer of die klopt:
                          de verlegde BTW geef je zelf aan in je BTW-aangifte.
                        </Typography>
                      </Alert>
                    )}

                    {/* Belastingadvies from Claude.
                        - Filter out the reverse_charge type because the
                          dedicated warning Alert above already covers it.
                        - Elevate "geen_btw" tips into their own prominent
                          info Alert at the top: these warn about BTW that
                          is NOT deductible (assurantiebelasting,
                          buitenlandse BTW, foreign tax) and the user kept
                          missing them in the small icon-less box. They
                          materially affect the booking, so they deserve
                          weight comparable to the reverse-charge alert.
                        - Render remaining advisory tips (KIA, vaste activa,
                          gemengd gebruik, representatie) in a compact list
                          below as before. */}
                    {(() => {
                      const allTips = (inv.source.invoice.belastingAdvies ?? []).filter(
                        (t) => t.type !== "reverse_charge",
                      );
                      const headlineTips = allTips.filter((t) => t.type === "geen_btw");
                      const otherTips = allTips.filter((t) => t.type !== "geen_btw");
                      if (allTips.length === 0) return null;
                      return (
                        <>
                          {headlineTips.map((tip, i) => (
                            <Alert
                              key={`headline-${i}`}
                              severity="info"
                              variant="outlined"
                              sx={{
                                mt: 2,
                                borderWidth: 2,
                                borderLeftWidth: 6,
                                bgcolor: "rgba(2, 136, 209, 0.06)",
                                "& .MuiAlert-message": { width: "100%" },
                              }}
                            >
                              <AlertTitle sx={{ fontWeight: 700 }}>
                                Let op: niet-aftrekbare BTW
                              </AlertTitle>
                              <Typography variant="body2" sx={{ lineHeight: 1.55 }}>
                                {tip.tekst}
                              </Typography>
                            </Alert>
                          ))}
                          {otherTips.length > 0 && (
                            <Alert severity="info" sx={{ mt: 2 }} icon={false}>
                              <Typography variant="body2" fontWeight={600} gutterBottom>
                                Belastingadvies
                              </Typography>
                              {otherTips.map((tip, i) => (
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
                        </>
                      );
                    })()}

                    {/* Bank line choice — a mail-folder document with several
                        candidate lines gets a select; the chosen line is shown
                        and linked below. */}
                    {isArchiveSource(inv.source) && (() => {
                      const choices = bankLineChoices(inv.source);
                      if (choices.length < 2) return null;
                      return (
                        <TextField
                          select
                          label="Afschriftregel"
                          value={inv.bankLine?.id ?? ""}
                          onChange={(e) => {
                            const id = Number(e.target.value);
                            selectBankLine(index, choices.find((l) => l.id === id) ?? null);
                          }}
                          size="small"
                          fullWidth
                          disabled={submitting}
                          helperText="Meerdere afschriftregels kunnen de betaling zijn. Kies de juiste."
                          sx={{ mt: 2 }}
                        >
                          {choices.map((line) => (
                            <MenuItem key={line.id} value={line.id}>
                              {bankLineOptionLabel(line)}
                            </MenuItem>
                          ))}
                        </TextField>
                      );
                    })()}

                    {/* Matched bank line */}
                    {inv.bankLine && (
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
                              onClick={() => updateField(index, "importId", inv.bankLine!.id)}
                            >
                              Koppelen
                            </Button>
                          )
                        }
                      >
                        <Typography variant="body2" fontWeight={600}>
                          {inv.importId ? "Gekoppeld aan afschriftregel:" : "Gevonden afschriftregel:"}
                          {inv.bankLine.currencyConverted && (
                            <Typography
                              component="span"
                              variant="caption"
                              sx={{ ml: 1, fontWeight: 500, fontStyle: "italic" }}
                            >
                              (geschatte koers, controleer het bedrag)
                            </Typography>
                          )}
                        </Typography>
                        <Typography variant="body2">
                          {new Date(inv.bankLine.datum).toLocaleDateString("nl-NL")} · {new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(inv.bankLine.bedrag)} · {inv.bankLine.omschrijving.slice(0, 80)}
                        </Typography>
                        {inv.bankLine.currencyConverted && inv.bankLine.invoiceCurrency && (
                          <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 0.5 }}>
                            {fxInvoiceNote(inv.bankLine.invoiceAmount, inv.bankLine.invoiceCurrency)}
                          </Typography>
                        )}
                      </Alert>
                    )}

                    {/* A mail-folder invoice without a bank line is booked
                        as an open invoice (UX flow 4). */}
                    {isOpenBooking({
                      archiveFileId: inv.source.archiveFileId,
                      isReceipt: inv.isReceipt,
                      importId: inv.importId,
                    }) && (
                      <Alert severity="info" sx={{ mt: 2 }}>
                        <Typography variant="body2">{NO_MATCH_ALERT_TEXT}</Typography>
                      </Alert>
                    )}

                    {/* Tip hint. When the bank line is more than € 0,05 above
                        the receipt, the difference is almost always a tip the
                        user added on the card. "Splits in fooi" turns the
                        booking into two lines: the receipt and the tip on the
                        same tegenrekening without BTW (there is no BTW on a
                        tip). The editor above then shows the lines; once
                        split, the remainder line replaces this hint. */}
                    {inv.isReceipt && inv.regels === null && (() => {
                      const incl = parseAmount(inv.bedragIncl) ?? 0;
                      const bankAmount = inv.bankLine ? Math.abs(inv.bankLine.bedrag) : 0;
                      const deltaCents = bankAmount > 0 ? toCents(bankAmount) - toCents(incl) : 0;
                      if (deltaCents <= 5) return null;
                      return (
                        <Alert
                          severity="info"
                          sx={{ mt: 2 }}
                          action={
                            <Button
                              color="inherit"
                              size="small"
                              onClick={() => splitTip(index, deltaCents)}
                              disabled={submitting}
                              sx={{ fontWeight: 600, whiteSpace: "nowrap" }}
                            >
                              Splits in fooi
                            </Button>
                          }
                        >
                          <Typography variant="body2">
                            Het bonnetje is {formatMoney(incl)}, maar op het afschrift
                            staat {formatMoney(bankAmount)}: een verschil van{" "}
                            <strong>{formatMoney(deltaCents / 100)}</strong>. Meestal is dat een
                            fooi. Zet het verschil op een aparte regel zonder BTW, want
                            op een fooi zit geen BTW.
                          </Typography>
                        </Alert>
                      );
                    })()}

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
            {blockedCount > 0 && !submitting && (
              <Typography
                variant="body2"
                color="warning.main"
                sx={{ mr: "auto", pl: 1 }}
              >
                {(() => {
                  const creditnotas = invoices.filter((inv) => inv.source.invoice.documentType === "creditnota").length;
                  const missingRel = invoices.filter((inv) => !inv.isReceipt && !inv.relation).length;
                  const missingLedger = invoices.filter((inv) => inv.regels === null && !inv.ledgerAccount).length;
                  const missingBank = invoices.filter((inv) => inv.isReceipt && !inv.importId).length;
                  const splitOpen = invoices.filter((inv) => splitBlocker(inv) !== null).length;
                  const parts = [];
                  if (creditnotas) parts.push(`${creditnotas} ${creditnotas === 1 ? "creditnota" : "creditnota's"}`);
                  if (missingRel) parts.push(`${missingRel} zonder relatie`);
                  if (missingLedger) parts.push(`${missingLedger} zonder tegenrekening`);
                  if (missingBank) parts.push(`${missingBank} bonnetje${missingBank > 1 ? "s" : ""} zonder afschriftregel`);
                  if (splitOpen) parts.push(`${splitOpen} met regels die nog niet kloppen`);
                  return `${blockedCount} ${blockedCount === 1 ? "wordt" : "worden"} overgeslagen (${parts.join(", ")})`;
                })()}
              </Typography>
            )}
            <Button onClick={handleClose} disabled={submitting}>
              Annuleren
            </Button>
            <Button
              variant="contained"
              onClick={handleSubmitAll}
              disabled={submitting || submittableCount === 0}
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
              {submitButtonLabel({
                submitting,
                progress,
                total: invoices.length,
                blockedCount,
                openBooking:
                  invoices.length === 1 &&
                  isOpenBooking({
                    archiveFileId: invoices[0].source.archiveFileId,
                    isReceipt: invoices[0].isReceipt,
                    importId: invoices[0].importId,
                  }),
              })}
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
function buildEdit(
  a: AnalyzedInvoice,
  ledgerAccounts: LedgerAccount[],
  vatCodes: VATCode[],
): InvoiceEdit {
  const inv = a.invoice;

  const ledger = initialLedger(a.matchedRelation, inv.grootboekcode, ledgerAccounts);

  // Build relation from matchedRelation if present
  const matchedRelation: Relation | null = a.matchedRelation
    ? {
        id: a.matchedRelation.id,
        code: a.matchedRelation.code,
        bedrijf: a.matchedRelation.bedrijf,
        grootboekrekeningId: a.matchedRelation.grootboekrekeningId ?? null,
        iban: "",
      }
    : null;

  // Normalize the bedragen on load. For any zero-rate BTW code (GEEN or a
  // reverse-charge variant like VERL_INK / BU_EU_INK) we force excl to
  // equal incl and zero the btw — non-deductible or shifted taxes are part
  // of the cost and must not be split out, regardless of what the PDF showed.
  const btwCode = inv.btwCode || "HOOG_INK_21";
  const rate = vatCodes.find((v) => v.code === btwCode)?.percentage ?? 0;
  let bedragIncl = inv.bedragInclBtw ?? 0;
  let bedragExcl = inv.bedragExclBtw ?? 0;
  let btwBedrag = inv.btwBedrag ?? 0;

  // For non-EUR invoices that fuzzy-matched a EUR bank line via FX
  // conversion, swap the foreign-currency invoice amount for the actual
  // EUR amount of the bank line. e-Boekhouden's mutation has to balance
  // against the bank's real EUR debit; booking CHF figures would create
  // a totals mismatch. The user can still adjust manually if needed.
  if (a.matchedBankLine?.currencyConverted) {
    bedragIncl = Math.abs(a.matchedBankLine.bedrag);
    if (rate === 0) {
      bedragExcl = bedragIncl;
      btwBedrag = 0;
    } else {
      btwBedrag = +(bedragIncl * rate / (100 + rate)).toFixed(2);
      bedragExcl = +(bedragIncl - btwBedrag).toFixed(2);
    }
  } else if (rate === 0) {
    bedragExcl = bedragIncl;
    btwBedrag = 0;
  } else if (Math.abs(bedragExcl + btwBedrag - bedragIncl) > 0.01) {
    // Numbers don't add up — trust incl + btw, derive excl.
    bedragExcl = bedragIncl - btwBedrag;
  }

  // Claude's split proposal opens the card split, with the single-line
  // values kept for "Ongedaan maken". Not for a credit note or a non-invoice
  // (nothing to book), and not when the amounts were swapped for the bank
  // line's euro amount: the proposed lines are in the invoice's currency.
  const single: SingleLineValues = {
    ledgerAccount: ledger.account,
    ledgerSource: ledger.source,
    btwCode,
    bedragExcl: formatAmountInput(bedragExcl),
    bedragIncl: formatAmountInput(bedragIncl),
    btwBedrag: formatAmountInput(btwBedrag),
  };
  const bookable = inv.documentType !== "creditnota" && inv.documentType !== "overig";
  const proposal = bookable && !a.matchedBankLine?.currencyConverted ? proposeRegels(inv, ledgerAccounts, ledger.account) : null;

  return {
    source: a,
    leverancier: inv.leverancier ?? "",
    factuurnummer: inv.factuurnummer ?? "",
    datum: inv.datum ?? new Date().toISOString().slice(0, 10),
    bedragExcl: formatAmountInput(bedragExcl),
    bedragIncl: formatAmountInput(bedragIncl),
    btwBedrag: formatAmountInput(btwBedrag),
    // Split by a proposal, the booking's ledger and BTW code mirror line 1
    // (as setRegels keeps them), with "ai" as their source.
    btwCode: proposal ? proposal.regels[0].btwCode : btwCode,
    omschrijving: inv.omschrijving ?? "",
    relation: matchedRelation,
    ledgerAccount: proposal ? proposal.regels[0].ledgerAccount : ledger.account,
    ledgerSource: proposal ? "ai" : ledger.source,
    importId: a.matchedBankLine?.id ?? 0,
    bankLine: a.matchedBankLine ?? null,
    isReceipt: inv.isReceipt ?? false,
    regels: proposal?.regels ?? null,
    proposal,
    proposalEdited: false,
    original: proposal ? single : null,
    archiveFolder: null,
  };
}

/** Format a string amount as euros */
/**
 * Turn a raw BTW code like "VERL_INK" into a human-readable label like
 * "Btw verlegd 21% (VERL_INK)", matching the format the picker dropdown
 * uses so the user sees the same string everywhere. Falls back to the raw
 * code if we can't resolve it.
 */
function formatBTWCode(code: string, vatCodes: VATCode[]): string {
  const match = vatCodes.find((v) => v.code === code);
  if (!match) return code;
  return `${match.omschrijving} (${code})`;
}


/**
 * Read a File and return the base64-encoded body without the
 * "data:<mime>;base64," prefix \u2014 that prefix is what readAsDataURL adds, but
 * the e-boekhouden archive upload endpoint expects raw base64. Rejects on
 * read errors so the caller can surface a clear UI message.
 */
function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result;
      if (typeof result !== "string") {
        reject(new Error("Het bestand kon niet worden gelezen"));
        return;
      }
      const comma = result.indexOf(",");
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.onerror = () => reject(reader.error ?? new Error("Lezen mislukt"));
    reader.readAsDataURL(file);
  });
}

/**
 * DocumentPreview — renders the document inline with a fallback link.
 *
 * Images (JPG/PNG, by media type or extension) render as a plain <img>. PDFs
 * use <object type="application/pdf"> which has better cross-browser PDF
 * rendering than <iframe> (Safari, Firefox handle it natively). The fallback
 * content inside <object> is shown when the browser cannot render PDFs inline
 * (e.g. mobile browsers), providing a direct download/open link.
 *
 * Accessibility:
 * - The <img> has alt text naming the document; the <object> an aria-label
 * - The fallback link is keyboard-accessible and clearly labeled
 * - The "open in new tab" link uses aria-describedby to clarify it opens externally
 * - role="document" on the object tells screen readers this is an embedded document
 */
function DocumentPreview({ url: pdfUrl, filename, mediaType }: { url: string; filename: string; mediaType?: string }) {
  if (pdfUrl && previewKind(mediaType, filename) === "image") {
    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Box
          component="img"
          src={pdfUrl}
          alt={`Voorbeeld van ${filename}`}
          sx={{
            width: "100%",
            maxHeight: { xs: 400, md: 600 },
            objectFit: "contain",
            border: "1px solid",
            borderColor: "divider",
            borderRadius: 1,
            bgcolor: "grey.50",
          }}
        />
        <OpenInNewTabLink href={pdfUrl} />
      </Box>
    );
  }

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
            Je browser kan deze PDF hier niet tonen.
          </Typography>
          <Link
            href={pdfUrl}
            target="_blank"
            rel="noopener noreferrer"
            sx={{ fontWeight: 600, fontSize: "0.875rem" }}
          >
            PDF openen in een nieuw tabblad
          </Link>
        </Box>
      </Box>

      {/* "Open in new tab" link — always visible below the preview */}
      <OpenInNewTabLink href={pdfUrl} />
    </Box>
  );
}

/** The "Openen in nieuw tabblad" link under a preview. */
function OpenInNewTabLink({ href }: { href: string }) {
  return (
    <Link
      href={href}
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
      Openen in een nieuw tabblad
    </Link>
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
    label = `Hoge zekerheid (${pct}%)`;
  } else if (pct >= 50) {
    color = "#92400e";
    bgcolor = "rgba(245, 158, 11, 0.1)";
    label = `Matige zekerheid (${pct}%)`;
  } else {
    color = "#991b1b";
    bgcolor = "rgba(220, 38, 38, 0.1)";
    label = `Lage zekerheid (${pct}%)`;
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
