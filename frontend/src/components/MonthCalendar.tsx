import { useState, useCallback, useEffect } from "react";
import {
  Box,
  IconButton,
  Typography,
  Button,
  Paper,
  Tooltip,
} from "@mui/material";
import ChevronLeftIcon from "@mui/icons-material/ChevronLeft";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import { getHolidayMap } from "../holidays";
import { formatHours } from "./hours/hourOverlay";
import type { HourOverviewEntry } from "../api/types";

interface Props {
  selectedDates: Set<string>;
  onChange: (dates: Set<string>) => void;
  /** Per-date hour total of previously-booked entries. The calendar shows
   *  a small "Xu" badge in the corner of any date with hours so the user
   *  knows it's already (partially) filed. Optional — when omitted the
   *  calendar behaves exactly like before. */
  bookedHours?: Map<string, number>;
  /** Per-date list of entries for the tooltip — typically the same data
   *  buildHourOverlay produces. Only needed when bookedHours is also set. */
  bookedDetails?: Map<string, HourOverviewEntry[]>;
  /** Notify the calendar the user is changing months so it can lift the
   *  visible range up for the parent's hour-overview fetch. Optional. */
  onVisibleRangeChange?: (from: string, to: string) => void;
}

const DAYS = ["Ma", "Di", "Wo", "Do", "Vr", "Za", "Zo"];
const MONTHS = [
  "Januari", "Februari", "Maart", "April", "Mei", "Juni",
  "Juli", "Augustus", "September", "Oktober", "November", "December",
];

