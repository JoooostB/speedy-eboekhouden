import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Box, Button, TextField, Typography } from "@mui/material";
import type { LedgerAccount, VATCode } from "../../api/types";
import { formatMoney } from "../../hooks/mailDocumentsModel";
import { AMOUNT_HINT, invalidAmountField } from "../inbox/reviewAmounts";
import {
  MAX_REGELS,
  addRegel,
  focusAfterDelete,
  regelBtwCents,
  regelCents,
  remainderCents,
  remainderStatus,
  validateRegels,
  type RegelEdit,
} from "../inbox/regels";
import { LedgerAccountPicker } from "./LedgerAccountPicker";
import { VATCodePicker } from "./VATCodePicker";

interface Props {
  regels: RegelEdit[];
  /** The amount the lines must add up to, in cents. */
  totalCents: number;
  /** Where the total comes from, shown next to the status ("afschrift");
   *  omitted when the total is typed right above the editor. */
  totalSource?: string;
  ledgerAccounts: LedgerAccount[];
  vatCodes: VATCode[];
  /** A BTW code's percentage; 0 for an unknown code. */
  vatRate: (code: string) => number;
  onChange: (regels: RegelEdit[]) => void;
  disabled?: boolean;
  /** The line whose amount field takes focus when the editor mounts (the
   *  empty line 2 of a fresh split, in a dialog). */
  initialFocusKey?: string;
}

/** How long after the last change the live region speaks, so a screen
 *  reader hears "Nog te verdelen: € 3,95" once instead of every keystroke. */
const ANNOUNCE_DELAY_MS = 600;

/** Where focus should land once the lines have been re-rendered. */
type PendingFocus = { key: string; target: "delete" | "omschrijving" };

/**
 * RegelsEditor — the lines of a split booking: one fieldset per regel with
 * its description, gross amount, BTW code and tegenrekening, plus the BTW
 * that follows from the amount (read-only, rounded per line like
 * e-boekhouden). The footer adds a line pre-filled with what is still to
 * divide and shows the state of the split: the remainder while there is
 * one, else the first line that is not ready, else "Verdeeld". The same
 * text reaches a visually hidden polite live region, debounced. The caller
 * blocks booking while validateRegels is not null.
 *
 * Keyboard: lines are keyed by RegelEdit.key, so a row keeps its identity
 * across deletes. After "Verwijderen" focus moves to the delete button of
 * the row that took its place (or the previous row's when the last row
 * went); after "Regel toevoegen" to the new row's Omschrijving.
 *
 * Layout follows the editor's own width (container query), not the
 * viewport: five columns from 720px, otherwise two rows per line, and one
 * column on a phone.
 */
