import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { LedgerAccount } from "../api/types";

export function useLedgerAccounts() {
  const [data, setData] = useState<LedgerAccount[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.getLedgerAccounts()
      .then(setData)
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  return { data, loading, error };
}
