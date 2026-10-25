import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  Alert,
  Box,
  Button,
  Collapse,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  LinearProgress,
  Paper,
  Skeleton,
  Snackbar,
  Tooltip,
  Typography,
  useMediaQuery,
} from "@mui/material";
import type { MailDocument, OpenPostGridItem } from "../../api/types";
import type { MailDocumentsActions, MailDocumentsState } from "../../hooks/useMailDocuments";
import {
  ANALYZE_CONFIRM_THRESHOLD,
  analyzeAllConfirmText,
  analyzeAllTargets,
  formatMoney,
  formatShortDate,
  groupDocuments,
  nextFocusId,
  parseDate,
  sectionsFromSnapshot,
} from "../../hooks/mailDocumentsModel";
import type { ArchiveFolderWithPath } from "../../hooks/useArchiveFolders";
import { ArchiveFolderPicker } from "../shared/ArchiveFolderPicker";
import { AiStatusAlert, aiBlockedReason, type AiStatus } from "./AiStatusAlert";
import { DocumentRow } from "./DocumentRow";
import { MailFolderSetupCard } from "./MailFolderSetupCard";

interface Props {
  state: MailDocumentsState;
  actions: MailDocumentsActions;
  aiStatus: AiStatus | null;
  /** Opens the booking flow for one document. Without it, matched and
   *  unmatched rows have no "Boeken" button. */
  onBook?: (doc: MailDocument) => void;
  /** Opens the booking flow for every document that is ready. Without it
   *  the "Klaar om te boeken" section has no "Alles boeken" button. */
  onBookAll?: (docs: MailDocument[]) => void;
}

interface SnackbarState {
  message: string;
  severity: "success" | "info" | "warning" | "error";
  action?: { label: string; onClick: () => void };
}

function messageOf(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

/** Small chevron that points down, or up when open. */
function Chevron({ open }: { open: boolean }) {
  return (
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
        width: 18,
        height: 18,
        transition: "transform 0.2s",
        transform: open ? "rotate(180deg)" : "rotate(0deg)",
        "@media (prefers-reduced-motion: reduce)": { transition: "none" },
      }}
    >
      <polyline points="6 9 12 15 18 9" />
    </Box>
  );
}

/** Section heading row: a real h2, with the section's buttons wrapping
 *  under it on narrow screens. */
function SectionHeader({
  id,
  title,
  count,
  children,
  collapsible,
}: {
  id: string;
  title: string;
  count?: number;
  children?: ReactNode;
  collapsible?: { open: boolean; onToggle: () => void; controls: string };
}) {
  const label = count != null ? `${title} (${count})` : title;
  return (
    <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 1, mb: 1 }}>
      <Typography component="h2" variant="h6" id={id} sx={{ flexGrow: 1, fontSize: "1.0625rem", m: 0 }}>
        {collapsible ? (
          <Button
            onClick={collapsible.onToggle}
            aria-expanded={collapsible.open}
            aria-controls={collapsible.controls}
            endIcon={<Chevron open={collapsible.open} />}
            sx={{
              font: "inherit",
              fontWeight: "inherit",
              color: "text.primary",
              px: 1,
              py: 0.5,
              ml: -1,
              textAlign: "left",
            }}
          >
            {label}
          </Button>
        ) : (
          label
        )}
      </Typography>
      {children}
    </Box>
  );
}

function RowSkeleton({ rows }: { rows: number }) {
  return (
    <Paper variant="outlined" sx={{ borderRadius: 2, overflow: "hidden" }} aria-busy="true" role="status" aria-label="Documenten worden geladen">
      {[...Array(rows)].map((_, i) => (
        <Box
          key={i}
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 2,
            px: 2.5,
            py: 2,
            borderBottom: i < rows - 1 ? "1px solid" : "none",
            borderColor: "divider",
          }}
        >
          <Skeleton width={50} />
          <Skeleton width={70} />
          <Skeleton sx={{ flex: 1 }} />
          <Skeleton variant="rounded" width={96} height={24} sx={{ borderRadius: 3 }} />
          <Skeleton variant="rounded" width={96} height={32} />
        </Box>
      ))}
    </Paper>
  );
}

