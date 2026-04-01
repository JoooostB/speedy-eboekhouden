import { Autocomplete, TextField, CircularProgress } from "@mui/material";
import type { Activity } from "../api/types";

interface Props {
  activities: Activity[];
  selected: Activity | null;
  onChange: (activity: Activity | null) => void;
  loading: boolean;
}

export function ActivitySelector({
  activities,
  selected,
  onChange,
  loading,
}: Props) {
  return (
    <Autocomplete
      options={activities}
      getOptionLabel={(opt) => opt.naam}
      value={selected}
      onChange={(_, value) => onChange(value)}
      loading={loading}
      renderInput={(params) => (
        <TextField
          {...params}
          label="Activiteit"
          placeholder="Selecteer een activiteit"
          InputProps={{
            ...params.InputProps,
            endAdornment: (
              <>
                {loading && <CircularProgress size={20} />}
                {params.InputProps.endAdornment}
              </>
            ),
          }}
        />
      )}
    />
  );
}
