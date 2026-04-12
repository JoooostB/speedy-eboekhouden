import { createContext, useContext, useState, useEffect, useCallback, useRef, type ReactNode } from "react";
import { api } from "../api/client";
import type { User, Team } from "../api/types";

const KEEPALIVE_INTERVAL_MS = 10 * 60 * 1000; // 10 minutes

interface AuthState {
  user: User | null;
  team: Team | null;
  eboekhoudenConnected: boolean;
  avatarUrl: string;
  checking: boolean;
}

interface AuthContextType extends AuthState {
  logout: () => Promise<void>;
  refreshMe: () => Promise<void>;
  setEBConnected: (connected: boolean) => void;
  setAvatarUrl: (url: string) => void;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({
    user: null,
    team: null,
    eboekhoudenConnected: false,
    avatarUrl: "",
    checking: true,
  });

  const keepaliveRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const refreshMe = useCallback(async () => {
    try {
      const data = await api.me();
      setState({
        user: data.user,
        team: data.team,
        eboekhoudenConnected: data.eboekhoudenConnected,
        avatarUrl: data.avatarUrl || "",
        checking: false,
      });
    } catch {
      setState({ user: null, team: null, eboekhoudenConnected: false, avatarUrl: "", checking: false });
    }
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } catch {
      // ignore
    }
    setState({ user: null, team: null, eboekhoudenConnected: false, avatarUrl: "", checking: false });
  }, []);

  const setEBConnected = useCallback((connected: boolean) => {
    setState((prev) => ({ ...prev, eboekhoudenConnected: connected }));
  }, []);

  const setAvatarUrl = useCallback((url: string) => {
    setState((prev) => ({ ...prev, avatarUrl: url }));
  }, []);

  useEffect(() => {
    refreshMe();
  }, [refreshMe]);

  useEffect(() => {
    const handler = () => {
      setState({ user: null, team: null, eboekhoudenConnected: false, avatarUrl: "", checking: false });
    };
    window.addEventListener("auth:expired", handler);
    return () => window.removeEventListener("auth:expired", handler);
  }, []);

  // The API client emits eb:session-expired when an e-boekhouden upstream call
  // fails because the cookie is no longer valid. Drop the connection flag so
  // the UI re-renders the connect prompt instead of showing a raw error.
  useEffect(() => {
    const handler = () => {
      setState((prev) => ({ ...prev, eboekhoudenConnected: false }));
    };
    window.addEventListener("eb:session-expired", handler);
    return () => window.removeEventListener("eb:session-expired", handler);
  }, []);

  // e-Boekhouden keepalive: ping every 10 minutes while connected.
  // Pauses when the tab is hidden to avoid background noise.
  useEffect(() => {
    // Clear any existing interval
    if (keepaliveRef.current) {
      clearInterval(keepaliveRef.current);
      keepaliveRef.current = null;
    }

    if (!state.eboekhoudenConnected || !state.user) {
      return;
    }

    const ping = async () => {
      // Skip if tab is hidden
      if (document.hidden) return;

      try {
        const result = await api.ebKeepalive();
        if (!result.alive) {
          setState((prev) => ({ ...prev, eboekhoudenConnected: false }));
        }
      } catch {
        // Network error — don't disconnect, just skip this cycle
      }
    };

    keepaliveRef.current = setInterval(ping, KEEPALIVE_INTERVAL_MS);

    return () => {
      if (keepaliveRef.current) {
        clearInterval(keepaliveRef.current);
        keepaliveRef.current = null;
      }
    };
  }, [state.eboekhoudenConnected, state.user]);

  return (
    <AuthContext.Provider value={{ ...state, logout, refreshMe, setEBConnected, setAvatarUrl }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
