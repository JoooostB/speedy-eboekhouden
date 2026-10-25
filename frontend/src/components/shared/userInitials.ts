/** The initials an avatar shows without a photo: "Voorbeeld Gebruiker" →
 *  "VG", at most two letters, "?" without a name. Shared by the app bar and
 *  Instellingen; pure, so it is unit-tested. */
export function userInitials(name: string | null | undefined): string {
  const letters = (name ?? "")
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
  return letters || "?";
}
