import type {
  AuthResponse,
  BulkRequest,
  BulkResponse,
  Employee,
  Project,
  Activity,
} from "./types";

const BASE = "/api/v1";

class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(
  path: string,
  options?: RequestInit,
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    ...options,
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    const message = body.error || res.statusText;

    // Only treat 401 as session expiry for authenticated endpoints, not login/mfa
    if (res.status === 401 && path !== "/login" && path !== "/mfa" && path !== "/me") {
      window.dispatchEvent(new CustomEvent("auth:expired"));
    }

    throw new ApiError(res.status, message);
  }

  return res.json();
}

export const api = {
  login(email: string, password: string) {
    return request<AuthResponse>("/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
  },

  mfa(code: string) {
    return request<AuthResponse>("/mfa", {
      method: "POST",
      body: JSON.stringify({ code }),
    });
  },

  logout() {
    return request<{ status: string }>("/logout", { method: "POST" });
  },

  checkSession() {
    return request<{ status: string }>("/me");
  },

  getEmployees() {
    return request<Employee[]>("/employees");
  },

  getProjects() {
    return request<Project[]>("/projects");
  },

  getActivities() {
    return request<Activity[]>("/activities");
  },

  submitHours(data: BulkRequest) {
    return request<BulkResponse>("/hours", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },
};

export { ApiError };
