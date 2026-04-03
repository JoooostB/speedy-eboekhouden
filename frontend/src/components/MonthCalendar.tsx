import { useState, useCallback } from "react";
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

interface Props {
  selectedDates: Set<string>;
  onChange: (dates: Set<string>) => void;
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

export function MonthCalendar({ selectedDates, onChange }: Props) {
  const now = new Date();
  const [year, setYear] = useState(now.getFullYear());
  const [month, setMonth] = useState(now.getMonth());
  const [rangeStart, setRangeStart] = useState<string | null>(null);

  const daysInMonth = new Date(year, month + 1, 0).getDate();
  // Monday = 0 for our grid
  const firstDayOfWeek = ((new Date(year, month, 1).getDay() + 6) % 7);
  const holidays = getHolidayMap(year);

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
            </Box>
          );

          if (holiday) {
            return (
              <Tooltip key={dateStr} title={holiday.name} arrow placement="top">
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
