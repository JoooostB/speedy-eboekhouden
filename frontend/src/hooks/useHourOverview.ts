import { useEffect, useState } from "react";
import { api } from "../api/client";
import { useAuth } from "../context/AuthContext";
import type { HourOverviewEntry } from "../api/types";
import { buildHourOverlay, type HourOverlay } from "../components/hours/hourOverlay";

interface UseHourOverviewArgs {
  from: string | null; // YYYY-MM-DD or null to skip fetching
  to: string | null;
  employeeIds: number[]; // empty = no employee filter (all)
}

/** Fetches previously-booked hour entries for the visible calendar range
 *  and aggregates them into the lookup shape the calendar / submit dialog
 *  need (byDate totals, strict-match key set, per-date details).
 *
 *  Re-runs whenever the range or selected employees change. When multiple
 *  employees are selected we issue one query per employee in parallel —
 *  the e-boekhouden endpoint accepts only a single medewerkerId filter
 *  per call. */
export function useHourOverview(args: UseHourOverviewArgs): {
  overlay: HourOverlay;
  loading: boolean;
  error: string | null;
  refresh: () => void;
} {
  const { eboekhoudenConnected } = useAuth();
  const { from, to, employeeIds } = args;
  const employeesKey = employeeIds.slice().sort((a, b) => a - b).join(",");

  const [overlay, setOverlay] = useState<HourOverlay>(() => buildHourOverlay([]));
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);

  useEffect(() => {
    if (!eboekhoudenConnected || !from || !to) {
      setOverlay(buildHourOverlay([]));
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);

    const fetches: Promise<HourOverviewEntry[]>[] = employeeIds.length === 0
      ? [api.getHoursOverview({ from, to }).then((r) => r.entries)]
      : employeeIds.map((id) =>
          api.getHoursOverview({ from, to, employeeId: id }).then((r) => r.entries),
        );

    Promise.all(fetches)
      .then((batches) => {
        if (cancelled) return;
        const all = batches.flat();
        setOverlay(buildHourOverlay(all));
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err?.message || "Eerder geboekte uren kunnen niet worden geladen");
        setOverlay(buildHourOverlay([]));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [eboekhoudenConnected, from, to, employeesKey, refreshTick]);

  return {
    overlay,
    loading,
    error,
    refresh: () => setRefreshTick((n) => n + 1),
  };
}