function toDateStr(year: number, month: number, day: number): string {
  return `${year}-${String(month + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

function isWeekend(year: number, month: number, day: number): boolean {
  const d = new Date(year, month, day);
  return d.getDay() === 0 || d.getDay() === 6;
}

export function MonthCalendar({
  selectedDates,
  onChange,
  bookedHours,
  bookedDetails,
  onVisibleRangeChange,
}: Props) {
  const now = new Date();
  const [year, setYear] = useState(now.getFullYear());
  const [month, setMonth] = useState(now.getMonth());
  const [rangeStart, setRangeStart] = useState<string | null>(null);

  const daysInMonth = new Date(year, month + 1, 0).getDate();
  // Monday = 0 for our grid
  const firstDayOfWeek = ((new Date(year, month, 1).getDay() + 6) % 7);
  const holidays = getHolidayMap(year);

  // Notify the parent of the visible range whenever month/year changes, so
  // it can re-fetch hour-overview data for the new window. Format matches
  // what the /hours/overview endpoint expects (YYYY-MM-DD).
  useEffect(() => {
    if (!onVisibleRangeChange) return;
    const from = toDateStr(year, month, 1);
    const to = toDateStr(year, month, daysInMonth);
    onVisibleRangeChange(from, to);
  }, [year, month, daysInMonth, onVisibleRangeChange]);

  const prevMonth = () => {
    if (month === 0) { setMonth(11); setYear(year - 1); }
    else setMonth(month - 1);
  };

  const nextMonth = () => {
    if (month === 11) { setMonth(0); setYear(year + 1); }
    else setMonth(month + 1);
  };

  const toggleDate = useCallback(
    (dateStr: string, shiftKey: boolean) => {
      const next = new Set(selectedDates);

      if (shiftKey && rangeStart) {
        const start = new Date(rangeStart);
        const end = new Date(dateStr);
        const [from, to] = start <= end ? [start, end] : [end, start];
        const cursor = new Date(from);
        while (cursor <= to) {
          next.add(toDateStr(cursor.getFullYear(), cursor.getMonth(), cursor.getDate()));
          cursor.setDate(cursor.getDate() + 1);
        }
      } else {
        if (next.has(dateStr)) next.delete(dateStr);
        else next.add(dateStr);
        setRangeStart(dateStr);
      }

      onChange(next);
    },
    [selectedDates, onChange, rangeStart],
  );

  const selectAllWeekdays = () => {
    const next = new Set(selectedDates);
    for (let d = 1; d <= daysInMonth; d++) {
      if (!isWeekend(year, month, d)) {
        const dateStr = toDateStr(year, month, d);
        if (!holidays.has(dateStr)) {
          next.add(dateStr);
        }
      }
    }
    onChange(next);
  };

  const clearMonth = () => {
    const next = new Set(selectedDates);
    for (let d = 1; d <= daysInMonth; d++) {
      next.delete(toDateStr(year, month, d));
    }
    onChange(next);
  };

  // Build calendar grid
  const cells: (number | null)[] = [];
  for (let i = 0; i < firstDayOfWeek; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) cells.push(d);

  return (
    <Paper variant="outlined" sx={{ p: 2 }}>
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mb: 1 }}>
        <IconButton onClick={prevMonth} size="small" aria-label="Vorige maand">
          <ChevronLeftIcon />
        </IconButton>
        <Typography variant="h6">
          {MONTHS[month]} {year}
        </Typography>
        <IconButton onClick={nextMonth} size="small" aria-label="Volgende maand">
          <ChevronRightIcon />
        </IconButton>
      </Box>

      <Box sx={{ display: "flex", gap: 0.5, mb: 1 }}>
        <Button size="small" onClick={selectAllWeekdays}>
          Alle werkdagen
        </Button>
        <Button size="small" color="secondary" onClick={clearMonth}>
          Wis maand
        </Button>
      </Box>

      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: "repeat(7, 1fr)",
          gap: 0.5,
          textAlign: "center",
        }}
      >
        {DAYS.map((d) => (
          <Typography key={d} variant="caption" fontWeight="bold">
            {d}
          </Typography>
        ))}

        {cells.map((day, i) => {
          if (day === null) return <Box key={`empty-${i}`} />;

          const dateStr = toDateStr(year, month, day);
          const selected = selectedDates.has(dateStr);
          const weekend = isWeekend(year, month, day);
          const holiday = holidays.get(dateStr);
          const bookedTotal = bookedHours?.get(dateStr) ?? 0;
          const details = bookedDetails?.get(dateStr) ?? [];

          const cell = (
            <Box
              key={dateStr}
              onClick={(e) => toggleDate(dateStr, e.shiftKey)}
              sx={{
                py: 0.5,
                borderRadius: 1,
                cursor: "pointer",
                position: "relative",
                bgcolor: selected
                  ? "primary.main"
                  : holiday
                    ? "#fff3e0"
                    : weekend
                      ? "grey.200"
                      : "transparent",
                color: selected ? "primary.contrastText" : "text.primary",
                opacity: (weekend || holiday) && !selected ? 0.7 : 1,
                border: holiday && !selected ? "1px solid #ffb74d" : "1px solid transparent",
                "&:hover": {
                  bgcolor: selected ? "primary.dark" : "grey.300",
                },
                userSelect: "none",
                minHeight: 40,
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <Typography variant="body2" sx={{ lineHeight: 1.2 }}>{day}</Typography>
              {holiday && (
                <Typography
                  component="span"
                  sx={{ fontSize: "0.7rem", lineHeight: 1, mt: "1px" }}
                >
                  {holiday.emoji}
                </Typography>
              )}
              {/* Previously-booked hours badge. Positioned in the corner
                  so it doesn't displace the day number. Indigo to
                  distinguish from the primary blue selection state, and
                  visible-on-selected via a thin white outline. */}
              {bookedTotal > 0 && (
                <Box
                  component="span"
                  aria-label={`${formatHours(bookedTotal)} al geboekt`}
                  sx={{
                    position: "absolute",
                    top: 2,
                    right: 2,
                    fontSize: "0.6125rem",
                    fontWeight: 700,
                    lineHeight: 1,
                    px: 0.5,
                    py: "1px",
                    borderRadius: 0.75,
                    bgcolor: selected ? "rgba(255,255,255,0.25)" : "rgba(99, 102, 241, 0.15)",
                    color: selected ? "primary.contrastText" : "#4338ca",
                    pointerEvents: "none",
                  }}
                >
                  {formatHours(bookedTotal)}
                </Box>
              )}
            </Box>
          );

          // Build the tooltip text: holiday name + booked-hours detail when
          // both are present. The booked-hours line is most useful when the
          // user is bulk-selecting and sees a date already has 4u + 4u
          // across two activities, which the corner badge can't convey.
          const tooltipParts: string[] = [];
          if (holiday) tooltipParts.push(holiday.name);
          if (details.length > 0) {
            const lines = details.map(
              (d) =>
                `${formatHours(d.aantalUren)} — ${d.medewerker} · ${d.project} · ${d.activiteit}`,
            );
            tooltipParts.push(lines.join("\n"));
          }

          if (tooltipParts.length > 0) {
            return (
              <Tooltip
                key={dateStr}
                title={
                  <Box component="span" sx={{ whiteSpace: "pre-line" }}>
                    {tooltipParts.join("\n")}
                  </Box>
                }
                arrow
                placement="top"
              >
                {cell}
              </Tooltip>
            );
          }

          return cell;
        })}
      </Box>
    </Paper>
  );
}
