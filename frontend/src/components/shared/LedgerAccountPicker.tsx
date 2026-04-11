import { Autocomplete, TextField } from "@mui/material";
import type { LedgerAccount } from "../../api/types";

interface Props {
  accounts: LedgerAccount[];
  value: LedgerAccount | null;
  onChange: (account: LedgerAccount | null) => void;
  label?: string;
  disabled?: boolean;
}

export function LedgerAccountPicker({ accounts, value, onChange, label = "Grootboekrekening", disabled }: Props) {
  return (
    <Autocomplete
      options={accounts}
      value={value}
      onChange={(_, v) => onChange(v)}
      getOptionLabel={(opt) => `${opt.code} - ${opt.omschrijving}`}
      groupBy={(opt) => opt.rekeningCategorie}
      isOptionEqualToValue={(opt, val) => opt.id === val.id}
      renderInput={(params) => <TextField {...params} label={label} size="small" />}
      disabled={disabled}
      size="small"
    />
  );
}
