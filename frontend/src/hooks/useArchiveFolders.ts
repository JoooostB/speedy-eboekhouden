import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import type { ArchiveFolder } from "../api/types";

/**
 * A flat archive folder enriched with its full breadcrumb path, e.g.
 * "Verwerkte facturen / Anthropic". The path is built by walking parentId
 * chains client-side; e-boekhouden only returns each folder's direct parent.
 */
export interface ArchiveFolderWithPath extends ArchiveFolder {
  /** Slash-separated breadcrumb including the folder's own name. */
  path: string;
}

interface State {
  folders: ArchiveFolderWithPath[];
  loading: boolean;
  /** Dutch error string suitable for surfacing to the user, or null. */
  error: string | null;
  /** Re-fetch the folder list — useful after creating a new folder so the
   *  picker reflects it without remounting. */
  refetch: () => Promise<void>;
}

/**
 * Build the breadcrumb path for a folder by walking parentId references.
 * Bounded by the number of folders so a corrupted parentId cycle can't loop.
 * Exported for unit testing — the hook itself uses the higher-level
 * enrichArchiveFolders below.
 */
export function buildArchiveFolderPath(folder: ArchiveFolder, byId: Map<number, ArchiveFolder>): string {
  const segments: string[] = [];
  let current: ArchiveFolder | undefined = folder;
  const seen = new Set<number>();
  while (current && !seen.has(current.id)) {
    seen.add(current.id);
    segments.unshift(current.naam);
    current = current.parentId === 0 ? undefined : byId.get(current.parentId);
  }
  return segments.join(" / ");
}

/**
 * Take the raw folder list from the backend and produce a UI-ready list:
 * deleted entries removed, each remaining folder enriched with its
 * breadcrumb path, sorted by path in Dutch locale. Exported for unit
 * testing.
 */
export function enrichArchiveFolders(folders: ArchiveFolder[]): ArchiveFolderWithPath[] {
  const byId = new Map(folders.map((f) => [f.id, f]));
  return folders
    .filter((f) => !f.isDeleted)
    .map((f) => ({ ...f, path: buildArchiveFolderPath(f, byId) }))
    .sort((a, b) => a.path.localeCompare(b.path, "nl"));
}

/**
 * Lowercase + strip everything that isn't a letter or digit. Used so
 * "Anthropic, PBC" and "anthropic" collapse to the same token "anthropic"
 * for fuzzy supplier→folder matching.
 */
function normalizeSupplierToken(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
}

/**
 * Find the best archive folder for an invoice supplier name. The match is
 * token-based and case-insensitive: any folder whose normalized name appears
 * as a whole word in the normalized supplier (or vice versa) counts. When
 * multiple folders match, the one with the longest name wins — that's
 * usually the most specific match (e.g. "TransIP B.V." should pick the
 * "TransIP" folder, not a shorter "T" folder).
 *
 * A minimum length of 3 characters is required so short tokens like "BV"
 * or "EU" don't produce spurious matches across the whole tree. Returns
 * null when nothing matches; the picker then stays empty so the user
 * makes an explicit choice.
 */
export function findArchiveFolderForSupplier(
  folders: ArchiveFolderWithPath[],
  supplier: string,
): ArchiveFolderWithPath | null {
  const supplierNorm = normalizeSupplierToken(supplier);
  if (supplierNorm.length < 3) return null;
  const supplierTokens = supplierNorm.split(" ").filter((t) => t.length >= 3);
  if (supplierTokens.length === 0) return null;

  let best: ArchiveFolderWithPath | null = null;
  let bestScore = 0;
  for (const folder of folders) {
    const folderNorm = normalizeSupplierToken(folder.naam);
    if (folderNorm.length < 3) continue;
    // Match if the folder name appears as a whole token in the supplier or
    // if any supplier token appears as a whole token in the folder name.
    const folderTokens = folderNorm.split(" ").filter((t) => t.length >= 3);
    const hit =
      supplierTokens.some((t) => folderTokens.includes(t)) ||
      folderTokens.some((t) => supplierTokens.includes(t));
    if (!hit) continue;
    if (folderNorm.length > bestScore) {
      best = folder;
      bestScore = folderNorm.length;
    }
  }
  return best;
}

/**
 * Fetches the e-boekhouden digitaal archief folder tree, filters out deleted
 * entries, and exposes each folder enriched with its breadcrumb path so a
 * flat Autocomplete dropdown can still show hierarchy ("Verwerkte facturen /
 * Anthropic"). Folders rarely change, so a single fetch per mount is enough.
 */
export function useArchiveFolders(): State {
  const [folders, setFolders] = useState<ArchiveFolder[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchFolders = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.getArchiveFolders();
      setFolders(data);
      setError(null);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Kon archiefmappen niet ophalen");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .getArchiveFolders()
      .then((data) => {
        if (cancelled) return;
        setFolders(data);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Kon archiefmappen niet ophalen");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const enriched = useMemo<ArchiveFolderWithPath[]>(
    () => enrichArchiveFolders(folders),
    [folders],
  );

  return { folders: enriched, loading, error, refetch: fetchFolders };
}
