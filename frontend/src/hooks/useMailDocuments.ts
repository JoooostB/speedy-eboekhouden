import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { track } from "../analytics";
import type {
  ArchiveDocumentsResponse,
  MailDocument,
  MailDocumentAnalyzeResponse,
  MailFolderInfo,
  OpenPostGridItem,
} from "../api/types";
import { delay, isAbortedError, runWithConcurrency } from "../utils/runWithConcurrency";
import {
  ANALYZE_BUSY_DELAY_MS,
  ANALYZE_BUSY_RETRIES,
  analyzeAllTargets,
  analyzeErrorMessage,
  applyAnalysis,
  isAnalyzeBusyError,
  isFatalAnalyzeError,
  summarizeRun,
  type AnalyzeOutcome,
  type DocumentRowState,
} from "./mailDocumentsModel";

export interface AnalyzeRunProgress {
  done: number;
  total: number;
}

/** The closing summary of a run; `at` makes two identical summaries
 *  distinguishable so the snackbar shows again. */
export interface RunSummary {
  text: string;
  at: number;
}

export interface OpenPostsState {
  loading: boolean;
  loaded: boolean;
  error: string | null;
  items: OpenPostGridItem[];
}

export interface MailDocumentsState {
  /** No list to show yet: first load, or a retry after an error. */
  loading: boolean;
  /** A reload is running behind an already shown list. */
  refreshing: boolean;
  loaded: boolean;
  error: string | null;
  configured: boolean;
  reason?: "missing";
  folder: MailFolderInfo | null;
  documents: MailDocument[];
  ignoredCount: number;
  rowStates: Record<number, DocumentRowState>;
  /** Progress of "Alles analyseren", null when no run is active. */
  run: AnalyzeRunProgress | null;
  /** The list as it was when the run started; the sections are grouped by
   *  it until the run ends so rows keep their place. */
  groupingSnapshot: MailDocument[] | null;
  runSummary: RunSummary | null;
  /** The error that stopped a run or a single analysis for good. */
  runError: string | null;
  openPosts: OpenPostsState;
}

export interface MailDocumentsActions {
  /** Fetches the list again and resolves with the fresh documents, or null
   *  when the load failed or was superseded by a newer one. Callers that
   *  need a document right after the reload read it from the result, since
   *  the state update is not visible to them yet. */
  reload: (opts?: { silent?: boolean }) => Promise<MailDocument[] | null>;
  analyze: (id: number, force?: boolean) => Promise<void>;
  analyzeAll: () => Promise<void>;
  stop: () => void;
  ignore: (id: number) => Promise<void>;
  unignore: (id: number) => Promise<void>;
  remove: (id: number) => Promise<void>;
  archive: (ids: number[], targetFolderId?: number) => Promise<ArchiveDocumentsResponse>;
  setup: (folderId?: number) => Promise<void>;
  loadOpenPosts: (force?: boolean) => Promise<void>;
  dismissRunSummary: () => void;
  dismissRunError: () => void;
}

const ANALYZE_CONCURRENCY = 3;

const initialOpenPosts: OpenPostsState = { loading: false, loaded: false, error: null, items: [] };

const initialState: MailDocumentsState = {
  loading: true,
  refreshing: false,
  loaded: false,
  error: null,
  configured: false,
  folder: null,
  documents: [],
  ignoredCount: 0,
  rowStates: {},
  run: null,
  groupingSnapshot: null,
  runSummary: null,
  runError: null,
  openPosts: initialOpenPosts,
};

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

function statusOf(err: unknown): number | undefined {
  return (err as { status?: number } | null)?.status;
}

/**
 * Calls the analyze endpoint and, when the backend says the file is already
 * being analysed (409), waits and tries again, up to ANALYZE_BUSY_RETRIES
 * attempts. The wait is cut short by the signal (AbortedError).
 */
async function analyzeWithRetry(
  id: number,
  force: boolean,
  signal: AbortSignal | undefined,
): Promise<MailDocumentAnalyzeResponse> {
  for (let attempt = 1; ; attempt++) {
    try {
      return await api.analyzeDocument(id, force);
    } catch (err) {
      if (isAnalyzeBusyError(err) && attempt < ANALYZE_BUSY_RETRIES) {
        await delay(ANALYZE_BUSY_DELAY_MS, signal);
        continue;
      }
      throw err;
    }
  }
}

/**
 * useMailDocuments — the documents inbox behind the "Documenten" tab.
 *
 * Loads the mail-folder listing as soon as e-boekhouden is connected (the
 * same trigger as the bank-line inbox) and never calls Claude on its own:
 * analysis runs per document (analyze) or for everything that is still
 * unanalysed (analyzeAll, three at a time, stoppable). During a run the rows
 * keep their place: the components group by groupingSnapshot and only after
 * the run ends, when the list has been reloaded for the global bank-line
 * assignment, is the snapshot dropped and the list regrouped.
 *
 * All list mutations are functional setState updates, because the run's
 * callbacks arrive concurrently.
 */
