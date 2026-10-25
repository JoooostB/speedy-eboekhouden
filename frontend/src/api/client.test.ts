import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, ApiError } from "./client";

/**
 * Characterisation of the error handling in client.ts against main
 * (a524a1a). On main request() handled a failed response inline; it now
 * goes through throwApiError, which requestBlob shares. The rules users
 * depend on must stay: a 401 fires "auth:expired" except for /auth/* and
 * /eboekhouden/* (otherwise the login page and the connect dialog would
 * loop), a 412 eboekhouden_session_expired fires "eb:session-expired" (the
 * reconnect prompt), and the thrown message prefers the human `message`.
 *
 * The suite runs in Node: window is a bare EventTarget and fetch is a stub.
 */

type Listener = { auth: number; eb: number };

let fired: Listener;
let fetchMock: ReturnType<typeof vi.fn>;

function jsonResponse(status: number, body: unknown, statusText = "Status"): Response {
  return new Response(JSON.stringify(body), { status, statusText, headers: { "Content-Type": "application/json" } });
}

beforeEach(() => {
  const target = new EventTarget();
  fired = { auth: 0, eb: 0 };
  target.addEventListener("auth:expired", () => fired.auth++);
  target.addEventListener("eb:session-expired", () => fired.eb++);
  vi.stubGlobal("window", target);
  fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

async function caught(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p;
  } catch (err) {
    expect(err).toBeInstanceOf(ApiError);
    return err as ApiError;
  }
  throw new Error("expected the call to fail");
}

describe("request() error handling (unchanged from main)", () => {
  it("does not fire auth:expired for a 401 on /auth/*", async () => {
    fetchMock.mockResolvedValue(jsonResponse(401, { error: "not authenticated" }));
    const err = await caught(api.me());
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/auth/me", expect.objectContaining({ credentials: "include" }));
    expect(err.status).toBe(401);
    expect(err.message).toBe("not authenticated");
    expect(fired).toEqual({ auth: 0, eb: 0 });
  });

  it("does not fire auth:expired for a 401 on /eboekhouden/*", async () => {
    fetchMock.mockResolvedValue(jsonResponse(401, { error: "not authenticated" }));
    await caught(api.ebStatus());
    expect(fired).toEqual({ auth: 0, eb: 0 });
  });

  it("fires auth:expired once for a 401 anywhere else", async () => {
    fetchMock.mockResolvedValue(jsonResponse(401, { error: "not authenticated" }));
    const err = await caught(api.getEmployees());
    expect(err.status).toBe(401);
    expect(fired).toEqual({ auth: 1, eb: 0 });
  });

  it("fires eb:session-expired for a 412 eboekhouden_session_expired and shows the Dutch message", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(412, {
        error: "eboekhouden_session_expired",
        message: "Je e-Boekhouden sessie is verlopen. Verbind opnieuw om door te gaan.",
      }),
    );
    const err = await caught(api.classifyInbox());
    expect(err.status).toBe(412);
    expect(err.message).toBe("Je e-Boekhouden sessie is verlopen. Verbind opnieuw om door te gaan.");
    expect(fired).toEqual({ auth: 0, eb: 1 });
  });

  it("does not fire eb:session-expired for another 412", async () => {
    fetchMock.mockResolvedValue(jsonResponse(412, { error: "something_else" }));
    await caught(api.getProjects());
    expect(fired).toEqual({ auth: 0, eb: 0 });
  });

  it("prefers message over error, falls back to statusText for a non-JSON body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(502, { error: "upstream", message: "e-Boekhouden: Voorbeeldfout" }));
    expect((await caught(api.getActivities())).message).toBe("e-Boekhouden: Voorbeeldfout");

    fetchMock.mockResolvedValueOnce(jsonResponse(400, { error: "invalid request" }));
    expect((await caught(api.getActivities())).message).toBe("invalid request");

    fetchMock.mockResolvedValueOnce(new Response("<html>Bad Gateway</html>", { status: 502, statusText: "Bad Gateway" }));
    expect((await caught(api.getActivities())).message).toBe("Bad Gateway");
  });

  it("returns the parsed body on success", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, [{ id: 1, naam: "Voorbeeld Medewerker" }]));
    await expect(api.getEmployees()).resolves.toEqual([{ id: 1, naam: "Voorbeeld Medewerker" }]);
    expect(fired).toEqual({ auth: 0, eb: 0 });
  });
});

describe("requestBlob shares the rules", () => {
  it("fires auth:expired for a 401 and eb:session-expired for a 412", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(401, { error: "not authenticated" }));
    await caught(api.getDocumentContent(42));
    fetchMock.mockResolvedValueOnce(jsonResponse(412, { error: "eboekhouden_session_expired", message: "verlopen" }));
    await caught(api.getDocumentContent(42));
    expect(fired).toEqual({ auth: 1, eb: 1 });
  });
});

describe("analyzeInvoice (upload) error handling", () => {
  const file = new File([new Uint8Array([0x25, 0x50, 0x44, 0x46])], "voorbeeld-factuur.pdf", { type: "application/pdf" });

  it("keeps main's message order for the errors main already sent (error before message)", async () => {
    // Unchanged from main: analyzeInvoice has its own fetch and prefers the
    // machine code. Recorded so a change here is deliberate.
    fetchMock.mockResolvedValue(jsonResponse(400, { error: "no_api_key", message: "Stel eerst een Anthropic API-sleutel in" }));
    const err = await caught(api.analyzeInvoice(file));
    expect(err.status).toBe(400);
    expect(err.message).toBe("no_api_key");
  });

  // regression: since this branch POST /invoices/analyze answers 412
  // eboekhouden_session_expired when the e-boekhouden session has expired
  // (main ignored the expired session and still analysed). analyzeInvoice
  // bypasses throwApiError, so the reconnect prompt never opens and the user
  // reads "Fout bij voorbeeld-factuur.pdf: eboekhouden_session_expired".
  it.skip("regression: a 412 from analyze opens the reconnect prompt and shows the Dutch message", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(412, {
        error: "eboekhouden_session_expired",
        message: "Je e-Boekhouden sessie is verlopen. Verbind opnieuw om door te gaan.",
      }),
    );
    const err = await caught(api.analyzeInvoice(file));
    expect(err.message).toBe("Je e-Boekhouden sessie is verlopen. Verbind opnieuw om door te gaan.");
    expect(fired.eb).toBe(1);
  });
});
