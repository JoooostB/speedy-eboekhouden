import { useState } from "react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
  Alert,
  CircularProgress,
} from "@mui/material";
import { useAuth } from "../context/AuthContext";

export function MFADialog() {
  const { needsMFA, submitMFA } = useAuth();
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await submitMFA(code);
    } catch (err) {
      setError(err instanceof Error ? err.message : "MFA failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={needsMFA} maxWidth="xs" fullWidth>
      <form onSubmit={handleSubmit}>
        <DialogTitle>Verificatiecode</DialogTitle>
        <DialogContent>
          {error && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {error}
            </Alert>
          )}
          <TextField
            fullWidth
            label="Code"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            margin="normal"
            autoFocus
            inputProps={{ maxLength: 8 }}
            placeholder="Voer je verificatiecode in"
          />
        </DialogContent>
        <DialogActions>
          <Button type="submit" variant="contained" disabled={loading}>
            {loading ? <CircularProgress size={24} /> : "Verifiëren"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
