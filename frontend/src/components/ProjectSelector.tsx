import { Autocomplete, TextField, CircularProgress } from "@mui/material";
import type { Project } from "../api/types";

interface Props {
  projects: Project[];
  selected: Project | null;
  onChange: (project: Project | null) => void;
  loading: boolean;
}

export function ProjectSelector({
  projects,
  selected,
  onChange,
  loading,
}: Props) {
  return (
    <Autocomplete
      options={projects}
      getOptionLabel={(opt) =>
        opt.relatieBedrijf ? `${opt.naam} — ${opt.relatieBedrijf}` : opt.naam
      }
      value={selected}
      onChange={(_, value) => onChange(value)}
      loading={loading}
      renderInput={(params) => (
        <TextField
          {...params}
          label="Project"
          placeholder="Zoek een project"
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
