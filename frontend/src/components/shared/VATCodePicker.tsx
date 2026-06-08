import { TextField, MenuItem } from "@mui/material";
import type { VATCode } from "../../api/types";

interface Props {
  codes: VATCode[];
  value: string;
  onChange: (code: string) => void;
  label?: string;
  disabled?: boolean;
}

export function VATCodePicker({ codes, value, onChange, label = "BTW-code", disabled }: Props) {
  // Defense-in-depth: when the value isn't present in the visible options
  // (e.g. a code the dedupe dropped, or an unfamiliar code from a
  // different administration), still render a placeholder MenuItem so the
  // field shows the value instead of going visually blank. The user can
  // then either pick a known option from the dropdown or stay with the
  // unrecognized code (which the backend may still accept). Without
  // this fallback, AI-suggested reverse-charge codes like VERL_INK would
  // show as an empty field even though the booking succeeded.
  const knownCodes = new Set(codes.map((c) => c.code));
  const showFallback = value !== "" && !knownCodes.has(value);

  return (
    <TextField
      select
      fullWidth
      label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      size="small"
      disabled={disabled}
    >
      {showFallback && (
        <MenuItem value={value} sx={{ fontStyle: "italic" }}>
          {value} (niet in lijst)
        </MenuItem>
      )}
      {codes.map((c) => (
        <MenuItem key={c.code} value={c.code}>
          {c.omschrijving} ({c.percentage}%)
        </MenuItem>
      ))}
    </TextField>
  );
}
