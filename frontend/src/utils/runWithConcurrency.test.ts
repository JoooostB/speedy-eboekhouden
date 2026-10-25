import { afterEach, describe, expect, it, vi } from "vitest";
import { AbortedError, delay, isAbortedError, runWithConcurrency } from "./runWithConcurrency";

/** A promise the test resolves or rejects by hand. */
function deferred<T = void>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/** Lets queued microtasks run so workers can pick up their next item. */
const flush = () => new Promise<void>((r) => setTimeout(r, 0));

describe("runWithConcurrency", () => {
  it("returns results in input order even when items finish out of order", async () => {
    const gates = [deferred<string>(), deferred<string>(), deferred<string>()];
    const run = runWithConcurrency([0, 1, 2], (i) => gates[i].promise, { limit: 3 });

    gates[2].resolve("c");
    gates[0].resolve("a");
    gates[1].resolve("b");

    expect(await run).toEqual([
      { status: "ok", value: "a" },
      { status: "ok", value: "b" },
      { status: "ok", value: "c" },
    ]);
  });

  it("starts items in input order", async () => {
    const started: number[] = [];
    await runWithConcurrency(
      [10, 20, 30, 40, 50],
      async (item) => {
        started.push(item);
        return item;
      },
      { limit: 2 },
    );
    expect(started).toEqual([10, 20, 30, 40, 50]);
  });

  it("never has more than `limit` workers in flight", async () => {
    const gates = Array.from({ length: 7 }, () => deferred());
    let inFlight = 0;
    let maxInFlight = 0;
    const run = runWithConcurrency(
      gates.map((_, i) => i),
      async (i) => {
        inFlight++;
        maxInFlight = Math.max(maxInFlight, inFlight);
        await gates[i].promise;
        inFlight--;
        return i;
      },
      { limit: 3 },
    );

    await flush();
    expect(inFlight).toBe(3);

    for (const g of gates) {
      g.resolve();
      await flush();
    }
    await run;
    expect(maxInFlight).toBe(3);
  });

  it("uses a limit of 3 by default", async () => {
    const gates = Array.from({ length: 5 }, () => deferred());
    let inFlight = 0;
    const run = runWithConcurrency(
      gates.map((_, i) => i),
      async (i) => {
        inFlight++;
        await gates[i].promise;
        inFlight--;
      },
    );
    await flush();
    expect(inFlight).toBe(3);
    gates.forEach((g) => g.resolve());
    await run;
  });

  it("keeps the other items going when one worker throws", async () => {
    const boom = new Error("boom");
    const errors: Array<[number, unknown]> = [];
    const results = await runWithConcurrency(
      [1, 2, 3, 4],
      async (i) => {
        if (i === 2) throw boom;
        return i * 10;
      },
      { limit: 2, onError: (item, error) => errors.push([item, error]) },
    );
    expect(results).toEqual([
      { status: "ok", value: 10 },
      { status: "error", error: boom },
      { status: "ok", value: 30 },
      { status: "ok", value: 40 },
    ]);
    expect(errors).toEqual([[2, boom]]);
  });

  it("stops starting new items after abort and marks them aborted", async () => {
    const controller = new AbortController();
    const gates = Array.from({ length: 6 }, () => deferred<number>());
    const started: number[] = [];
    const run = runWithConcurrency(
      gates.map((_, i) => i),
      (i) => gates[i].promise,
      { limit: 2, signal: controller.signal, onStart: (item) => started.push(item) },
    );

    await flush();
    expect(started).toEqual([0, 1]);

    controller.abort();
    gates[0].resolve(100);
    gates[1].resolve(101);

    const results = await run;
    expect(started).toEqual([0, 1]);
    expect(results).toEqual([
      { status: "ok", value: 100 },
      { status: "ok", value: 101 },
      { status: "aborted" },
      { status: "aborted" },
      { status: "aborted" },
      { status: "aborted" },
    ]);
  });

  it("marks every item aborted when the signal is already aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    const worker = vi.fn(async (i: number) => i);
    const results = await runWithConcurrency([1, 2], worker, { signal: controller.signal });
    expect(worker).not.toHaveBeenCalled();
    expect(results).toEqual([{ status: "aborted" }, { status: "aborted" }]);
  });

  it("hands the signal to the worker so it can bail out early", async () => {
    const controller = new AbortController();
    let seen: AbortSignal | undefined;
    await runWithConcurrency(
      [1],
      async (_item, _index, signal) => {
        seen = signal;
      },
      { signal: controller.signal },
    );
    expect(seen).toBe(controller.signal);
  });

  it("calls onStart and onSuccess with the item and its index", async () => {
    const calls: string[] = [];
    await runWithConcurrency(
      ["a", "b"],
      async (item) => item.toUpperCase(),
      {
        limit: 1,
        onStart: (item, index) => calls.push(`start ${item}@${index}`),
        onSuccess: (item, value, index) => calls.push(`ok ${item}=${value}@${index}`),
      },
    );
    expect(calls).toEqual(["start a@0", "ok a=A@0", "start b@1", "ok b=B@1"]);
  });

  it("resolves to an empty array for no items", async () => {
    expect(await runWithConcurrency([], async () => 1)).toEqual([]);
  });
});

describe("delay", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("resolves after the given time", async () => {
    vi.useFakeTimers();
    let done = false;
    const p = delay(1500).then(() => {
      done = true;
    });
    await vi.advanceTimersByTimeAsync(1499);
    expect(done).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    await p;
    expect(done).toBe(true);
  });

  it("rejects with AbortedError when aborted while waiting", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const p = delay(1500, controller.signal);
    const caught = p.catch((e) => e);
    await vi.advanceTimersByTimeAsync(100);
    controller.abort();
    const err = await caught;
    expect(err).toBeInstanceOf(AbortedError);
    expect(isAbortedError(err)).toBe(true);
  });

  it("rejects immediately when the signal is already aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(delay(10, controller.signal)).rejects.toBeInstanceOf(AbortedError);
  });

  it("recognises the DOM AbortError too", () => {
    const domStyle = new Error("The operation was aborted");
    domStyle.name = "AbortError";
    expect(isAbortedError(domStyle)).toBe(true);
    expect(isAbortedError(new Error("other"))).toBe(false);
  });
});
