import { ApiError } from "./api/client";

declare global {
  interface Window {
    plausible: (
      event: string,
      options?: { props?: Record<string, string> },
    ) => void;
  }
}

/**
 * Analytics policy
 *
 * An event carries at most enum-like values, counts and flags. Never free
 * text: no error messages, names, amounts, descriptions or file names.
 * sanitizeTrackProps enforces that at runtime (a value that is not a token,
 * a number or a boolean is dropped) and analytics.test.ts scans every
 * track() call for the usual leaks. errorClass turns an error into a token.
 * The backend has the same rule for logs: see handler/logging.go.
 */

/** A value a tracked event may carry: an enum-like token, a count or a flag. */
export type TrackValue = string | number | boolean;
export type TrackProps = Record<string, TrackValue>;

/** Enum-like token: short, no whitespace; letters, digits and _ . : - only. */
const ENUM_TOKEN = /^[A-Za-z0-9_.:-]{1,40}$/;

/**
 * Reduces props to what Plausible may receive: booleans as "true"/"false",
 * finite numbers rounded to integers, strings only when they look like an
 * enum token. Everything else is dropped.
 */
export function sanitizeTrackProps(props: TrackProps): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(props)) {
    if (typeof value === "boolean") {
      out[key] = String(value);
    } else if (typeof value === "number") {
      if (Number.isFinite(value)) out[key] = String(Math.round(value));
    } else if (typeof value === "string" && ENUM_TOKEN.test(value)) {
      out[key] = value;
    }
  }
  return out;
}

export function track(event: string, props?: TrackProps) {
  if (typeof window.plausible === "function") {
    const clean = props ? sanitizeTrackProps(props) : undefined;
    window.plausible(event, clean && Object.keys(clean).length > 0 ? { props: clean } : undefined);
  }
}

/**
 * The class of a failure for analytics: the backend's machine-readable code
 * when it is one (e.g. "no_api_key"), otherwise the HTTP status for an
 * ApiError; the Error or DOMException name for anything else (e.g.
 * "NotAllowedError" from WebAuthn). Never the message.
 */
export function errorClass(err: unknown): string {
  if (err instanceof ApiError) {
    return err.code && ENUM_TOKEN.test(err.code) ? err.code : `http_${err.status}`;
  }
  if (err instanceof Error && err.name && ENUM_TOKEN.test(err.name)) {
    return err.name;
  }
  return "unknown";
}
