import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { LedgerAccount } from "../api/types";
import { useAuth } from "../context/AuthContext";

/**
 * Fetches the user's active grootboekrekeningen. Re-runs whenever the
 * e-boekhouden connection becomes available so a page that mounted before
 * the session was restored still ends up with real data once the user
 * connects. Without this, the empty-state race caused the BookingConfirm
 * and InvoiceReview dialogs to show empty dropdowns.
 */
export function useLedgerAccounts() {
  const { eboekhoudenConnected } = useAuth();
  const [data, setData] = useState<LedgerAccount[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!eboekhoudenConnected) {
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .getLedgerAccounts()
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch((err) => {
        if (!cancelled) setError(err?.message || "Kon grootboekrekeningen niet laden");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [eboekhoudenConnected]);

  return { data, loading, error };
}