export function useMailDocuments(eboekhoudenConnected: boolean): { state: MailDocumentsState } & MailDocumentsActions {
  const [state, setState] = useState<MailDocumentsState>(initialState);
  // The latest state for callbacks that need to read it without being
  // recreated on every change (functional updates run later, during render,
  // so they cannot hand a value back to the caller).
  const latest = useRef(state);
  latest.current = state;
  // Ignore responses of a load that was superseded by a newer one.
  const loadSeq = useRef(0);
  // The active run's controller; null when no run is active.
  const runRef = useRef<AbortController | null>(null);

  const setRowState = useCallback((id: number, rowState: DocumentRowState | undefined) => {
    setState((s) => {
      const rowStates = { ...s.rowStates };
      if (rowState) rowStates[id] = rowState;
      else delete rowStates[id];
      return { ...s, rowStates };
    });
  }, []);

  const updateDocument = useCallback((id: number, update: (doc: MailDocument) => MailDocument) => {
    setState((s) => ({ ...s, documents: s.documents.map((d) => (d.id === id ? update(d) : d)) }));
  }, []);

  const removeDocuments = useCallback((ids: number[]) => {
    const gone = new Set(ids);
    setState((s) => {
      const removedIgnored = s.documents.filter((d) => gone.has(d.id) && d.ignored).length;
      const rowStates = { ...s.rowStates };
      for (const id of ids) delete rowStates[id];
      return {
        ...s,
        documents: s.documents.filter((d) => !gone.has(d.id)),
        ignoredCount: Math.max(0, s.ignoredCount - removedIgnored),
        rowStates,
      };
    });
  }, []);

  const reload = useCallback(async (opts?: { silent?: boolean }): Promise<MailDocument[] | null> => {
    const seq = ++loadSeq.current;
    setState((s) => ({
      ...s,
      loading: !opts?.silent && (s.documents.length === 0 || s.error !== null),
      refreshing: true,
      error: opts?.silent ? s.error : null,
    }));
    try {
      const res = await api.getDocuments();
      if (seq !== loadSeq.current) return null;
      const documents = res.documents ?? [];
      setState((s) => ({
        ...s,
        loading: false,
        refreshing: false,
        loaded: true,
        error: null,
        configured: res.configured,
        reason: res.reason,
        folder: res.folder ?? null,
        documents,
        ignoredCount: res.ignoredCount ?? 0,
      }));
      return documents;
    } catch (err) {
      if (seq !== loadSeq.current) return null;
      // An expired e-boekhouden session flips eboekhoudenConnected through
      // the API client's event; the effect below then resets this hook.
      if (statusOf(err) === 412) {
        setState((s) => ({ ...s, loading: false, refreshing: false }));
        return null;
      }
      // A failed background refresh keeps the list that is already shown.
      setState((s) => ({
        ...s,
        loading: false,
        refreshing: false,
        loaded: true,
        error: opts?.silent && s.documents.length > 0 ? s.error : errorMessage(err, "Probeer het zo opnieuw."),
      }));
      return null;
    }
  }, []);

  // Load when the connection comes up; reset when it goes away.
  useEffect(() => {
    if (!eboekhoudenConnected) {
      runRef.current?.abort();
      runRef.current = null;
      loadSeq.current++;
      setState({ ...initialState, loading: false });
      return;
    }
    setState({ ...initialState });
    void reload();
  }, [eboekhoudenConnected, reload]);

  const stop = useCallback(() => {
    runRef.current?.abort();
  }, []);

  const analyze = useCallback(
    async (id: number, force = false) => {
      setRowState(id, { kind: "analyzing" });
      try {
        const resp = await analyzeWithRetry(id, force, undefined);
        updateDocument(id, (d) => applyAnalysis(d, resp));
        setRowState(id, undefined);
        track("Document Analyzed", { status: resp.status, forced: String(force) });
        // The analyze response rates the document's own best bank line; the
        // list recomputes the assignment across all documents.
        void reload({ silent: true });
      } catch (err) {
        const message = analyzeErrorMessage(err);
        setRowState(id, { kind: "error", message });
        if (isFatalAnalyzeError(err)) {
          setState((s) => ({ ...s, runError: message }));
        }
      }
    },
    [reload, setRowState, updateDocument],
  );

  const analyzeAll = useCallback(async () => {
    if (runRef.current) return;
    const targets = analyzeAllTargets(state.documents);
    if (targets.length === 0) return;

    const controller = new AbortController();
    runRef.current = controller;
    setState((s) => {
      const rowStates = { ...s.rowStates };
      for (const id of targets) rowStates[id] = { kind: "queued" };
      return {
        ...s,
        rowStates,
        run: { done: 0, total: targets.length },
        groupingSnapshot: s.documents,
        runSummary: null,
        runError: null,
      };
    });
    track("Documents Analyze All", { total: String(targets.length) });

    const bumpDone = () =>
      setState((s) => (s.run ? { ...s, run: { ...s.run, done: Math.min(s.run.total, s.run.done + 1) } } : s));

    const results = await runWithConcurrency(
      targets,
      async (id, _index, signal) => {
        setRowState(id, { kind: "analyzing" });
        return analyzeWithRetry(id, false, signal);
      },
      {
        limit: ANALYZE_CONCURRENCY,
        signal: controller.signal,
        onSuccess: (id, resp) => {
          updateDocument(id, (d) => applyAnalysis(d, resp));
          setRowState(id, undefined);
          bumpDone();
        },
        onError: (id, err) => {
          if (isAbortedError(err)) {
            // Stopped while waiting to retry a busy file: back to how it was.
            setRowState(id, undefined);
            return;
          }
          const message = analyzeErrorMessage(err);
          setRowState(id, { kind: "error", message });
          bumpDone();
          if (isFatalAnalyzeError(err)) {
            controller.abort();
            setState((s) => ({ ...s, runError: message }));
          }
        },
      },
    );

    const outcomes: AnalyzeOutcome[] = results.map((r) => {
      if (r.status === "ok") return { kind: "ok", status: r.value.status };
      if (r.status === "error" && !isAbortedError(r.error)) {
        return { kind: "error", message: analyzeErrorMessage(r.error) };
      }
      return { kind: "aborted" };
    });
    // Rows that never started go back to their plain state.
    setState((s) => {
      const rowStates = { ...s.rowStates };
      results.forEach((r, i) => {
        if (r.status !== "ok" && rowStates[targets[i]]?.kind === "queued") delete rowStates[targets[i]];
      });
      return { ...s, rowStates };
    });

    if (runRef.current === controller) {
      // Only now the sections regroup: reload for the global assignment,
      // then drop the snapshot in the same update.
      await reload({ silent: true });
      runRef.current = null;
      setState((s) => ({
        ...s,
        run: null,
        groupingSnapshot: null,
        runSummary: { text: summarizeRun(outcomes), at: Date.now() },
      }));
    }
  }, [reload, setRowState, state.documents, updateDocument]);

  const ignore = useCallback(async (id: number) => {
    // Optimistic: hide the row at once, put it back if the backend refuses.
    const previous = latest.current.documents.find((d) => d.id === id);
    const changed = !!previous && !previous.ignored;
    if (changed) {
      setState((s) => ({
        ...s,
        ignoredCount: s.ignoredCount + 1,
        documents: s.documents.map((d) => (d.id === id ? { ...d, ignored: true, status: "ignored" } : d)),
      }));
    }
    try {
      await api.ignoreDocument(id);
      track("Document Ignored");
    } catch (err) {
      if (changed && previous) {
        setState((s) => ({
          ...s,
          ignoredCount: Math.max(0, s.ignoredCount - 1),
          documents: s.documents.map((d) => (d.id === id ? previous : d)),
        }));
      }
      throw err;
    }
  }, []);

  const unignore = useCallback(
    async (id: number) => {
      await api.unignoreDocument(id);
      track("Document Unignored");
      // The status it gets back depends on the backend's rules; reload.
      await reload({ silent: true });
    },
    [reload],
  );

  const remove = useCallback(
    async (id: number) => {
      await api.deleteDocument(id);
      track("Document Deleted");
      removeDocuments([id]);
    },
    [removeDocuments],
  );

  const archive = useCallback(
    async (ids: number[], targetFolderId?: number) => {
      const res = await api.archiveDocuments(ids, targetFolderId);
      track("Documents Archived", { moved: String(res.moved.length), failed: String(res.failed.length) });
      if (res.moved.length > 0) {
        removeDocuments(res.moved.map((m) => m.id));
      }
      return res;
    },
    [removeDocuments],
  );

  const setup = useCallback(
    async (folderId?: number) => {
      await api.setupMailFolder(folderId);
      track("Mail Folder Setup", { existing: String(!!folderId) });
      await reload();
    },
    [reload],
  );

  const loadOpenPosts = useCallback(async (force = false) => {
    const current = latest.current.openPosts;
    if (current.loading || (current.loaded && !force)) return;
    setState((s) => ({ ...s, openPosts: { ...s.openPosts, loading: true, error: null } }));
    try {
      const items = await api.getOpenPosts("Crediteur");
      setState((s) => ({ ...s, openPosts: { loading: false, loaded: true, error: null, items } }));
    } catch (err) {
      setState((s) => ({
        ...s,
        openPosts: {
          ...s.openPosts,
          loading: false,
          loaded: true,
          error: errorMessage(err, "Openstaande inkoopfacturen laden mislukt"),
        },
      }));
    }
  }, []);

  const dismissRunSummary = useCallback(() => setState((s) => ({ ...s, runSummary: null })), []);
  const dismissRunError = useCallback(() => setState((s) => ({ ...s, runError: null })), []);

  return {
    state,
    reload,
    analyze,
    analyzeAll,
    stop,
    ignore,
    unignore,
    remove,
    archive,
    setup,
    loadOpenPosts,
    dismissRunSummary,
    dismissRunError,
  };
}
