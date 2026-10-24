import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  Box,
  Typography,
  Button,
  Chip,
  Tabs,
  Tab,
  Paper,
  Skeleton,
  Alert,
  AlertTitle,
  CircularProgress,
  Checkbox,
  Tooltip,
  Snackbar,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import { useSearchParams } from "react-router-dom";
import { api } from "../../api/client";
import { track } from "../../analytics";
import { useAuth } from "../../context/AuthContext";
import { useLedgerAccounts } from "../../hooks/useLedgerAccounts";
import { useVATCodes } from "../../hooks/useVATCodes";
import { useMailDocuments } from "../../hooks/useMailDocuments";
import { inboxSubtitle, pendingDocumentCount } from "../../hooks/mailDocumentsModel";
import type { InboxClassification, InboxCategory, InboxProcessResult, AnalyzedInvoice } from "../../api/types";
import { InboxRow } from "./InboxRow";
import { BatchApproveBar } from "./BatchApproveBar";
import { InvoiceReviewDialog } from "./InvoiceReviewDialog";
import { AiStatusAlert, type AiStatus } from "./AiStatusAlert";
import { DocumentList } from "./DocumentList";

/** The two lists of the inbox; the active one lives in ?lijst=. */
type InboxTab = "afschriften" | "documenten";

/** Tab label with its count in a small pill. */
function TabLabel({ text, count }: { text: string; count: number | null }) {
  return (
    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.75 }}>
      {text}
      {count !== null && (
        <Box
          component="span"
          sx={{
            px: 0.75,
            minWidth: 20,
            borderRadius: 999,
            bgcolor: "action.selected",
            fontSize: "0.75rem",
            fontWeight: 600,
            lineHeight: 1.7,
            textAlign: "center",
            fontVariantNumeric: "tabular-nums",
          }}
        >
          {count}
        </Box>
      )}
    </Box>
  );
}

/**
 * Category filter tab configuration.
 * "all" is a synthetic category that shows everything.
 */
const FILTER_TABS: { value: InboxCategory | "all"; label: string }[] = [
  { value: "all", label: "Alles" },
  { value: "auto", label: "Auto" },
  { value: "review", label: "Controleer" },
  { value: "invoice", label: "Factuur" },
  { value: "manual", label: "Handmatig" },
];

/** Summary badge colors — same tokens as InboxRow category badges */
const SUMMARY_COLORS: Record<InboxCategory, { color: string; bgcolor: string }> = {
  auto: { color: "#166534", bgcolor: "rgba(22, 163, 74, 0.1)" },
  review: { color: "#92400e", bgcolor: "rgba(245, 158, 11, 0.1)" },
  invoice: { color: "#1e40af", bgcolor: "rgba(59, 130, 246, 0.1)" },
  manual: { color: "#991b1b", bgcolor: "rgba(220, 38, 38, 0.1)" },
};

const SUMMARY_LABELS: Record<InboxCategory, string> = {
  auto: "auto",
  review: "controleer",
  invoice: "factuur",
  manual: "handmatig",
};

const CATEGORY_TOOLTIPS: Record<InboxCategory, string> = {
  auto: "Geen factuur nodig — bankkosten, belasting, prive. Verwerk met een klik.",
  review: "AI heeft een suggestie. Controleer de velden voor verwerking.",
  invoice: "Factuur of bon vereist. Upload het PDF-bestand om te verwerken.",
  manual: "Vul de boeking handmatig in.",
};

function getGreeting(): string {
  const hour = new Date().getHours();
  if (hour < 12) return "Goedemorgen";
  if (hour < 18) return "Goedemiddag";
  return "Goedenavond";
}

function getFirstName(name: string | undefined): string {
  if (!name) return "";
  return name.split(" ")[0];
}

