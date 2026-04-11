import { useState, useEffect, useCallback, useMemo } from "react";
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
} from "@mui/material";
import { api } from "../../api/client";
import { track } from "../../analytics";
import { useAuth } from "../../context/AuthContext";
import { useLedgerAccounts } from "../../hooks/useLedgerAccounts";
import { useVATCodes } from "../../hooks/useVATCodes";
import type { InboxClassification, InboxCategory, InboxProcessResult, InvoiceAnalyzeResponse } from "../../api/types";
import { InboxRow } from "./InboxRow";
import { BatchApproveBar } from "./BatchApproveBar";
import { InvoiceReviewDialog } from "./InvoiceReviewDialog";

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
 * BookkeepingInbox — the core "todo list" for unprocessed bank lines.
 *
 * This replaces the old bank statement list + invoice upload with a unified
 * workflow. AI classifies every unprocessed bank line into one of four
 * categories (auto, review, invoice, manual) and the user works through
 * them toward inbox zero.
 *
 * Layout:
 * - Greeting + summary bar with colored category badges
 * - Filter tabs to narrow by category
 * - List of InboxRow components with checkboxes for batch selection
 * - Floating BatchApproveBar when items are selected
 *
 * Accessibility:
 * - Semantic structure: <main> landmark (provided by Layout), h1 heading, <nav> for tabs
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
  const [reviewInvoices, setReviewInvoices] = useState<InvoiceAnalyzeResponse[] | null>(null);
  const [apiKeyStatus, setApiKeyStatus] = useState<{ status: string; message?: string } | null>(null);

  const firstName = getFirstName(user?.name);

  // Shared handler for analyzing invoice files (used by both upload button and InboxRow)
  const handleInvoiceFiles = useCallback(async (files: File[]) => {
    setAnalyzingInvoices(true);
    setAnalyzeError(null);

    const analyzed: InvoiceAnalyzeResponse[] = [];
    for (const file of files) {
      try {
        const res = await api.analyzeInvoice(file);
        analyzed.push(res);
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

  // Load inbox — try AI classification first, fall back to raw bank lines
  const loadInbox = useCallback(async (force = false) => {
    if (!eboekhoudenConnected) {
      setLoading(false);
      return;
    }

    setClassifying(true);
    setError(null);

    try {
      // Try AI classification (force=true skips Redis cache)
      const res = await api.classifyInbox(force);
      setItems(res.classifications);
      setSummary(res.summary);
      track("Inbox Classify", { total: String(res.totalCount) });
    } catch {
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
      } catch (err2) {
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
    results.forEach((result, id) => {
      if (result.status === "ok") newProcessed.add(id);
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
          {loading
            ? "Inbox wordt geladen..."
            : totalActive === 0
              ? "Je inbox is leeg. Alles is verwerkt!"
              : `${totalActive} ${totalActive === 1 ? "afschriftregel" : "afschriftregels"} om te verwerken`}
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
            onClick={() => loadInbox(true)}
            disabled={classifying}
            startIcon={
              classifying ? (
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

        {/* Right: summary badges */}
        {!loading && totalActive > 0 && (
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

      {/* AI status banner — context-dependent based on API key status */}
      {!loading && totalActive > 0 && summary.auto === 0 && summary.review === 0 && summary.invoice === 0 && apiKeyStatus && (
        <Alert
          severity={apiKeyStatus.status === "no_credits" ? "warning" : apiKeyStatus.status === "active" ? "info" : "info"}
          sx={{ mb: 3 }}
          action={
            apiKeyStatus.status === "not_configured" ? (
              <Button color="inherit" size="small" href="/app/instellingen" sx={{ fontWeight: 600 }}>
                Instellen
              </Button>
            ) : apiKeyStatus.status === "no_credits" ? (
              <Button
                color="inherit" size="small"
                href="https://console.anthropic.com/settings/plans"
                target="_blank" rel="noopener noreferrer"
                sx={{ fontWeight: 600 }}
              >
                Tegoed opwaarderen
              </Button>
            ) : undefined
          }
        >
          <AlertTitle sx={{ fontWeight: 600 }}>
            {apiKeyStatus.status === "not_configured" && "AI-classificatie is niet actief"}
            {apiKeyStatus.status === "no_credits" && "AI-tegoed is op"}
            {apiKeyStatus.status === "invalid" && "API-sleutel is ongeldig"}
            {apiKeyStatus.status === "active" && "AI is beschikbaar — klik op 'Vernieuwen' om te classificeren"}
            {apiKeyStatus.status === "error" && "AI-status kon niet worden gecontroleerd"}
          </AlertTitle>
          {apiKeyStatus.status === "not_configured" && (
            "Met een Anthropic API-sleutel classificeert Speedy je afschriftregels automatisch. Bankkosten, abonnementen en leveranciersbetalingen worden herkend en met een klik verwerkt."
          )}
          {apiKeyStatus.status === "no_credits" && (
            "Je API-sleutel is geldig, maar je tegoed bij Anthropic is op. Waardeer je tegoed op om AI-classificatie te hervatten."
          )}
          {apiKeyStatus.status === "invalid" && (
            "Controleer je API-sleutel in de instellingen."
          )}
        </Alert>
      )}

      {/* Invoice analyze error */}
      {analyzeError && (
        <Alert severity="error" sx={{ mb: 3 }} onClose={() => setAnalyzeError(null)}>
          {analyzeError}
        </Alert>
      )}

      {/* Invoice review dialog — shown after PDFs are analyzed */}
      {reviewInvoices && (
        <InvoiceReviewDialog
          open={true}
          onClose={() => setReviewInvoices(null)}
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

            {/* Row list */}
            <Box
              component="ul"
              role="list"
              aria-label="Inboxitems"
              sx={{ m: 0, p: 0 }}
            >
              {filteredItems.map((item) => (
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

      {/* Empty state — inbox zero */}
      {!loading && totalActive === 0 && items.length >= 0 && (
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
            Alle bankafschriften zijn verwerkt. Kom later terug of klik op Vernieuwen.
          </Typography>
        </Paper>
      )}

      {/* Batch approve bar — sticky bottom when items are selected */}
      {selectedItems.length > 0 && (
        <BatchApproveBar
          selected={selectedItems}
          onProcessed={handleBatchProcessed}
          onClear={clearSelection}
        />
      )}

      {/* Bottom padding to prevent batch bar from covering last row */}
      {selectedItems.length > 0 && <Box sx={{ height: 80 }} aria-hidden="true" />}
    </Box>
  );
}