/** Read-only list of outstanding purchase invoices for "Wacht op betaling". */
function OpenPostRows({ posts }: { posts: OpenPostGridItem[] }) {
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  return (
    <Paper variant="outlined" sx={{ borderRadius: 2, overflow: "hidden" }}>
      <Box component="ul" role="list" aria-label="Openstaande inkoopfacturen" sx={{ m: 0, p: 0 }}>
        {posts.map((p, i) => {
          const due = parseDate(p.vervaldatum);
          const overdue = !!due && due < today;
          const dueText = due ? `${overdue ? "vervallen op" : "vervalt op"} ${formatShortDate(p.vervaldatum)}` : "";
          const details = [p.mutFactuur ? `Factuur ${p.mutFactuur}` : null, dueText || null].filter(Boolean).join(" · ");
          return (
            <Box
              component="li"
              key={`${p.mutNr}-${i}`}
              sx={{
                listStyle: "none",
                display: "flex",
                alignItems: "center",
                gap: 2,
                px: { xs: 1.5, sm: 2.5 },
                py: 1.25,
                borderBottom: "1px solid",
                borderColor: "divider",
                "&:last-child": { borderBottom: 0 },
              }}
            >
              <Typography variant="body2" color="text.secondary" sx={{ minWidth: 52, fontSize: "0.8125rem", flexShrink: 0 }}>
                {formatShortDate(p.mutDatum)}
              </Typography>
              <Typography
                variant="body2"
                sx={{ minWidth: 88, textAlign: "right", fontWeight: 600, fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap", flexShrink: 0 }}
              >
                {formatMoney(p.teOntvangen)}
              </Typography>
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography variant="body2" noWrap sx={{ fontWeight: 500 }}>
                  {p.bedrijf || "Onbekende relatie"}
                </Typography>
                {details && (
                  <Typography variant="caption" color={overdue ? "error.main" : "text.secondary"} noWrap sx={{ display: "block" }}>
                    {details}
                  </Typography>
                )}
              </Box>
            </Box>
          );
        })}
      </Box>
    </Paper>
  );
}

/**
 * DocumentList — the "Documenten" tab: the files in the mail folder grouped
 * into "Klaar om te boeken", "Te doen", "Al verwerkt in e-Boekhouden" and,
 * from the open-post grid, "Wacht op betaling", with ignored documents
 * behind a toggle at the bottom.
 *
 * During an "Alles analyseren" run the sections are grouped by the hook's
 * snapshot so rows only change chips; they regroup when the run ends.
 *
 * Accessibility: every section has an h2; the progress bar carries
 * aria-valuenow; only the closing summary is announced (polite); when a row
 * disappears, focus moves to the next row's primary action; collapses and
 * the progress bar follow prefers-reduced-motion.
 */
export function DocumentList({ state, actions, aiStatus, onBook, onBookAll }: Props) {
  const { analyze, analyzeAll, stop, ignore, unignore, remove, archive, setup, loadOpenPosts, dismissRunError, reload } = actions;
  const reducedMotion = useMediaQuery("(prefers-reduced-motion: reduce)");

  const [processedOpen, setProcessedOpen] = useState(false);
  const [waitingOpen, setWaitingOpen] = useState(false);
  const [showIgnored, setShowIgnored] = useState(false);
  const [confirmAllOpen, setConfirmAllOpen] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [snackbar, setSnackbar] = useState<SnackbarState | null>(null);

  const [deleteTarget, setDeleteTarget] = useState<MailDocument | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const [moveTarget, setMoveTarget] = useState<MailDocument | null>(null);
  const [moveFolder, setMoveFolder] = useState<ArchiveFolderWithPath | null>(null);
  const [moving, setMoving] = useState(false);
  const [moveError, setMoveError] = useState<string | null>(null);

  const [archivingAll, setArchivingAll] = useState(false);

  const blockedReason = aiBlockedReason(aiStatus);
  const runActive = !!state.run;

  const sections = useMemo(
    () =>
      state.groupingSnapshot
        ? sectionsFromSnapshot(state.groupingSnapshot, state.documents)
        : groupDocuments(state.documents),
    [state.groupingSnapshot, state.documents],
  );
  const targets = useMemo(() => analyzeAllTargets(state.documents), [state.documents]);
  const excludeFolderIds = useMemo(() => (state.folder ? [state.folder.id] : []), [state.folder]);

  // Focus management: when a row disappears, focus goes to the primary
  // action of the row that followed it (or preceded it, for the last row).
  const visibleIds = useMemo(
    () =>
      [
        ...sections.ready,
        ...sections.todo,
        ...(processedOpen ? sections.processed : []),
        ...(showIgnored ? sections.ignored : []),
      ].map((d) => d.id),
    [sections, processedOpen, showIgnored],
  );
  const focusTargets = useRef(new Map<number, HTMLElement>());
  const pendingFocus = useRef<number | null>(null);
  const registerFocusTarget = useCallback((id: number, el: HTMLElement | null) => {
    if (el) focusTargets.current.set(id, el);
    else focusTargets.current.delete(id);
  }, []);
  const markNextFocus = useCallback(
    (id: number) => {
      pendingFocus.current = nextFocusId(visibleIds, id);
    },
    [visibleIds],
  );
  useEffect(() => {
    const id = pendingFocus.current;
    if (id == null) return;
    pendingFocus.current = null;
    if (visibleIds.includes(id)) focusTargets.current.get(id)?.focus();
  }, [visibleIds]);

  // The closing summary of a run is the only announcement of the run.
  useEffect(() => {
    if (state.runSummary) {
      setSnackbar({ message: state.runSummary.text, severity: "success" });
    }
  }, [state.runSummary]);

  useEffect(() => {
    if (!state.run) setStopping(false);
  }, [state.run]);

  // Open posts are fetched the first time the section is expanded.
  useEffect(() => {
    if (waitingOpen) void loadOpenPosts();
  }, [waitingOpen, loadOpenPosts]);

  const handleAnalyzeAllClick = () => {
    if (targets.length > ANALYZE_CONFIRM_THRESHOLD) {
      setConfirmAllOpen(true);
    } else {
      void analyzeAll();
    }
  };

  const handleStop = () => {
    setStopping(true);
    stop();
  };

  const handleUnignore = useCallback(
    async (id: number, fromUndo = false) => {
      if (!fromUndo) markNextFocus(id);
      try {
        await unignore(id);
        setSnackbar(fromUndo ? null : { message: "Niet meer genegeerd", severity: "success" });
      } catch (err) {
        setSnackbar({ message: messageOf(err, "Ongedaan maken mislukt"), severity: "error" });
      }
    },
    [markNextFocus, unignore],
  );

  const handleIgnore = useCallback(
    async (id: number) => {
      markNextFocus(id);
      try {
        await ignore(id);
        setSnackbar({
          message: "Genegeerd",
          severity: "success",
          action: { label: "Ongedaan maken", onClick: () => void handleUnignore(id, true) },
        });
      } catch (err) {
        pendingFocus.current = null;
        setSnackbar({ message: messageOf(err, "Negeren mislukt"), severity: "error" });
      }
    },
    [handleUnignore, ignore, markNextFocus],
  );

  const reportArchive = useCallback((res: Awaited<ReturnType<typeof archive>>) => {
    const n = res.moved.length;
    const f = res.failed.length;
    const moved = `${n} ${n === 1 ? "document" : "documenten"} opgeruimd`;
    if (f === 0) {
      setSnackbar({ message: moved, severity: "success" });
    } else {
      setSnackbar({ message: `${moved}, ${f} mislukt: ${res.failed[0].error}`, severity: "warning" });
    }
  }, []);

  const handleArchiveOne = useCallback(
    async (id: number) => {
      markNextFocus(id);
      try {
        reportArchive(await archive([id]));
      } catch (err) {
        pendingFocus.current = null;
        setSnackbar({ message: messageOf(err, "Opruimen mislukt"), severity: "error" });
      }
    },
    [archive, markNextFocus, reportArchive],
  );

  const handleArchiveAll = async () => {
    const ids = sections.processed.map((d) => d.id);
    if (ids.length === 0) return;
    setArchivingAll(true);
    try {
      reportArchive(await archive(ids));
    } catch (err) {
      setSnackbar({ message: messageOf(err, "Opruimen mislukt"), severity: "error" });
    } finally {
      setArchivingAll(false);
    }
  };

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    setDeleteError(null);
    markNextFocus(deleteTarget.id);
    try {
      await remove(deleteTarget.id);
      setDeleteTarget(null);
      setSnackbar({ message: "Verwijderd uit e-Boekhouden", severity: "success" });
    } catch (err) {
      pendingFocus.current = null;
      setDeleteError(messageOf(err, "Verwijderen mislukt"));
    } finally {
      setDeleting(false);
    }
  };

  const openMove = (doc: MailDocument) => {
    setMoveTarget(doc);
    setMoveFolder(null);
    setMoveError(null);
  };

  const handleMoveConfirm = async () => {
    if (!moveTarget || !moveFolder) return;
    setMoving(true);
    setMoveError(null);
    markNextFocus(moveTarget.id);
    try {
      const res = await archive([moveTarget.id], moveFolder.id);
      if (res.failed.length > 0) {
        pendingFocus.current = null;
        setMoveError(res.failed[0].error);
      } else {
        setMoveTarget(null);
        setSnackbar({ message: `Verplaatst naar ${moveFolder.path}`, severity: "success" });
      }
    } catch (err) {
      pendingFocus.current = null;
      setMoveError(messageOf(err, "Verplaatsen mislukt"));
    } finally {
      setMoving(false);
    }
  };

  const renderRows = (docs: MailDocument[], ariaLabel: string) => (
    <Paper variant="outlined" sx={{ borderRadius: 2, overflow: "hidden" }}>
      <Box component="ul" role="list" aria-label={ariaLabel} sx={{ m: 0, p: 0 }}>
        {docs.map((doc) => (
          <DocumentRow
            key={doc.id}
            doc={doc}
            rowState={state.rowStates[doc.id]}
            aiBlockedReason={blockedReason}
            runActive={runActive}
            onAnalyze={(id, force) => void analyze(id, force)}
            onIgnore={(id) => void handleIgnore(id)}
            onUnignore={(id) => void handleUnignore(id)}
            onArchive={(id) => void handleArchiveOne(id)}
            onRequestMove={openMove}
            onRequestDelete={(d) => {
              setDeleteTarget(d);
              setDeleteError(null);
            }}
            onBook={onBook}
            focusTargetRef={registerFocusTarget}
          />
        ))}
      </Box>
    </Paper>
  );

  // ----- Loading / error / setup states -----

  if (state.loading || (!state.loaded && !state.error)) {
    return <RowSkeleton rows={5} />;
  }

  if (state.error) {
    return (
      <Alert
        severity="error"
        action={
          <Button color="inherit" size="small" onClick={() => void reload()} sx={{ fontWeight: 600 }}>
            Opnieuw
          </Button>
        }
      >
        <Typography component="span" sx={{ fontWeight: 600, display: "block" }}>
          Mailmap laden mislukt
        </Typography>
        {state.error}
      </Alert>
    );
  }

  if (!state.configured) {
    return <MailFolderSetupCard configured={false} reason={state.reason} onSetup={setup} />;
  }

  const hasWork = sections.ready.length > 0 || sections.todo.length > 0;
  const analyzeAllDisabled = targets.length === 0 || !!blockedReason || runActive;
  const analyzeAllTooltip = blockedReason ?? (runActive ? "Er loopt al een analyse" : targets.length === 0 ? "Er is niets meer te analyseren" : "");

  return (
    <Box>
      {state.runError && (
        <Alert severity="warning" sx={{ mb: 3 }} onClose={dismissRunError}>
          <Typography component="span" sx={{ fontWeight: 600, display: "block" }}>
            Analyse gestopt
          </Typography>
          {state.runError}
        </Alert>
      )}

      <AiStatusAlert status={aiStatus} variant="documents" sx={{ mb: 3 }} />

      {state.documents.length === 0 && (
        <Box sx={{ mb: 3 }}>
          <MailFolderSetupCard configured reason={state.reason} folderName={state.folder?.name} onSetup={setup} />
        </Box>
      )}

      {/* 1. Klaar om te boeken */}
      {sections.ready.length > 0 && (
        <Box component="section" aria-labelledby="documents-ready-heading" sx={{ mb: 3 }}>
          <SectionHeader id="documents-ready-heading" title="Klaar om te boeken" count={sections.ready.length}>
            {onBookAll && (
              <Button
                variant="contained"
                size="small"
                onClick={() => onBookAll(sections.ready)}
                disabled={runActive}
                sx={{ fontWeight: 600 }}
              >
                Alles boeken ({sections.ready.length})
              </Button>
            )}
          </SectionHeader>
          {renderRows(sections.ready, "Documenten klaar om te boeken")}
        </Box>
      )}

      {/* 2. Te doen */}
      {(sections.todo.length > 0 || runActive) && (
        <Box component="section" aria-labelledby="documents-todo-heading" sx={{ mb: 3 }}>
          <SectionHeader id="documents-todo-heading" title="Te doen" count={sections.todo.length}>
            {!runActive && (
              <Box sx={{ display: "flex", flexDirection: "column", alignItems: { xs: "flex-start", sm: "flex-end" }, gap: 0.25 }}>
                <Tooltip title={analyzeAllDisabled ? analyzeAllTooltip : ""} arrow disableHoverListener={!analyzeAllDisabled} disableFocusListener={!analyzeAllDisabled}>
                  <Box component="span" sx={{ display: "inline-flex" }}>
                    <Button
                      variant="outlined"
                      size="small"
                      onClick={handleAnalyzeAllClick}
                      disabled={analyzeAllDisabled}
                      sx={{ fontWeight: 600 }}
                    >
                      Alles analyseren ({targets.length})
                    </Button>
                  </Box>
                </Tooltip>
                <Typography variant="caption" color="text.secondary">
                  Ongeveer € 0,03 per document, van je eigen tegoed bij Anthropic
                </Typography>
              </Box>
            )}
          </SectionHeader>

          {state.run && (
            <Box sx={{ display: "flex", alignItems: "center", gap: 2, mb: 1.5 }}>
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography variant="body2" sx={{ mb: 0.5 }}>
                  Analyseren {Math.min(state.run.done + 1, state.run.total)} van {state.run.total}…
                </Typography>
                <LinearProgress
                  variant="determinate"
                  value={state.run.total > 0 ? (state.run.done / state.run.total) * 100 : 0}
                  aria-label="Voortgang van de analyse"
                  aria-valuenow={state.run.done}
                  aria-valuemin={0}
                  aria-valuemax={state.run.total}
                  sx={{
                    borderRadius: 2,
                    height: 6,
                    "& .MuiLinearProgress-bar": reducedMotion ? { transition: "none" } : undefined,
                  }}
                />
              </Box>
              <Button variant="outlined" size="small" onClick={handleStop} disabled={stopping} sx={{ fontWeight: 600, flexShrink: 0 }}>
                {stopping ? "Stoppen…" : "Stoppen"}
              </Button>
            </Box>
          )}

          {sections.todo.length > 0 && renderRows(sections.todo, "Documenten om te verwerken")}
        </Box>
      )}

      {state.documents.length > 0 && !hasWork && !runActive && (
        <Paper variant="outlined" sx={{ borderRadius: 2, p: 3, mb: 3, textAlign: "center" }}>
          <Typography variant="body2" color="text.secondary">
            Geen documenten om te verwerken.
          </Typography>
        </Paper>
      )}

      {/* 3. Al verwerkt in e-Boekhouden */}
      {sections.processed.length > 0 && (
        <Box component="section" aria-labelledby="documents-processed-heading" sx={{ mb: 3 }}>
          <SectionHeader
            id="documents-processed-heading"
            title="Al verwerkt in e-Boekhouden"
            count={sections.processed.length}
            collapsible={{ open: processedOpen, onToggle: () => setProcessedOpen((v) => !v), controls: "documents-processed-panel" }}
          >
            <Button
              variant="outlined"
              size="small"
              onClick={() => void handleArchiveAll()}
              disabled={archivingAll || runActive}
              aria-label={`Alle ${sections.processed.length} verwerkte documenten opruimen`}
              sx={{ fontWeight: 600 }}
            >
              {archivingAll ? "Opruimen…" : "Alles opruimen"}
            </Button>
          </SectionHeader>
          <Collapse in={processedOpen} timeout={reducedMotion ? 0 : "auto"}>
            <Box id="documents-processed-panel">
              {renderRows(sections.processed, "Documenten die al in e-Boekhouden verwerkt zijn")}
            </Box>
          </Collapse>
        </Box>
      )}

      {/* 4. Wacht op betaling */}
      <Box component="section" aria-labelledby="documents-waiting-heading" sx={{ mb: 3 }}>
        <SectionHeader
          id="documents-waiting-heading"
          title="Wacht op betaling"
          count={state.openPosts.loaded && !state.openPosts.error ? state.openPosts.items.length : undefined}
          collapsible={{ open: waitingOpen, onToggle: () => setWaitingOpen((v) => !v), controls: "documents-waiting-panel" }}
        />
        <Collapse in={waitingOpen} timeout={reducedMotion ? 0 : "auto"}>
          <Box id="documents-waiting-panel">
            {state.openPosts.loading && <RowSkeleton rows={3} />}
            {!state.openPosts.loading && state.openPosts.error && (
              <Alert
                severity="error"
                action={
                  <Button color="inherit" size="small" onClick={() => void loadOpenPosts(true)} sx={{ fontWeight: 600 }}>
                    Opnieuw
                  </Button>
                }
              >
                {state.openPosts.error}
              </Alert>
            )}
            {!state.openPosts.loading && !state.openPosts.error && state.openPosts.loaded && (
              state.openPosts.items.length === 0 ? (
                <Paper variant="outlined" sx={{ borderRadius: 2, p: 3, textAlign: "center" }}>
                  <Typography variant="body2" color="text.secondary">
                    Geen openstaande inkoopfacturen.
                  </Typography>
                </Paper>
              ) : (
                <>
                  <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 1 }}>
                    Inkoopfacturen die in e-Boekhouden nog openstaan. Verschijnt de betaling op je afschrift, dan stelt Speedy voor om die te koppelen.
                  </Typography>
                  <OpenPostRows posts={state.openPosts.items} />
                </>
              )
            )}
          </Box>
        </Collapse>
      </Box>

      {/* Ignored documents */}
      {sections.ignored.length > 0 && (
        <Box component="section" aria-labelledby="documents-ignored-heading" sx={{ mb: 3 }}>
          <Button
            variant="text"
            size="small"
            onClick={() => setShowIgnored((v) => !v)}
            aria-expanded={showIgnored}
            aria-controls="documents-ignored-panel"
            endIcon={<Chevron open={showIgnored} />}
            sx={{ fontWeight: 600, color: "text.secondary", px: 1, ml: -1 }}
          >
            {showIgnored ? "Verberg genegeerd" : "Toon genegeerd"} ({sections.ignored.length})
          </Button>
          <Collapse in={showIgnored} timeout={reducedMotion ? 0 : "auto"}>
            <Box id="documents-ignored-panel" sx={{ mt: 1 }}>
              <Typography component="h2" variant="h6" id="documents-ignored-heading" sx={{ fontSize: "1.0625rem", mb: 1 }}>
                Genegeerd ({sections.ignored.length})
              </Typography>
              {renderRows(sections.ignored, "Genegeerde documenten")}
            </Box>
          </Collapse>
        </Box>
      )}

      {/* Confirm a large "Alles analyseren" */}
      <Dialog open={confirmAllOpen} onClose={() => setConfirmAllOpen(false)} aria-labelledby="analyze-all-title">
        <DialogTitle id="analyze-all-title">Alles analyseren</DialogTitle>
        <DialogContent>
          <DialogContentText>{analyzeAllConfirmText(targets.length)}</DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmAllOpen(false)} autoFocus>
            Annuleren
          </Button>
          <Button
            variant="contained"
            onClick={() => {
              setConfirmAllOpen(false);
              void analyzeAll();
            }}
          >
            Analyseren
          </Button>
        </DialogActions>
      </Dialog>

      {/* Confirm a delete */}
      <Dialog
        open={!!deleteTarget}
        onClose={() => !deleting && setDeleteTarget(null)}
        aria-labelledby="delete-document-title"
        aria-describedby="delete-document-text"
      >
        <DialogTitle id="delete-document-title">Verwijderen uit e-Boekhouden</DialogTitle>
        <DialogContent>
          {deleteTarget && (
            <Typography variant="body2" sx={{ fontWeight: 600, mb: 1 }}>
              {deleteTarget.title}
            </Typography>
          )}
          <DialogContentText id="delete-document-text">
            Speedy verwijdert dit bestand definitief uit je digitale archief in e-Boekhouden. Dat kun je niet ongedaan maken.
          </DialogContentText>
          {deleteError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {deleteError}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteTarget(null)} disabled={deleting} autoFocus>
            Annuleren
          </Button>
          <Button variant="contained" color="error" onClick={() => void handleDeleteConfirm()} disabled={deleting}>
            {deleting ? "Verwijderen…" : "Verwijderen"}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Move to another folder */}
      <Dialog
        open={!!moveTarget}
        onClose={() => !moving && setMoveTarget(null)}
        aria-labelledby="move-document-title"
        fullWidth
        maxWidth="sm"
      >
        <DialogTitle id="move-document-title">Verplaatsen naar…</DialogTitle>
        <DialogContent>
          {moveTarget && (
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              {moveTarget.title}
            </Typography>
          )}
          <ArchiveFolderPicker
            value={moveFolder}
            onChange={setMoveFolder}
            label="Doelmap"
            helperText="Het bestand krijgt een archiefnaam en verlaat je mailmap."
            excludeFolderIds={excludeFolderIds}
            disabled={moving}
            autoFocus
          />
          {moveError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {moveError}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setMoveTarget(null)} disabled={moving}>
            Annuleren
          </Button>
          <Button variant="contained" onClick={() => void handleMoveConfirm()} disabled={!moveFolder || moving}>
            {moving ? "Verplaatsen…" : "Verplaatsen"}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Feedback — polite: a status, not an alert, so it never interrupts. */}
      <Snackbar
        open={snackbar !== null}
        autoHideDuration={snackbar?.action ? 8000 : 5000}
        onClose={(_, reason) => {
          if (reason === "clickaway") return;
          setSnackbar(null);
        }}
        anchorOrigin={{ vertical: "bottom", horizontal: "center" }}
      >
        {snackbar ? (
          <Alert
            severity={snackbar.severity}
            onClose={() => setSnackbar(null)}
            variant="filled"
            role="status"
            aria-live="polite"
            sx={{ fontWeight: 600, alignItems: "center" }}
            action={
              snackbar.action ? (
                <Button color="inherit" size="small" onClick={snackbar.action.onClick} sx={{ fontWeight: 700 }}>
                  {snackbar.action.label}
                </Button>
              ) : undefined
            }
          >
            {snackbar.message}
          </Alert>
        ) : undefined}
      </Snackbar>
    </Box>
  );
}
