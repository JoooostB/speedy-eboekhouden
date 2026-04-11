import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { VATCode } from "../api/types";

export function useVATCodes() {
  const [data, setData] = useState<VATCode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.getVATCodes()
      .then(setData)
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  return { data, loading, error };
}
