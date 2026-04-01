import { Box, TextField, ToggleButton, ToggleButtonGroup } from "@mui/material";

interface Props {
  value: string;
  onChange: (value: string) => void;
}

const PRESETS = ["4.00", "6.00", "8.00"];

export function HoursInput({ value, onChange }: Props) {
  return (
    <Box sx={{ display: "flex", gap: 2, alignItems: "center" }}>
      <TextField
        label="Uren"
        type="number"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        inputProps={{ min: 0.25, max: 24, step: 0.25 }}
        sx={{ width: 120 }}
      />
      <ToggleButtonGroup
        value={value}
        exclusive
        onChange={(_, v) => v && onChange(v)}
        size="small"
      >
        {PRESETS.map((p) => (
          <ToggleButton key={p} value={p}>
            {p}
          </ToggleButton>
        ))}
      </ToggleButtonGroup>
    </Box>
  );
}
