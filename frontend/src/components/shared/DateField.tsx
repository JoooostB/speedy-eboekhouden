import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import {
  Box,
  Button,
  ButtonBase,
  Divider,
  IconButton,
  InputAdornment,
  Popover,
  TextField,
  useMediaQuery,
} from "@mui/material";
import { alpha } from "@mui/material/styles";
import ChevronLeftIcon from "@mui/icons-material/ChevronLeft";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import ArrowDropDownIcon from "@mui/icons-material/ArrowDropDown";
import ArrowDropUpIcon from "@mui/icons-material/ArrowDropUp";
import CalendarTodayOutlinedIcon from "@mui/icons-material/CalendarTodayOutlined";
import {
  MONTH_NAMES,
  MONTH_SHORT_NAMES,
  WEEKDAY_NAMES,
  WEEKDAY_SHORT_NAMES,
  addDays,
  addMonths,
  buildMonthGrid,
  daysInMonth,
  endOfWeek,
  formatDutchDate,
  formatLongDutchDate,
  formatMonthYear,
  isoParts,
  isValidIso,
  parseDutchDate,
  startOfWeek,
  toIso,
  todayIso,
} from "./dutchDate";

/**
 * Dutch date field: a text field that takes typed dates (15-11-2026,
 * 15/11/26, 15112026, …; see parseDutchDate) plus a calendar popover.
 * Replaces the native <input type="date">, whose calendar follows the OS
 * language and looks nothing like the app.
 *
 * Keyboard
 *   In the field: type a date, Enter or leaving the field commits it;
 *   Alt+ArrowDown opens the calendar.
 *   In the calendar (roving tabindex on the days):
 *     ArrowLeft/Right   ±1 day          ArrowUp/Down        ±1 week
 *     Home/End          start/end of the week (Monday/Sunday)
 *     PageUp/PageDown   ±1 month        Shift+PageUp/Down   ±1 year
 *     Enter/Space       pick the day    Escape              close, no change
 *   Moving past the visible month moves the view along.
 */

interface Props {
  label: string;
  /** ISO `YYYY-MM-DD`, or "" for no date. */
  value: string;
  /** Called with an ISO date, or "" when the field was emptied or holds
   *  text that is not a date. Never called without a change. */
  onChange: (iso: string) => void;
  disabled?: boolean;
  size?: "small" | "medium";
  fullWidth?: boolean;
  helperText?: string;
}

const INVALID_TEXT = "Vul een datum in als dd-mm-jjjj.";
const DIALOG_LABEL = "Kies datum";

/** Visually hidden but read by screen readers (same recipe as the live
 *  regions elsewhere in the app). */
const visuallyHidden = {
  position: "absolute",
  width: 1,
  height: 1,
  overflow: "hidden",
  clip: "rect(0 0 0 0)",
  whiteSpace: "nowrap",
} as const;

