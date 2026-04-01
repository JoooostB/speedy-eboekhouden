import { useState, useEffect } from "react";
import { api } from "../api/client";
import type { Activity } from "../api/types";

export function useActivities() {
  const [activities, setActivities] = useState<Activity[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .getActivities()
      .then(setActivities)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  return { activities, loading, error };
}
