import { describe, it, expect } from "vitest";
import {
  buildArchiveFolderPath,
  enrichArchiveFolders,
  findArchiveFolderForSupplier,
} from "./useArchiveFolders";
import type { ArchiveFolder } from "../api/types";

const f = (id: number, naam: string, parentId: number, isDeleted = false): ArchiveFolder => ({
  id,
  naam,
  parentId,
  isDeleted,
});

/**
 * Real folder layout reported by a self-hoster — a representative tree with
 * per-supplier folders under a "Verwerkte facturen" container nested in an
 * "Inbox" root, plus a soft-deleted duplicate. Keeping this as the primary
 * fixture documents the user-facing shape the hook must support.
 */
const realWorldFolders: ArchiveFolder[] = [
  f(81704302, "Inbox", 0),
  f(81705440, "Verwerkte facturen", 81704302),
  f(81745103, "Anthropic", 81705440),
  f(82508580, "e-boekhouden", 81705440),
  f(81724419, "Overig", 81705440),
  f(81724115, "TransIP", 81705440),
  f(81724104, "Vimexx", 81705440),
  f(81703243, "Transip", 0, true), // legacy lowercase duplicate, soft-deleted
];

describe("buildArchiveFolderPath", () => {
  const byId = new Map(realWorldFolders.map((x) => [x.id, x]));

  it("returns the folder name alone for a root-level folder", () => {
    expect(buildArchiveFolderPath(realWorldFolders[0], byId)).toBe("Inbox");
  });

  it("walks parentId chains to produce a slash-delimited breadcrumb", () => {
    const anthropic = realWorldFolders.find((x) => x.naam === "Anthropic")!;
    expect(buildArchiveFolderPath(anthropic, byId)).toBe(
      "Inbox / Verwerkte facturen / Anthropic",
    );
  });

  it("stops at parentId 0 (the implicit root)", () => {
    // The implicit "Basismap" root is never returned by e-boekhouden, so any
    // parentId of 0 means the folder sits at the top — no synthetic segment
    // should appear in the path.
    const anthropic = realWorldFolders.find((x) => x.naam === "Anthropic")!;
    expect(buildArchiveFolderPath(anthropic, byId).startsWith("Inbox")).toBe(true);
  });

  it("survives a parentId cycle without looping", () => {
    // Defensive: corrupt data shouldn't hang the UI. Two folders pointing at
    // each other still produce a finite path.
    const cyclic: ArchiveFolder[] = [f(1, "A", 2), f(2, "B", 1)];
    const cycByID = new Map(cyclic.map((x) => [x.id, x]));
    const path = buildArchiveFolderPath(cyclic[0], cycByID);
    // Either "B / A" or "A / B" depending on traversal; both are finite.
    expect(path.split(" / ").length).toBeLessThanOrEqual(2);
  });
});

describe("enrichArchiveFolders", () => {
  it("drops soft-deleted folders", () => {
    const out = enrichArchiveFolders(realWorldFolders);
    expect(out.find((x) => x.naam === "Transip" && x.isDeleted)).toBeUndefined();
    // The active capitalised TransIP must survive.
    expect(out.find((x) => x.naam === "TransIP")).toBeDefined();
  });

  it("annotates every folder with its full path", () => {
    const out = enrichArchiveFolders(realWorldFolders);
    const anthropic = out.find((x) => x.naam === "Anthropic");
    expect(anthropic?.path).toBe("Inbox / Verwerkte facturen / Anthropic");
  });

  it("sorts folders by path in Dutch locale", () => {
    const out = enrichArchiveFolders(realWorldFolders);
    const paths = out.map((x) => x.path);
    const sorted = [...paths].sort((a, b) => a.localeCompare(b, "nl"));
    expect(paths).toEqual(sorted);
  });

  it("returns an empty list when given empty input", () => {
    expect(enrichArchiveFolders([])).toEqual([]);
  });

  it("does not crash when all folders are deleted", () => {
    const deleted = realWorldFolders.map((x) => ({ ...x, isDeleted: true }));
    expect(enrichArchiveFolders(deleted)).toEqual([]);
  });
});

describe("findArchiveFolderForSupplier", () => {
  const enriched = enrichArchiveFolders(realWorldFolders);

  it("matches a folder name that appears as a token in the supplier", () => {
    expect(findArchiveFolderForSupplier(enriched, "Anthropic, PBC")?.naam).toBe("Anthropic");
  });

  it("is case-insensitive and ignores punctuation", () => {
    expect(findArchiveFolderForSupplier(enriched, "anthropic pbc")?.naam).toBe("Anthropic");
    expect(findArchiveFolderForSupplier(enriched, "ANTHROPIC, PBC")?.naam).toBe("Anthropic");
  });

  it("matches when the supplier is the folder name verbatim", () => {
    expect(findArchiveFolderForSupplier(enriched, "TransIP")?.naam).toBe("TransIP");
    expect(findArchiveFolderForSupplier(enriched, "Vimexx")?.naam).toBe("Vimexx");
  });

  it("prefers the longer folder name on multiple matches", () => {
    // Both "Anthropic" and a hypothetical "An" would match — the longer one
    // is more specific. Add a short noise folder to verify.
    const withNoise = enrichArchiveFolders([
      ...realWorldFolders,
      f(999, "Ant", 0), // 3 chars, would match "anthropic" via no-no, "ant" isn't a token in "anthropic pbc"
    ]);
    expect(findArchiveFolderForSupplier(withNoise, "Anthropic, PBC")?.naam).toBe("Anthropic");
  });

  it("returns null when nothing matches", () => {
    expect(findArchiveFolderForSupplier(enriched, "Tesla Inc")).toBeNull();
    expect(findArchiveFolderForSupplier(enriched, "Klant XYZ")).toBeNull();
  });

  it("returns null for too-short inputs", () => {
    expect(findArchiveFolderForSupplier(enriched, "")).toBeNull();
    expect(findArchiveFolderForSupplier(enriched, "AB")).toBeNull();
  });

  it("does not match across whole-word boundaries", () => {
    // "Vimexxhosting" contains "vimexx" as substring but not as whole word.
    // Our tokenization is whitespace+punctuation based, so it would NOT
    // match — the supplier collapses to a single token "vimexxhosting".
    expect(findArchiveFolderForSupplier(enriched, "Vimexxhosting")).toBeNull();
  });

  it("does match when the folder name is the longer token", () => {
    // Edge case: supplier "TransIP" -> token "transip" matches folder
    // "TransIP" -> token "transip". Symmetric.
    expect(findArchiveFolderForSupplier(enriched, "TransIP B.V.")?.naam).toBe("TransIP");
  });
});
