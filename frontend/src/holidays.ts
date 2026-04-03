/**
 * Dutch public holidays (officiële feestdagen).
 * Source: rijksoverheid.nl
 *
 * Easter is calculated using the Anonymous Gregorian algorithm (Computus).
 */

interface Holiday {
  date: string; // YYYY-MM-DD
  name: string;
  emoji: string;
}

function computeEaster(year: number): { month: number; day: number } {
  const a = year % 19;
  const b = Math.floor(year / 100);
  const c = year % 100;
  const d = Math.floor(b / 4);
  const e = b % 4;
  const f = Math.floor((b + 8) / 25);
  const g = Math.floor((b - f + 1) / 3);
  const h = (19 * a + b - d - g + 15) % 30;
  const i = Math.floor(c / 4);
  const k = c % 4;
  const l = (32 + 2 * e + 2 * i - h - k) % 7;
  const m = Math.floor((a + 11 * h + 22 * l) / 451);
  const month = Math.floor((h + l - 7 * m + 114) / 31);
  const day = ((h + l - 7 * m + 114) % 31) + 1;
  return { month, day }; // month is 1-indexed (3 = March, 4 = April)
}

function addDays(year: number, month: number, day: number, offset: number): Date {
  const d = new Date(year, month - 1, day);
  d.setDate(d.getDate() + offset);
  return d;
}

function fmt(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function getKoningsdag(year: number): string {
  // April 27, unless it falls on Sunday — then April 26
  const d = new Date(year, 3, 27);
  if (d.getDay() === 0) return `${year}-04-26`;
  return `${year}-04-27`;
}

export function getDutchHolidays(year: number): Holiday[] {
  const easter = computeEaster(year);
  const easterDate = new Date(year, easter.month - 1, easter.day);

  const rel = (offset: number) => fmt(addDays(year, easter.month, easter.day, offset));

  return [
    { date: `${year}-01-01`, name: "Nieuwjaarsdag", emoji: "🎆" },
    { date: rel(-2), name: "Goede Vrijdag", emoji: "✝️" },
    { date: fmt(easterDate), name: "Eerste Paasdag", emoji: "🥚" },
    { date: rel(1), name: "Tweede Paasdag", emoji: "🥚" },
    { date: getKoningsdag(year), name: "Koningsdag", emoji: "👑" },
    { date: `${year}-05-05`, name: "Bevrijdingsdag", emoji: "🕊️" },
    { date: rel(39), name: "Hemelvaartsdag", emoji: "☁️" },
    { date: rel(49), name: "Eerste Pinksterdag", emoji: "🕊️" },
    { date: rel(50), name: "Tweede Pinksterdag", emoji: "🕊️" },
    { date: `${year}-12-25`, name: "Eerste Kerstdag", emoji: "🎄" },
    { date: `${year}-12-26`, name: "Tweede Kerstdag", emoji: "🎄" },
  ];
}

const holidayCache = new Map<number, Map<string, Holiday>>();

export function getHolidayMap(year: number): Map<string, Holiday> {
  if (!holidayCache.has(year)) {
    const map = new Map<string, Holiday>();
    for (const h of getDutchHolidays(year)) {
      map.set(h.date, h);
    }
    holidayCache.set(year, map);
  }
  return holidayCache.get(year)!;
}