/**
 * BookkeepingInbox — the core "todo list" for unprocessed bank lines and
 * for the documents in the Mail & herken folder.
 *
 * This replaces the old bank statement list + invoice upload with a unified
 * workflow. AI classifies every unprocessed bank line into one of four
 * categories (auto, review, invoice, manual) and the user works through
 * them toward inbox zero. The "Documenten" tab (DocumentList) does the same
 * for the files that arrive by mail.
 *
 * Layout:
 * - Greeting + subtitle with both counts
 * - Action buttons; summary bar with colored category badges (Afschriften)
 * - Tabs "Afschriften {n}" / "Documenten {n}", state in ?lijst=
 * - Afschriften: filter tabs, InboxRow list with checkboxes, BatchApproveBar
 * - Documenten: DocumentList
 *
 * Accessibility:
 * - Semantic structure: <main> landmark (provided by Layout), h1 heading, <nav> for tabs
 * - List tabs name their counts; both panels stay mounted and are hidden
 *   with the hidden attribute so row state survives switching
 * - Filter tabs use aria-label and aria-controls to link to the list
 * - The list uses <ul> with role="list", each row is <li>
 * - "Select all" checkbox has descriptive aria-label
 * - Loading states use aria-busy and role="status"
 * - Dynamic count updates use aria-live="polite"
 * - Empty state is announced to screen readers
 */
