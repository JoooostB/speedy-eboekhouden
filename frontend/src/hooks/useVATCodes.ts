import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { VATCode } from "../api/types";
import { useAuth } from "../context/AuthContext";

/**
 * Fetches the user's BTW codes. Re-runs whenever the e-boekhouden
 * connection becomes available — see useLedgerAccounts for the rationale.
 */
export function useVATCodes() {
  const { eboekhoudenConnected } = useAuth();
  const [data, setData] = useState<VATCode[]>([]);
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
      .getVATCodes()
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch((err) => {
        if (!cancelled) setError(err?.message || "Kon BTW-codes niet laden");
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
