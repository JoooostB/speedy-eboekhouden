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
      {codes.map((c) => (
        <MenuItem key={c.code} value={c.code}>
          {c.omschrijving} ({c.percentage}%)
        </MenuItem>
      ))}
    </TextField>
  );
}