export function RegelsEditor({
  regels,
  totalCents,
  totalSource,
  ledgerAccounts,
  vatCodes,
  vatRate,
  onChange,
  disabled,
  initialFocusKey,
}: Props) {
  const rest = remainderCents(regels, totalCents);
  const blocker = validateRegels(regels, totalCents);
  const status = rest !== 0 ? remainderStatus(rest) : (blocker ?? remainderStatus(0));
  const ready = blocker === null;
  const btwTotal = regels.reduce((sum, r) => sum + regelBtwCents(regelCents(r), vatRate(r.btwCode)).btwCents, 0);

  const [announced, setAnnounced] = useState(status);
  useEffect(() => {
    const t = setTimeout(() => setAnnounced(status), ANNOUNCE_DELAY_MS);
    return () => clearTimeout(t);
  }, [status]);

  const deleteButtons = useRef(new Map<string, HTMLButtonElement>());
  const omschrijvingInputs = useRef(new Map<string, HTMLInputElement>());
  const amountInputs = useRef(new Map<string, HTMLInputElement>());
  const pendingFocus = useRef<PendingFocus | null>(null);
  // Once, on mount: the caller's line takes focus (after the dialog's own
  // focus handling, hence the timeout).
  useEffect(() => {
    if (!initialFocusKey) return;
    const t = setTimeout(() => amountInputs.current.get(initialFocusKey)?.focus(), 0);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // Before paint, so focus does not visibly jump. A pending entry whose row
  // is gone (the parent collapsed the editor) is dropped, never applied to a
  // later change; a delete button that became disabled (two lines left)
  // hands focus to its row's Omschrijving instead.
  useLayoutEffect(() => {
    const pending = pendingFocus.current;
    if (!pending) return;
    pendingFocus.current = null;
    if (!regels.some((r) => r.key === pending.key)) return;
    const button = deleteButtons.current.get(pending.key);
    const input = omschrijvingInputs.current.get(pending.key);
    const el = pending.target === "delete" && button && !button.disabled ? button : input;
    el?.focus();
  }, [regels]);

  const update = <K extends keyof RegelEdit>(i: number, field: K, value: RegelEdit[K]) => {
    onChange(regels.map((r, idx) => (idx === i ? { ...r, [field]: value } : r)));
  };

  const remove = (i: number) => {
    const next = regels.filter((_, idx) => idx !== i);
    const focusIndex = focusAfterDelete(i, next.length);
    pendingFocus.current = focusIndex === null ? null : { key: next[focusIndex].key, target: "delete" };
    onChange(next);
  };

  const add = () => {
    const next = addRegel(regels, totalCents);
    if (next.length > regels.length) pendingFocus.current = { key: next[next.length - 1].key, target: "omschrijving" };
    onChange(next);
  };

  return (
    <Box sx={{ containerType: "inline-size" }}>
      {regels.map((regel, i) => {
        const btw = regelBtwCents(regelCents(regel), vatRate(regel.btwCode)).btwCents;
        // Only text that is not an amount is an error; an empty line is
        // reported by the status line, not by a red field.
        const invalid = invalidAmountField(regel.bedragIncl);
        return (
          <Box
            component="fieldset"
            key={regel.key}
            sx={{
              border: 0,
              m: 0,
              p: 0,
              py: 1.5,
              borderTop: i === 0 ? "none" : "1px solid",
              borderColor: "divider",
              display: "grid",
              gap: 1.5,
              alignItems: "start",
              gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
              "@container (min-width: 720px)": { gridTemplateColumns: "1.4fr 1fr 1.2fr 1.6fr auto" },
            }}
          >
            <Typography component="legend" variant="caption" color="text.secondary" sx={{ p: 0, mb: 0.5 }}>
              Regel {i + 1}
            </Typography>
            <TextField
              label="Omschrijving"
              value={regel.omschrijving}
              onChange={(e) => update(i, "omschrijving", e.target.value)}
              size="small"
              fullWidth
              disabled={disabled}
              inputRef={(el: HTMLInputElement | null) => {
                if (el) omschrijvingInputs.current.set(regel.key, el);
                else omschrijvingInputs.current.delete(regel.key);
              }}
            />
            <TextField
              label="Bedrag incl. BTW"
              value={regel.bedragIncl}
              error={invalid}
              helperText={invalid ? AMOUNT_HINT : `BTW ${formatMoney(btw / 100)}`}
              onChange={(e) => update(i, "bedragIncl", e.target.value)}
              size="small"
              fullWidth
              disabled={disabled}
              inputRef={(el: HTMLInputElement | null) => {
                if (el) amountInputs.current.set(regel.key, el);
                else amountInputs.current.delete(regel.key);
              }}
              slotProps={{
                htmlInput: { inputMode: "decimal" },
                input: {
                  startAdornment: (
                    <Typography variant="body2" color="text.secondary" sx={{ mr: 0.5 }}>
                      &euro;
                    </Typography>
                  ),
                },
              }}
            />
            <VATCodePicker
              codes={vatCodes}
              value={regel.btwCode}
              onChange={(code) => update(i, "btwCode", code)}
              disabled={disabled}
            />
            <LedgerAccountPicker
              accounts={ledgerAccounts}
              value={regel.ledgerAccount}
              onChange={(acc) => update(i, "ledgerAccount", acc)}
              label="Tegenrekening"
              disabled={disabled}
            />
            <Button
              color="error"
              size="small"
              onClick={() => remove(i)}
              /* Two lines is the minimum of a split; "Niet splitsen" is the
                 way back to one line. */
              disabled={disabled || regels.length <= 2}
              sx={{ minWidth: 0, alignSelf: "start", mt: 0.5, justifySelf: "start" }}
              aria-label={`Regel ${i + 1} verwijderen`}
              ref={(el: HTMLButtonElement | null) => {
                if (el) deleteButtons.current.set(regel.key, el);
                else deleteButtons.current.delete(regel.key);
              }}
            >
              Verwijderen
            </Button>
          </Box>
        );
      })}
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          flexWrap: "wrap",
          gap: 1,
          pt: 1,
          borderTop: "1px solid",
          borderColor: "divider",
        }}
      >
        <Box>
          <Button size="small" onClick={add} disabled={disabled || regels.length >= MAX_REGELS}>
            Regel toevoegen
          </Button>
          {regels.length >= MAX_REGELS && (
            <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
              Maximaal {MAX_REGELS} regels
            </Typography>
          )}
        </Box>
        <Typography variant="body2" sx={{ fontWeight: 600, color: ready ? "success.dark" : "text.primary" }}>
          {status}
          <Typography component="span" variant="body2" color="text.secondary" sx={{ fontWeight: 400 }}>
            {totalSource ? ` · totaal ${totalSource} ${formatMoney(totalCents / 100)}` : ""} · BTW {formatMoney(btwTotal / 100)}
          </Typography>
        </Typography>
        <Box
          role="status"
          aria-live="polite"
          sx={{ position: "absolute", width: 1, height: 1, overflow: "hidden", clip: "rect(0 0 0 0)", whiteSpace: "nowrap" }}
        >
          {announced}
        </Box>
      </Box>
    </Box>
  );
}