export function DateField({ label, value, onChange, disabled, size = "small", fullWidth, helperText }: Props) {
  const [text, setText] = useState(() => formatDutchDate(value));
  const [invalid, setInvalid] = useState(false);
  const [open, setOpen] = useState(false);
  // Bumped on every open, so the calendar starts fresh (month of the value,
  // day view) instead of where it was left.
  const [openCount, setOpenCount] = useState(0);
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);

  const anchorRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  /** Where focus goes after the calendar closes: the control that opened it. */
  const returnFocusRef = useRef<HTMLElement | null>(null);
  /** The value the text currently reflects. A value from outside that
   *  differs from it replaces the text; our own onChange does not, so an
   *  unparseable text survives the onChange("") it causes. */
  const syncedValueRef = useRef(value);

  useEffect(() => {
    if (value === syncedValueRef.current) return;
    syncedValueRef.current = value;
    setText(formatDutchDate(value));
    setInvalid(false);
  }, [value]);

  const emit = useCallback(
    (iso: string) => {
      syncedValueRef.current = iso;
      if (iso !== value) onChange(iso);
    },
    [onChange, value],
  );

  const commit = useCallback(() => {
    const trimmed = text.trim();
    if (trimmed === "") {
      setText("");
      setInvalid(false);
      emit("");
      return;
    }
    const iso = parseDutchDate(trimmed, todayIso());
    if (iso) {
      setText(formatDutchDate(iso));
      setInvalid(false);
      emit(iso);
    } else {
      setInvalid(true);
      emit("");
    }
  }, [text, emit]);

  const openCalendar = useCallback((from: HTMLElement | null) => {
    returnFocusRef.current = from;
    setAnchorEl(anchorRef.current);
    setOpenCount((n) => n + 1);
    setOpen(true);
  }, []);

  const closeCalendar = useCallback(() => setOpen(false), []);

  // Return focus after the popover's focus trap has let go (its effects run
  // before ours on the render that closes it). Modal's own restore is
  // disabled: a mouse click does not focus the button in Safari, so it would
  // restore to <body>.
  const wasOpenRef = useRef(false);
  useEffect(() => {
    if (wasOpenRef.current && !open) returnFocusRef.current?.focus();
    wasOpenRef.current = open;
  }, [open]);

  const pick = useCallback(
    (iso: string) => {
      setText(formatDutchDate(iso));
      setInvalid(false);
      emit(iso);
      setOpen(false);
    },
    [emit],
  );

  const committedIso = isValidIso(value) ? value : "";
  const longValue = formatLongDutchDate(committedIso);
  const buttonLabel = longValue ? `${DIALOG_LABEL}, gekozen is ${longValue}` : DIALOG_LABEL;

  return (
    <>
      <TextField
        ref={anchorRef}
        label={label}
        value={text}
        onChange={(e) => {
          const next = e.target.value;
          setText(next);
          // The error clears as soon as the text reads as a date again.
          if (invalid && (next.trim() === "" || parseDutchDate(next, todayIso()))) setInvalid(false);
        }}
        onBlur={commit}
        size={size}
        fullWidth={fullWidth}
        disabled={disabled}
        error={invalid}
        helperText={invalid ? INVALID_TEXT : helperText}
        placeholder="dd-mm-jjjj"
        autoComplete="off"
        slotProps={{
          inputLabel: { shrink: true },
          htmlInput: {
            ref: inputRef,
            inputMode: "numeric",
            // On the input itself, not the TextField root: keys pressed on
            // the calendar button must keep their own meaning.
            onKeyDown: (e: KeyboardEvent<HTMLInputElement>) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commit();
              } else if (e.key === "ArrowDown" && e.altKey) {
                e.preventDefault();
                commit();
                openCalendar(inputRef.current);
              }
            },
          },
          input: {
            endAdornment: (
              <InputAdornment position="end">
                <IconButton
                  ref={buttonRef}
                  edge="end"
                  size={size}
                  disabled={disabled}
                  aria-label={buttonLabel}
                  aria-haspopup="dialog"
                  aria-expanded={open}
                  onClick={() => openCalendar(buttonRef.current)}
                >
                  <CalendarTodayOutlinedIcon fontSize="small" />
                </IconButton>
              </InputAdornment>
            ),
          },
        }}
      />
      <CalendarPopover
        key={openCount}
        open={open}
        anchorEl={anchorEl}
        value={committedIso}
        onPick={pick}
        onClose={closeCalendar}
      />
    </>
  );
}

// ---------------------------------------------------------------------------
// Calendar popover
// ---------------------------------------------------------------------------

interface CalendarProps {
  open: boolean;
  anchorEl: HTMLElement | null;
  /** A valid ISO date or "". */
  value: string;
  onPick: (iso: string) => void;
  onClose: () => void;
}

