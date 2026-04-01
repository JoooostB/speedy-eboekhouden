import { createContext, useContext, useState, useEffect, useCallback, type ReactNode } from "react";
import { api } from "../api/client";

interface AuthState {
  isAuthenticated: boolean;
  needsMFA: boolean;
  checking: boolean;
}

interface AuthContextType extends AuthState {
  login: (email: string, password: string) => Promise<void>;
  submitMFA: (code: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({
    isAuthenticated: false,
    needsMFA: false,
    checking: true,
  });

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } catch {
      // ignore
    }
    setState({ isAuthenticated: false, needsMFA: false, checking: false });
  }, []);

  // Check for existing session on mount
  useEffect(() => {
    api.checkSession()
      .then(() => {
        setState({ isAuthenticated: true, needsMFA: false, checking: false });
      })
      .catch(() => {
        setState({ isAuthenticated: false, needsMFA: false, checking: false });
      });
  }, []);

  useEffect(() => {
    const handler = () => {
      setState({ isAuthenticated: false, needsMFA: false, checking: false });
    };
    window.addEventListener("auth:expired", handler);
    return () => window.removeEventListener("auth:expired", handler);
  }, []);

  const login = async (email: string, password: string) => {
    const res = await api.login(email, password);
    if (res.status === "mfa_required") {
      setState({ isAuthenticated: false, needsMFA: true, checking: false });
    } else {
      setState({ isAuthenticated: true, needsMFA: false, checking: false });
    }
  };

  const submitMFA = async (code: string) => {
    await api.mfa(code);
    setState({ isAuthenticated: true, needsMFA: false, checking: false });
  };

  return (
    <AuthContext.Provider value={{ ...state, login, submitMFA, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
