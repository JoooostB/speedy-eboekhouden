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
import type { Employee, EntryResult } from "../api/types";
import { employeeNames, entryResultLabel } from "./hours/entryResultLabel";

interface Props {
  results: EntryResult[] | null;
  loading: boolean;
  total: number;
  /** For showing names instead of employee ids. */
  employees?: Employee[];
  onReset?: () => void;
}

export function SubmitResults({ results, loading, total, employees, onReset }: Props) {
  if (loading) {
    return (
      <Box sx={{ mt: 3 }}>
        <Typography variant="body2" gutterBottom>
          Uren indienen… ({total} {total === 1 ? "regel" : "regels"})
        </Typography>
        <LinearProgress />
      </Box>
    );
  }

  if (!results) return null;

  const names = employeeNames(employees);
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
            Uren ingediend
          </Typography>
          <Typography variant="body1" color="text.secondary" sx={{ mb: 3 }}>
            {successes} {successes === 1 ? "regel staat" : "regels staan"} nu in e-Boekhouden.
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
            {successes} van de {results.length} {results.length === 1 ? "regel" : "regels"} gelukt, {failures} mislukt.
            De fouten staan hieronder.
          </Alert>
          <List dense>
            {results
              .filter((r) => r.status === "error")
              .map((r, i) => (
                <ListItem key={i}>
                  <ListItemIcon>
                    <ErrorIcon color="error" />
                  </ListItemIcon>
                  <ListItemText primary={entryResultLabel(r, names)} secondary={r.error} />
                </ListItem>
              ))}
          </List>
          {successes > 0 && (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 2, mb: 1 }}>
                Gelukt:
              </Typography>
              <List dense>
                {results.filter((r) => r.status === "ok").slice(0, 5).map((r, i) => (
                  <ListItem key={i}>
                    <ListItemIcon>
                      <CheckCircleIcon color="success" />
                    </ListItemIcon>
                    <ListItemText primary={entryResultLabel(r, names)} />
                  </ListItem>
                ))}
                {successes > 5 && (
                  <ListItem>
                    <ListItemText
                      primary={`en nog ${successes - 5}`}
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
