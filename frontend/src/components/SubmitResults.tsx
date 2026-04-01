import {
  Box,
  Typography,
  Alert,
  AlertTitle,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  LinearProgress,
  Paper,
  Button,
} from "@mui/material";
import CheckCircleOutlineIcon from "@mui/icons-material/CheckCircleOutline";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import ErrorIcon from "@mui/icons-material/Error";
import type { EntryResult } from "../api/types";

interface Props {
  results: EntryResult[] | null;
  loading: boolean;
  total: number;
  onReset?: () => void;
}

export function SubmitResults({ results, loading, total, onReset }: Props) {
  if (loading) {
    return (
      <Box sx={{ mt: 3 }}>
        <Typography variant="body2" gutterBottom>
          Bezig met indienen... ({total} items)
        </Typography>
        <LinearProgress />
      </Box>
    );
  }

  if (!results) return null;

  const successes = results.filter((r) => r.status === "ok").length;
  const failures = results.filter((r) => r.status === "error").length;
  const allSuccess = failures === 0;

  return (
    <Box sx={{ mt: 3 }}>
      {allSuccess ? (
        <Paper
          elevation={0}
          sx={{
            p: 4,
            textAlign: "center",
            bgcolor: "#f0fdf4",
            border: "1px solid #bbf7d0",
            borderRadius: 2,
          }}
        >
          <CheckCircleOutlineIcon sx={{ fontSize: 56, color: "#16a34a", mb: 2 }} />
          <Typography variant="h5" sx={{ fontWeight: 700, color: "#15803d", mb: 1 }}>
            Uren ingediend!
          </Typography>
          <Typography variant="body1" color="text.secondary" sx={{ mb: 3 }}>
            {successes} {successes === 1 ? "item" : "items"} succesvol ingevoerd in e-boekhouden.
          </Typography>
          {onReset && (
            <Button variant="outlined" color="success" onClick={onReset}>
              Nieuwe invoer
            </Button>
          )}
        </Paper>
      ) : (
        <>
          <Alert severity="warning" sx={{ mb: 2 }}>
            <AlertTitle>Gedeeltelijk ingediend</AlertTitle>
            {successes} gelukt, {failures} mislukt van {results.length} items.
            Controleer de fouten hieronder.
          </Alert>
          <List dense>
            {results
              .filter((r) => r.status === "error")
              .map((r, i) => (
                <ListItem key={i}>
                  <ListItemIcon>
                    <ErrorIcon color="error" />
                  </ListItemIcon>
                  <ListItemText
                    primary={`Medewerker ${r.employeeId} — ${r.date}`}
                    secondary={r.error}
                  />
                </ListItem>
              ))}
          </List>
          {successes > 0 && (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 2, mb: 1 }}>
                Succesvol ingevoerd:
              </Typography>
              <List dense>
                {results.filter((r) => r.status === "ok").slice(0, 5).map((r, i) => (
                  <ListItem key={i}>
                    <ListItemIcon>
                      <CheckCircleIcon color="success" />
                    </ListItemIcon>
                    <ListItemText primary={`Medewerker ${r.employeeId} — ${r.date}`} />
                  </ListItem>
                ))}
                {successes > 5 && (
                  <ListItem>
                    <ListItemText
                      primary={`...en ${successes - 5} meer`}
                      sx={{ color: "text.secondary" }}
                    />
                  </ListItem>
                )}
              </List>
            </>
          )}
          {onReset && (
            <Button variant="outlined" onClick={onReset} sx={{ mt: 2 }}>
              Nieuwe invoer
            </Button>
          )}
        </>
      )}
    </Box>
  );
}