function CalendarPopover({ open, anchorEl, value, onPick, onClose }: CalendarProps) {
  const reduceMotion = useMediaQuery("(prefers-reduced-motion: reduce)");
  const today = todayIso();
  const start = value || today;
  const startParts = isoParts(start)!;

  const [view, setView] = useState({ year: startParts.year, month: startParts.month });
  const [mode, setMode] = useState<"day" | "month">("day");
  /** The day that holds the roving tabindex; always in the visible month. */
  const [focused, setFocused] = useState(start);
  const [announced, setAnnounced] = useState("");

  const gridRef = useRef<HTMLDivElement>(null);
  const monthGridRef = useRef<HTMLDivElement>(null);
  /** Set when focus should land on the focused day after the next render:
   *  on open, after keyboard navigation and after picking a month. Not after
   *  "Vorige/Volgende maand", where focus stays on that button. */
  const focusDayRef = useRef(true);
  const focusMonthRef = useRef(false);
  const headingId = useId();

  useEffect(() => {
    // The flags are only consumed once the target exists: the popover's
    // content mounts a render after it opens (portal).
    if (mode === "day" && focusDayRef.current) {
      const day = gridRef.current?.querySelector<HTMLElement>(`[data-iso="${focused}"]`);
      if (day) {
        focusDayRef.current = false;
        day.focus();
      }
    }
    if (mode === "month" && focusMonthRef.current) {
      const month = monthGridRef.current?.querySelector<HTMLElement>('[data-current="true"]');
      if (month) {
        focusMonthRef.current = false;
        month.focus();
      }
    }
  });

  // Announce the visible month when it changes, not on open (the focused
  // day's label already names it).
  const firstViewRef = useRef(true);
  useEffect(() => {
    if (firstViewRef.current) {
      firstViewRef.current = false;
      return;
    }
    setAnnounced(mode === "day" ? formatMonthYear(view.year, view.month) : String(view.year));
  }, [view.year, view.month, mode]);

  /** Move the roving focus to iso and bring its month into view. */
  const moveTo = (iso: string, focus: boolean) => {
    const p = isoParts(iso);
    if (!p || !isValidIso(iso)) return; // outside 1900..2100
    focusDayRef.current = focus;
    setFocused(iso);
    setView({ year: p.year, month: p.month });
  };

  const shiftMonth = (n: number) => moveTo(addMonths(focused, n), false);

  const onDayKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    let next: string | null = null;
    switch (e.key) {
      case "ArrowLeft": next = addDays(focused, -1); break;
      case "ArrowRight": next = addDays(focused, 1); break;
      case "ArrowUp": next = addDays(focused, -7); break;
      case "ArrowDown": next = addDays(focused, 7); break;
      case "Home": next = startOfWeek(focused); break;
      case "End": next = endOfWeek(focused); break;
      case "PageUp": next = addMonths(focused, e.shiftKey ? -12 : -1); break;
      case "PageDown": next = addMonths(focused, e.shiftKey ? 12 : 1); break;
      default: return; // Enter/Space: the button's own click; Escape: the Popover
    }
    e.preventDefault();
    moveTo(next, true);
  };

  const pickMonth = (month: number) => {
    const p = isoParts(focused)!;
    const day = Math.min(p.day, daysInMonth(view.year, month));
    focusDayRef.current = true;
    setFocused(toIso(view.year, month, day));
    setView({ year: view.year, month });
    setMode("day");
  };

  const grid = useMemo(() => buildMonthGrid(view.year, view.month), [view.year, view.month]);
  const valueParts = value ? isoParts(value) : null;
  const heading = mode === "day" ? formatMonthYear(view.year, view.month) : String(view.year);

  const stepLabels =
    mode === "day" ? (["Vorige maand", "Volgende maand"] as const) : (["Vorig jaar", "Volgend jaar"] as const);
  const step = (dir: -1 | 1) => {
    if (mode === "day") shiftMonth(dir);
    else setView((v) => ({ ...v, year: Math.min(2100, Math.max(1900, v.year + dir)) }));
  };
  const canStep = (dir: -1 | 1) =>
    mode === "day"
      ? isValidIso(addMonths(toIso(view.year, view.month, 1), dir))
      : view.year + dir >= 1900 && view.year + dir <= 2100;

  return (
    <Popover
      open={open}
      anchorEl={anchorEl}
      onClose={onClose}
      anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
      transformOrigin={{ vertical: "top", horizontal: "left" }}
      transitionDuration={reduceMotion ? 0 : "auto"}
      // We focus the day ourselves and return focus in DateField.
      disableAutoFocus
      disableRestoreFocus
      slotProps={{
        paper: {
          role: "dialog",
          "aria-modal": true,
          "aria-label": DIALOG_LABEL,
          sx: {
            mt: 0.5,
            // 7 cells of at least 36 px plus padding; never wider than the
            // viewport minus the Popover's 16 px margin on each side.
            width: "min(320px, calc(100vw - 32px))",
            p: { xs: 1, sm: 1.5 },
          },
        },
      }}
    >
      {/* Header: step back, month/year switch, step forward */}
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 0.5, mb: 1 }}>
        <IconButton aria-label={stepLabels[0]} onClick={() => step(-1)} disabled={!canStep(-1)} size="small">
          <ChevronLeftIcon />
        </IconButton>
        <Button
          id={headingId}
          onClick={() => {
            if (mode === "day") {
              focusMonthRef.current = true;
              setMode("month");
            } else {
              focusDayRef.current = true;
              setMode("day");
            }
          }}
          aria-label={mode === "day" ? `${heading}, kies een andere maand` : `${heading}, terug naar de dagen`}
          endIcon={mode === "day" ? <ArrowDropDownIcon /> : <ArrowDropUpIcon />}
          color="inherit"
          sx={{ fontWeight: 600, fontSize: "0.95rem", minHeight: 40, px: 1.5 }}
        >
          {heading}
        </Button>
        <IconButton aria-label={stepLabels[1]} onClick={() => step(1)} disabled={!canStep(1)} size="small">
          <ChevronRightIcon />
        </IconButton>
      </Box>

      {mode === "day" ? (
        <Box ref={gridRef} role="grid" aria-labelledby={headingId} sx={{ display: "grid", gap: 0.25 }}>
          <Box role="row" sx={rowSx}>
            {WEEKDAY_SHORT_NAMES.map((short, i) => (
              <Box
                key={short}
                role="columnheader"
                aria-label={WEEKDAY_NAMES[i]}
                sx={{ textAlign: "center", py: 0.5, typography: "caption", fontWeight: 600, color: "text.secondary" }}
              >
                <abbr title={WEEKDAY_NAMES[i]} aria-hidden="true" style={{ textDecoration: "none" }}>
                  {short}
                </abbr>
              </Box>
            ))}
          </Box>
          {grid.map((row) => (
            <Box key={row[0].iso} role="row" sx={rowSx}>
              {row.map((cell, i) => {
                const selected = cell.iso === value;
                const isToday = cell.iso === today;
                const weekend = i >= 5;
                return (
                  <Box key={cell.iso} role="gridcell" aria-selected={selected}>
                    <ButtonBase
                      data-iso={cell.iso}
                      tabIndex={cell.iso === focused ? 0 : -1}
                      aria-label={formatLongDutchDate(cell.iso)}
                      aria-current={isToday ? "date" : undefined}
                      onClick={() => onPick(cell.iso)}
                      onKeyDown={onDayKeyDown}
                      onFocus={() => {
                        // A click on a day of the adjacent month focuses it
                        // before picking; keep the roving index in step.
                        if (cell.iso !== focused) setFocused(cell.iso);
                      }}
                      sx={(t) => ({
                        width: "100%",
                        minWidth: 36,
                        aspectRatio: "1",
                        maxHeight: 40,
                        minHeight: 36,
                        borderRadius: "50%",
                        typography: "body2",
                        fontWeight: cell.inMonth ? 500 : 400,
                        // text.secondary keeps adjacent-month days readable
                        // (well above 4.5:1), just quieter.
                        color: cell.inMonth ? "text.primary" : "text.secondary",
                        bgcolor: weekend && cell.inMonth ? alpha(t.palette.text.primary, 0.04) : "transparent",
                        border: "1px solid transparent",
                        ...(isToday && { borderColor: t.palette.primary.main }),
                        "@media (hover: hover)": {
                          "&:hover": { bgcolor: t.palette.action.hover },
                        },
                        ...(selected && {
                          bgcolor: t.palette.primary.main,
                          color: t.palette.primary.contrastText,
                          fontWeight: 600,
                          "@media (hover: hover)": {
                            "&:hover": { bgcolor: t.palette.primary.dark },
                          },
                        }),
                        "&.Mui-focusVisible, &:focus-visible": {
                          outline: `2px solid ${t.palette.primary.main}`,
                          outlineOffset: 2,
                        },
                        "@media (prefers-reduced-motion: no-preference)": {
                          transition: t.transitions.create(["background-color", "color"], {
                            duration: t.transitions.duration.shortest,
                          }),
                        },
                      })}
                    >
                      {cell.day}
                    </ButtonBase>
                  </Box>
                );
              })}
            </Box>
          ))}
        </Box>
      ) : (
        <Box
          ref={monthGridRef}
          role="group"
          aria-label={`Maanden van ${view.year}`}
          sx={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 1, py: 1 }}
        >
          {MONTH_SHORT_NAMES.map((short, i) => {
            const month = i + 1;
            const chosen = valueParts?.year === view.year && valueParts.month === month;
            const current = month === view.month;
            return (
              <Button
                key={short}
                data-current={current ? "true" : undefined}
                onClick={() => pickMonth(month)}
                aria-label={`${MONTH_NAMES[i]} ${view.year}`}
                aria-pressed={chosen}
                variant={chosen ? "contained" : "text"}
                color={chosen ? "primary" : "inherit"}
                sx={(t) => ({
                  minHeight: 44,
                  fontWeight: chosen ? 600 : 500,
                  ...(current && !chosen && { outline: `1px solid ${t.palette.divider}` }),
                  "&.Mui-focusVisible": {
                    outline: `2px solid ${t.palette.primary.main}`,
                    outlineOffset: 2,
                  },
                })}
              >
                {short}
              </Button>
            );
          })}
        </Box>
      )}

      <Divider sx={{ mt: 1 }} />
      <Box sx={{ display: "flex", justifyContent: "flex-end", pt: 1 }}>
        <Button size="small" onClick={() => onPick(today)} aria-label={`Vandaag, ${formatLongDutchDate(today)}`}>
          Vandaag
        </Button>
      </Box>

      <Box role="status" aria-live="polite" sx={visuallyHidden}>
        {announced}
      </Box>
    </Popover>
  );
}

const rowSx = { display: "grid", gridTemplateColumns: "repeat(7, minmax(0, 1fr))", gap: 0.25 } as const;
