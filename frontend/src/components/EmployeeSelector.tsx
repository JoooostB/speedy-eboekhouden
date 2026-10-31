import {
  Autocomplete,
  Chip,
  TextField,
  CircularProgress,
} from "@mui/material";
import type { Employee } from "../api/types";

interface Props {
  employees: Employee[];
  selected: Employee[];
  onChange: (selected: Employee[]) => void;
  loading: boolean;
}

export function EmployeeSelector({
  employees,
  selected,
  onChange,
  loading,
}: Props) {
  return (
    <Autocomplete
      multiple
      options={employees}
      getOptionLabel={(opt) => opt.naam}
      value={selected}
      onChange={(_, value) => onChange(value)}
      loading={loading}
      renderValue={(value, getItemProps) =>
        value.map((option, index) => {
          const { key, ...rest } = getItemProps({ index });
          return (
            <Chip key={key} label={option.naam} size="small" {...rest} />
          );
        })
      }
      renderInput={(params) => (
        <TextField
          {...params}
          label="Medewerkers"
          placeholder="Kies medewerkers"
          slotProps={{
            ...params.slotProps,
            input: {
              ...params.slotProps.input,
              endAdornment: (
                <>
                  {loading && <CircularProgress size={20} />}
                  {params.slotProps.input.endAdornment}
                </>
              ),
            },
          }}
        />
      )}
    />
  );
}