export function BookkeepingInbox() {
  const { user, eboekhoudenConnected } = useAuth();
  const { data: ledgerAccounts } = useLedgerAccounts();
  const { data: vatCodes } = useVATCodes();
  const theme = useTheme();
  const isXs = useMediaQuery(theme.breakpoints.down("sm"));
  const [searchParams, setSearchParams] = useSearchParams();

  const [items, setItems] = useState<InboxClassification[]>([]);
  const [summary, setSummary] = useState<Record<InboxCategory, number>>({ auto: 0, review: 0, invoice: 0, manual: 0 });
  const [loading, setLoading] = useState(true);
  const [classifying, setClassifying] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<InboxCategory | "all">("all");
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [processedIds, setProcessedIds] = useState<Set<number>>(new Set());
  const [analyzingInvoices, setAnalyzingInvoices] = useState(false);
  const [analyzeError, setAnalyzeError] = useState<string | null>(null);
  const [reviewInvoices, setReviewInvoices] = useState<AnalyzedInvoice[] | null>(null);
  const [apiKeyStatus, setApiKeyStatus] = useState<AiStatus | null>(null);
  // Snackbar shown after rows are processed so the user gets explicit
  // feedback that something happened — without this, processed rows just
  // vanish silently because we render activeItems.
  const [snackbar, setSnackbar] = useState<{ message: string; severity: "success" | "warning" } | null>(null);

  // The documents inbox loads on the same trigger as the bank lines.
  const documents = useMailDocuments(eboekhoudenConnected);
  const { state: docState, ...docActions } = documents;
  const docsKnown = docState.loaded || docState.error !== null;
  const docCount = pendingDocumentCount(docState.documents);

  const firstName = getFirstName(user?.name);

  // Shared handler for analyzing invoice files (used by both upload button and InboxRow).
  //
  // Each analyzed response is enriched with the original File object and a
  // blob: URL so the review dialog can (a) preview the PDF locally even when
  // R2 isn't configured and (b) re-ship the PDF inline as base64 on submit
  // without a second upload round-trip. The blob URLs are revoked when the
  // dialog closes — see handleReviewClose below.
  const handleInvoiceFiles = useCallback(async (files: File[]) => {
    setAnalyzingInvoices(true);
    setAnalyzeError(null);

    const analyzed: AnalyzedInvoice[] = [];
    for (const file of files) {
      try {
        const res = await api.analyzeInvoice(file);
        analyzed.push({
          ...res,
          localFile: file,
          localPdfUrl: URL.createObjectURL(file),
        });
        track("Invoice Analyzed", { confidence: String(Math.round(res.invoice.confidence * 100)) });
      } catch (err: any) {
        setAnalyzeError(`Fout bij ${file.name}: ${err.message}`);
      }
    }

    setAnalyzingInvoices(false);
    if (analyzed.length > 0) {
      setReviewInvoices(analyzed);
    }
  }, []);

  // Revoke any blob URLs we created for PDF previews to release the underlying
  // file data. The browser holds the File alive for the lifetime of every
  // outstanding object URL, so leaking these on every batch eats memory fast.
  const handleReviewClose = useCallback(() => {
    if (reviewInvoices) {
      for (const r of reviewInvoices) {
        if (r.localPdfUrl) URL.revokeObjectURL(r.localPdfUrl);
      }
    }
    setReviewInvoices(null);
  }, [reviewInvoices]);

  // Load inbox — try AI classification first, fall back to raw bank lines.
  //
  // We reset `loading` to true at the start of every load so refresh /
  // reconnect flows show the same loading affordance as the very first
  // mount. Without this, the empty-state guard ("Inbox zero!") rendered
  // momentarily during the connect → classify path because `loading`
  // stayed false from the not-connected branch while `classifying` was
  // true with items still empty.
  const loadInbox = useCallback(async (force = false) => {
    if (!eboekhoudenConnected) {
      setLoading(false);
      return;
    }

    setLoading(true);
    setClassifying(true);
    setError(null);

    try {
      // Try AI classification (force=true skips Redis cache)
      const res = await api.classifyInbox(force);
      setItems(res.classifications);
      setSummary(res.summary);
      track("Inbox Classify", { total: String(res.totalCount) });
    } catch (classifyErr: any) {
      // If the e-boekhouden session expired, the API client has already
      // dispatched eb:session-expired and the AuthContext will flip the
      // connection flag. Skip the bank-statements fallback so we don't
      // surface a confusing 412 error — the not-connected branch will render
      // the connect prompt on the next pass.
      if (classifyErr?.status === 412) {
        setLoading(false);
        setClassifying(false);
        return;
      }
      // AI failed — fall back to raw bank statement lines (manual mode)
      try {
        const raw = await api.getBankStatements();
        const manualItems: InboxClassification[] = (raw.items || []).map((row: any) => ({
          id: row.id,
          datum: row.mutDatum || row.datum || "",
          bedrag: row.mutBedrag ?? row.bedrag ?? 0,
          omschrijving: row.mutOmschrijving || row.omschrijving || "",
          rekening: row.rekening || "",
          grootboekId: row.grootboekId || 0,
          category: "manual" as InboxCategory,
          needsInvoice: (row.mutBedrag ?? row.bedrag ?? 0) < 0, // outgoing payments need invoice
          confidence: 0,
          grootboekcode: "",
          btwCode: "GEEN",
          soort: (row.mutBedrag ?? row.bedrag ?? 0) < 0 ? "GeldUitgegeven" : "GeldOntvangen",
          aiOmschrijving: "",
          indicator: (row.mutBedrag ?? row.bedrag ?? 0) < 0 ? "Factuur uploaden" : "Handmatig verwerken",
        }));
        setItems(manualItems);
        setSummary({ auto: 0, review: 0, invoice: 0, manual: manualItems.length });
      } catch (err2: any) {
        if (err2?.status === 412) {
          // Same case here — defer to the AuthContext-driven not-connected branch.
          return;
        }
        setError(err2 instanceof Error ? err2.message : "Kon afschriften niet laden");
      }
    } finally {
      setLoading(false);
      setClassifying(false);
    }
  }, [eboekhoudenConnected]);

  useEffect(() => {
    loadInbox();
    // Check API key status in background
    api.checkApiKeyStatus().then(setApiKeyStatus).catch(() => {});
  }, [loadInbox]);

  // Filter items by category
  const filteredItems = useMemo(
    () => (filter === "all" ? items : items.filter((i) => i.category === filter)),
    [items, filter],
  );

  // Items that aren't processed yet and are in the current filter
  const activeItems = useMemo(
    () => filteredItems.filter((i) => !processedIds.has(i.id)),
    [filteredItems, processedIds],
  );

  // Toggle individual selection
  const toggleItem = useCallback((id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  // Select/deselect all visible active items
  const toggleAll = useCallback(() => {
    setSelected((prev) => {
      const activeIds = activeItems.map((i) => i.id);
      const allSelected = activeIds.every((id) => prev.has(id));
      if (allSelected) {
        const next = new Set(prev);
        activeIds.forEach((id) => next.delete(id));
        return next;
      } else {
        const next = new Set(prev);
        activeIds.forEach((id) => next.add(id));
        return next;
      }
    });
  }, [activeItems]);

  // Handle batch processing results
  const handleBatchProcessed = useCallback((results: Map<number, InboxProcessResult>) => {
    const newProcessed = new Set<number>();
    let errorCount = 0;
    results.forEach((result, id) => {
      if (result.status === "ok") newProcessed.add(id);
      else errorCount++;
    });
    setProcessedIds((prev) => new Set([...prev, ...newProcessed]));
    setSelected(new Set());

    // Update summary counts
    setSummary((prev) => {
      const next = { ...prev };
      newProcessed.forEach((id) => {
        const item = items.find((i) => i.id === id);
        if (item) next[item.category] = Math.max(0, next[item.category] - 1);
      });
      return next;
    });

    // Surface explicit feedback — rows disappear instantly so the user
    // needs an external signal that the action succeeded.
    if (newProcessed.size > 0) {
      const okMsg = `${newProcessed.size} ${newProcessed.size === 1 ? "regel" : "regels"} geboekt`;
      setSnackbar({
        message: errorCount > 0 ? `${okMsg} (${errorCount} mislukt)` : okMsg,
        severity: errorCount > 0 ? "warning" : "success",
      });
    }
  }, [items]);

  // Handle single item processed
  const handleSingleProcessed = useCallback((id: number) => {
    setProcessedIds((prev) => new Set([...prev, id]));
    setSelected((prev) => {
      const next = new Set(prev);
      next.delete(id);
      return next;
    });

    const item = items.find((i) => i.id === id);
    if (item) {
      setSummary((prev) => ({
        ...prev,
        [item.category]: Math.max(0, prev[item.category] - 1),
      }));
    }
    setSnackbar({ message: "Regel geboekt", severity: "success" });
  }, [items]);

  // Clear selection
  const clearSelection = useCallback(() => setSelected(new Set()), []);

  // Selected items for the batch bar
  const selectedItems = useMemo(
    () => items.filter((i) => selected.has(i.id) && !processedIds.has(i.id)),
    [items, selected, processedIds],
  );

  const totalActive = items.filter((i) => !processedIds.has(i.id)).length;
  const allVisibleSelected = activeItems.length > 0 && activeItems.every((i) => selected.has(i.id));
  const someVisibleSelected = activeItems.some((i) => selected.has(i.id));

  // Active list. ?lijst=documenten|afschriften wins; without it Afschriften
  // is the default, except that once both lists are known and there are no
  // bank lines but there are documents, Documenten opens. That decision is
  // made once per visit so a tab never jumps while the user works.
  const lijstParam = searchParams.get("lijst");
  const explicitTab: InboxTab | null =
    lijstParam === "documenten" ? "documenten" : lijstParam === "afschriften" ? "afschriften" : null;
  const tab: InboxTab = explicitTab ?? "afschriften";
  const autoTabDecided = useRef(false);
  useEffect(() => {
    if (autoTabDecided.current || explicitTab || !eboekhoudenConnected) return;
    if (loading || classifying || !docsKnown) return;
    autoTabDecided.current = true;
    if (totalActive === 0 && docCount > 0) {
      setSearchParams({ lijst: "documenten" }, { replace: true });
    }
  }, [explicitTab, eboekhoudenConnected, loading, classifying, docsKnown, totalActive, docCount, setSearchParams]);

  const setTab = useCallback(
    (next: InboxTab) => {
      setSearchParams((prev) => {
        const params = new URLSearchParams(prev);
        params.set("lijst", next);
        return params;
      });
      track("Inbox Tab", { tab: next });
    },
    [setSearchParams],
  );

  // Not connected state
  if (!eboekhoudenConnected) {
    return (
      <Box>
        <Typography variant="h4" component="h1" sx={{ mb: 0.5 }}>
          {getGreeting()}{firstName ? `, ${firstName}` : ""}
        </Typography>
        <Typography variant="body1" color="text.secondary" sx={{ mb: 4 }}>
          Welkom bij Speedy e-Boekhouden. Verbind eerst met e-Boekhouden om je inbox te laden.
        </Typography>
        <Alert
          severity="info"
          action={
            <Button
              color="inherit"
              size="small"
              onClick={() => window.dispatchEvent(new CustomEvent("eb:connect"))}
              sx={{ fontWeight: 600, whiteSpace: "nowrap" }}
            >
              Verbinden
            </Button>
          }
        >
          <AlertTitle sx={{ fontWeight: 600 }}>Verbind met e-Boekhouden</AlertTitle>
          Log in met je e-boekhouden.nl account om je bankafschriften en uitgaven te verwerken.
          Voor je veiligheid slaan wij je wachtwoord niet op — daarom vragen we je elke sessie
          opnieuw in te loggen.
        </Alert>
      </Box>
    );
  }

  return (
    <Box>
      {/* Header: greeting + subtitle */}
      <Box sx={{ mb: 1 }}>
        <Typography variant="h4" component="h1" sx={{ mb: 0.5 }}>
          {getGreeting()}{firstName ? `, ${firstName}` : ""}
        </Typography>
        <Typography variant="body1" color="text.secondary" aria-live="polite">
          {/* Distinguish the two loading flavors so the user knows WHY
              the initial connect → classify path can take 5-30 seconds
              (it's the AI call, not a stuck request). The cached path
              still flashes briefly through "Inbox wordt geladen" which
              is fine. */}
          {loading && items.length === 0
            ? classifying
              ? "Afschriften worden geclassificeerd met AI — dit duurt even bij een nieuwe verbinding..."
              : "Inbox wordt geladen..."
            : classifying && items.length > 0
              ? "Vernieuwen…"
              : !docsKnown && totalActive === 0
                ? "Inbox wordt geladen..."
                : inboxSubtitle(totalActive, docsKnown ? docCount : 0)}
        </Typography>
      </Box>

      {/* Action bar: buttons left, summary badges right */}
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          flexWrap: "wrap",
          gap: 1.5,
          mb: 3,
          mt: 1,
        }}
      >
        {/* Left: action buttons */}
        <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
          <Button
            variant="contained"
            size="small"
            component="label"
            disabled={analyzingInvoices}
            startIcon={
              analyzingInvoices ? (
                <CircularProgress size={16} color="inherit" aria-hidden="true" />
              ) : (
                /* Upload icon */
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
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                  <polyline points="17 8 12 3 7 8" />
                  <line x1="12" y1="3" x2="12" y2="15" />
                </Box>
              )
            }
          >
            {analyzingInvoices ? "Analyseren..." : "Facturen uploaden"}
            <input
              type="file"
              accept=".pdf"
              multiple
              hidden
              onChange={(e) => {
                const files = e.target.files;
                if (!files || files.length === 0) return;
                handleInvoiceFiles(Array.from(files));
                e.target.value = "";
              }}
            />
          </Button>

          <Button
            variant="outlined"
            size="small"
            onClick={() => (tab === "documenten" ? void docActions.reload() : loadInbox(true))}
            disabled={tab === "documenten" ? docState.refreshing : classifying}
            startIcon={
              (tab === "documenten" ? docState.refreshing : classifying) ? (
                <CircularProgress size={16} color="inherit" aria-hidden="true" />
              ) : (
                /* Refresh icon */
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
                  <polyline points="23 4 23 10 17 10" />
                  <polyline points="1 20 1 14 7 14" />
                  <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
                </Box>
              )
            }
          >
            Vernieuwen
          </Button>
        </Box>

        {/* Right: summary badges (bank lines only) */}
        {tab === "afschriften" && !loading && totalActive > 0 && (
          <Box
            sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}
            role="status"
            aria-label="Overzicht inboxcategorieen"
          >
            {(Object.keys(SUMMARY_COLORS) as InboxCategory[]).map((cat) => {
              const count = summary[cat];
              if (count === 0) return null;
              const { color, bgcolor } = SUMMARY_COLORS[cat];
              return (
                <Tooltip key={cat} title={CATEGORY_TOOLTIPS[cat]} arrow>
                  <Chip
                    label={`${count} ${SUMMARY_LABELS[cat]}`}
                    size="small"
                    sx={{
                      fontWeight: 600,
                      fontSize: "0.75rem",
                      color,
                      bgcolor,
                      height: 28,
                      cursor: "help",
                    }}
                  />
                </Tooltip>
              );
            })}
          </Box>
        )}
      </Box>

      {/* List tabs: bank lines and mail-folder documents */}
      <Tabs
        value={tab}
        onChange={(_, value: InboxTab) => setTab(value)}
        variant={isXs ? "fullWidth" : "standard"}
        aria-label="Inboxlijsten"
        sx={{
          mb: 3,
          borderBottom: "1px solid",
          borderColor: "divider",
          minHeight: 44,
          "& .MuiTabs-indicator": { height: 3, borderRadius: "3px 3px 0 0" },
          "& .MuiTab-root": {
            minHeight: 44,
            textTransform: "none",
            fontWeight: 500,
            fontSize: "0.9375rem",
            "&.Mui-selected": { fontWeight: 600 },
          },
        }}
      >
        <Tab
          value="afschriften"
          id="inbox-tab-afschriften"
          aria-controls="inbox-panel-afschriften"
          aria-label={`Afschriften, ${totalActive} om te verwerken`}
          label={<TabLabel text="Afschriften" count={loading && items.length === 0 ? null : totalActive} />}
        />
        <Tab
          value="documenten"
          id="inbox-tab-documenten"
          aria-controls="inbox-panel-documenten"
          aria-label={docsKnown ? `Documenten, ${docCount} om te verwerken` : "Documenten"}
          label={<TabLabel text="Documenten" count={docsKnown ? docCount : null} />}
        />
      </Tabs>

      {/* Invoice analyze error */}
      {analyzeError && (
        <Alert severity="error" sx={{ mb: 3 }} onClose={() => setAnalyzeError(null)}>
          {analyzeError}
        </Alert>
      )}

      {/* Documenten panel — kept mounted so its state survives a switch */}
      <Box
        role="tabpanel"
        id="inbox-panel-documenten"
        aria-labelledby="inbox-tab-documenten"
        hidden={tab !== "documenten"}
      >
        <DocumentList state={docState} actions={docActions} aiStatus={apiKeyStatus} />
      </Box>

      {/* Afschriften panel */}
      <Box
        role="tabpanel"
        id="inbox-panel-afschriften"
        aria-labelledby="inbox-tab-afschriften"
        hidden={tab !== "afschriften"}
      >

      {/* AI status banner — context-dependent based on API key status */}
      {!loading && totalActive > 0 && summary.auto === 0 && summary.review === 0 && summary.invoice === 0 && apiKeyStatus && (
        <AiStatusAlert status={apiKeyStatus} variant="bank" sx={{ mb: 3 }} />
      )}

      {/* Invoice review dialog — shown after PDFs are analyzed */}
      {reviewInvoices && (
        <InvoiceReviewDialog
          open={true}
          onClose={handleReviewClose}
          analyzed={reviewInvoices}
          ledgerAccounts={ledgerAccounts ?? []}
          vatCodes={vatCodes ?? []}
          onComplete={() => loadInbox(true)}
        />
      )}

      {error && (
        <Alert severity="error" sx={{ mb: 3 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* Loading skeleton */}
      {loading && (
        <Paper variant="outlined" sx={{ borderRadius: 2, overflow: "hidden" }} aria-busy="true" role="status">
          {[...Array(6)].map((_, i) => (
            <Box
              key={i}
              sx={{
                display: "flex",
                alignItems: "center",
                gap: 2,
                px: 2.5,
                py: 2,
                borderBottom: i < 5 ? "1px solid" : "none",
                borderColor: "divider",
              }}
            >
              <Skeleton variant="rounded" width={20} height={20} />
              <Skeleton width={50} />
              <Skeleton width={70} />
              <Skeleton sx={{ flex: 1 }} />
              <Skeleton variant="rounded" width={72} height={24} sx={{ borderRadius: 3 }} />
            </Box>
          ))}
        </Paper>
      )}

      {/* Main content: filter tabs + list */}
      {!loading && totalActive > 0 && (
        <>
          {/* Filter tabs */}
          <Box
            component="nav"
            aria-label="Inboxfilters"
            sx={{ mb: 2 }}
          >
            <Tabs
              value={FILTER_TABS.findIndex((t) => t.value === filter)}
              onChange={(_, idx) => setFilter(FILTER_TABS[idx].value)}
              aria-label="Filter op categorie"
              variant="scrollable"
              scrollButtons="auto"
              sx={{
                minHeight: 40,
                "& .MuiTabs-indicator": {
                  height: 3,
                  borderRadius: "3px 3px 0 0",
                },
                "& .MuiTab-root": {
                  minHeight: 40,
                  textTransform: "none",
                  fontWeight: 500,
                  fontSize: "0.875rem",
                  "&.Mui-selected": { fontWeight: 600 },
                },
              }}
            >
              {FILTER_TABS.map((tab) => (
                <Tab key={tab.value} label={tab.label} />
              ))}
            </Tabs>
          </Box>

          {/* Item list */}
          <Paper
            variant="outlined"
            sx={{ borderRadius: 2, overflow: "hidden" }}
          >
            {/* Select-all header */}
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                gap: 2,
                px: { xs: 1.5, sm: 2.5 },
                py: 1,
                borderBottom: "1px solid",
                borderColor: "divider",
                bgcolor: "grey.50",
              }}
            >
              <Checkbox
                checked={allVisibleSelected}
                indeterminate={someVisibleSelected && !allVisibleSelected}
                onChange={toggleAll}
                size="small"
                inputProps={{
                  "aria-label": allVisibleSelected
                    ? "Deselecteer alle zichtbare items"
                    : "Selecteer alle zichtbare items",
                }}
                sx={{ p: 0.5 }}
              />
              <Typography variant="caption" color="text.secondary" fontWeight={500}>
                {filter === "all" ? "Alle items" : FILTER_TABS.find((t) => t.value === filter)?.label}
                {` (${activeItems.length})`}
              </Typography>
            </Box>

            {/* Row list — uses activeItems so successfully processed rows
                disappear immediately after Goedkeuren / Verwerken instead of
                lingering with a "Verwerkt" chip. */}
            <Box
              component="ul"
              role="list"
              aria-label="Inboxitems"
              sx={{ m: 0, p: 0 }}
            >
              {activeItems.map((item) => (
                <InboxRow
                  key={item.id}
                  item={item}
                  checked={selected.has(item.id)}
                  onToggle={toggleItem}
                  ledgerAccounts={ledgerAccounts}
                  vatCodes={vatCodes}
                  onProcessed={handleSingleProcessed}
                  onInvoiceUpload={(file, _bankLineId) => {
                    // Use the same review dialog flow as "Facturen uploaden"
                    handleInvoiceFiles([file]);
                  }}
                  processed={processedIds.has(item.id)}
                />
              ))}
            </Box>
          </Paper>
        </>
      )}

      {/* Bank lines done, documents left — point at the other tab instead
          of celebrating too early. */}
      {!loading && !classifying && totalActive === 0 && docsKnown && docCount > 0 && (
        <Paper variant="outlined" sx={{ borderRadius: 2, p: 4, textAlign: "center", mt: 3 }}>
          <Typography variant="body1" sx={{ fontWeight: 600 }} gutterBottom>
            Alle afschriftregels zijn verwerkt
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {docCount === 1 ? "Er staat nog 1 document" : `Er staan nog ${docCount} documenten`} in je mailmap.
          </Typography>
          <Button variant="outlined" size="small" onClick={() => setTab("documenten")} sx={{ fontWeight: 600 }}>
            Bekijk documenten
          </Button>
        </Paper>
      )}

      {/* Empty state — inbox zero, only when both lists are empty.
          Belt-and-suspenders against the race we hit before: NEVER show
          the celebration while a load or classification is still in
          flight, otherwise the user sees "all caught up" while Claude is
          still working through their backlog. */}
      {!loading && !classifying && totalActive === 0 && docsKnown && docCount === 0 && (
        <Paper
          variant="outlined"
          sx={{
            borderRadius: 2,
            p: 6,
            textAlign: "center",
            mt: 3,
          }}
        >
          {/* Checkmark circle icon */}
          <Box
            component="svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
            sx={{
              width: 48,
              height: 48,
              color: "success.main",
              mb: 2,
              mx: "auto",
              display: "block",
            }}
          >
            <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
            <polyline points="22 4 12 14.01 9 11.01" />
          </Box>
          <Typography variant="h6" component="p" gutterBottom>
            Inbox zero!
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Alle afschriftregels en documenten zijn verwerkt. Kom later terug of klik op Vernieuwen.
          </Typography>
        </Paper>
      )}

      {/* Batch approve bar — sticky bottom when items are selected */}
      {selectedItems.length > 0 && (
        <BatchApproveBar
          selected={selectedItems}
          onProcessed={handleBatchProcessed}
          onClear={clearSelection}
          ledgerAccounts={ledgerAccounts ?? []}
          vatCodes={vatCodes ?? []}
        />
      )}

      {/* Bottom padding to prevent batch bar from covering last row */}
      {selectedItems.length > 0 && <Box sx={{ height: 80 }} aria-hidden="true" />}

      </Box>
      {/* End of Afschriften panel */}

      {/* Feedback toast — visible long enough to be noticed, dismissible,
          announced to screen readers via the underlying Alert role. */}
      <Snackbar
        open={snackbar !== null}
        autoHideDuration={4000}
        onClose={() => setSnackbar(null)}
        anchorOrigin={{ vertical: "bottom", horizontal: "center" }}
      >
        {snackbar ? (
          <Alert
            severity={snackbar.severity}
            onClose={() => setSnackbar(null)}
            variant="filled"
            sx={{ fontWeight: 600 }}
          >
            {snackbar.message}
          </Alert>
        ) : undefined}
      </Snackbar>
    </Box>
  );
}
