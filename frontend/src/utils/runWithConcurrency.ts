/**
 * Small, dependency-free concurrency helpers for client-side batch work such
 * as "Alles analyseren" in the documents inbox. Pure: no React, no DOM.
 */

/** Outcome of one item in runWithConcurrency, aligned with the input order. */
export type ConcurrencyResult<R> =
  | { status: "ok"; value: R }
  | { status: "error"; error: unknown }
  /** The item never started because the signal was aborted first. */
  | { status: "aborted" };

export interface RunWithConcurrencyOptions<T, R> {
  /** Maximum number of workers in flight at once. Default 3. */
  limit?: number;
  /** Aborting stops new items from starting; items already running are
   *  left to finish (they receive the same signal to bail out early). */
  signal?: AbortSignal;
  onStart?: (item: T, index: number) => void;
  onSuccess?: (item: T, value: R, index: number) => void;
  onError?: (item: T, error: unknown, index: number) => void;
}

/** Thrown by delay() when its signal is aborted before the time is up. */
export class AbortedError extends Error {
  constructor() {
    super("aborted");
    this.name = "AbortedError";
  }
}

export function isAbortedError(err: unknown): boolean {
  return err instanceof AbortedError || (err instanceof Error && err.name === "AbortError");
}

/**
 * Resolves after ms milliseconds, or rejects with AbortedError as soon as
 * the signal is aborted (immediately when it already is).
 */
export function delay(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new AbortedError());
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(new AbortedError());
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

/**
 * Runs worker over items with at most `limit` in flight. Items start in
 * input order; results come back in input order. A failing worker only
 * fails its own item, the others keep going. Once the signal is aborted no
 * further item starts and the remaining ones resolve as "aborted".
 */
export async function runWithConcurrency<T, R>(
  items: readonly T[],
  worker: (item: T, index: number, signal: AbortSignal | undefined) => Promise<R>,
  options: RunWithConcurrencyOptions<T, R> = {},
): Promise<ConcurrencyResult<R>[]> {
  const limit = Math.max(1, Math.floor(options.limit ?? 3));
  const { signal, onStart, onSuccess, onError } = options;
  const results: ConcurrencyResult<R>[] = new Array(items.length);
  let next = 0;

  const runOne = async (): Promise<void> => {
    while (next < items.length) {
      const index = next++;
      const item = items[index];
      if (signal?.aborted) {
        results[index] = { status: "aborted" };
        continue;
      }
      onStart?.(item, index);
      try {
        const value = await worker(item, index, signal);
        results[index] = { status: "ok", value };
        onSuccess?.(item, value, index);
      } catch (error) {
        results[index] = { status: "error", error };
        onError?.(item, error, index);
      }
    }
  };

  const workers: Promise<void>[] = [];
  for (let i = 0; i < Math.min(limit, items.length); i++) {
    workers.push(runOne());
  }
  await Promise.all(workers);
  return results;
}
