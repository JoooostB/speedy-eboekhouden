export interface Employee {
  id: number;
  naam: string;
}

export interface Project {
  id: number;
  naam: string;
  relatieBedrijf?: string;
}

export interface Activity {
  id: number;
  naam: string;
}

export interface BulkEntry {
  employeeId: number;
  projectId: number;
  activityId: number;
  hours: string;
  dates: string[];
  description: string;
}

export interface BulkRequest {
  entries: BulkEntry[];
}

export interface EntryResult {
  employeeId: number;
  date: string;
  status: "ok" | "error";
  error?: string;
}

export interface BulkResponse {
  results: EntryResult[];
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface MFARequest {
  code: string;
}

export interface AuthResponse {
  status: "ok" | "mfa_required";
}
