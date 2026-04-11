import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Box,
  Typography,
  Alert,
  Button,
  TextField,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  CircularProgress,
  Grid,
} from "@mui/material";
import { api } from "../../api/client";
import type { Saldo, SettingsResponse } from "../../api/types";
import { useAuth } from "../../context/AuthContext";
import { track } from "../../analytics";

/**
 * BalancesPage shows account balances for a selected date range
 * from the e-boekhouden SOAP API.
 *
 * Accessibility:
 * - Date inputs have visible labels via TextField.
 * - Saldo amounts are colored green/red AND prefixed with +/- text,
 *   ensuring no information is conveyed by color alone (WCAG 1.4.1).
 * - Loading state announced via aria-live region.
 */

function getDefaultDates() {
  const now = new Date();
  const year = now.getFullYear();
  // Default to current calendar year
  const van = `${year}-01-01`;
  const tot = now.toISOString().split("T")[0];
  return { van, tot };
}

export function BalancesPage() {
  const { eboekhoudenConnected } = useAuth();
  const navigate = useNavigate();

  const defaults = getDefaultDates();
  const [datumVan, setDatumVan] = useState(defaults.van);
  const [datumTot, setDatumTot] = useState(defaults.tot);
  const [saldi, setSaldi] = useState<Saldo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [hasSoap, setHasSoap] = useState<boolean | null>(null);

  useEffect(() => {
    api.getSettings()
      .then((s: SettingsResponse) => setHasSoap(s.hasSoapCredentials))
      .catch(() => setHasSoap(false));
  }, []);

  const fetchSaldi = () => {
    if (!datumVan || !datumTot) return;
    setLoading(true);
    setError("");
    api.getSaldi(datumVan, datumTot)
      .then((data) => {
        setSaldi(data || []);
        track("Balances Viewed");
      })
      .catch((err: any) => setError(err.message || "Ophalen mislukt"))
      .finally(() => setLoading(false));
  };

  // Auto-fetch on mount when SOAP is available
  useEffect(() => {
    if (hasSoap && eboekhoudenConnected) {
      fetchSaldi();
    }
  }, [hasSoap, eboekhoudenConnected]);

  const formatCurrency = (amount: number) =>
    new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(amount);

  if (hasSoap === false) {
    return (
      <Box>
        <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
          Saldi
        </Typography>
        <Alert
          severity="warning"
          action={
            <Button color="inherit" size="small" onClick={() => navigate("/instellingen")}>
              Naar instellingen
            </Button>
          }
        >
          SOAP API-gegevens zijn niet ingesteld. Configureer je SOAP-gegevens in de instellingen om saldi te bekijken.
        </Alert>
      </Box>
    );
  }

  if (hasSoap === null) {
    return (
      <Box>
        <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
          Saldi
        </Typography>
        <Box sx={{ display: "flex", justifyContent: "center", py: 6 }} role="status" aria-live="polite">
          <CircularProgress aria-label="Instellingen laden" />
        </Box>
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
        Saldi
      </Typography>

      {/* Date range filter */}
      <Paper sx={{ p: 2, mb: 3 }}>
        <Grid container spacing={2} alignItems="flex-end">
          <Grid size={{ xs: 12, sm: 4 }}>
            <TextField
              fullWidth
              label="Van"
              type="date"
              value={datumVan}
              onChange={(e) => setDatumVan(e.target.value)}
              size="small"
              slotProps={{ inputLabel: { shrink: true } }}
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 4 }}>
            <TextField
              fullWidth
              label="Tot"
              type="date"
              value={datumTot}
              onChange={(e) => setDatumTot(e.target.value)}
              size="small"
              slotProps={{ inputLabel: { shrink: true } }}
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 4 }}>
            <Button
              variant="contained"
              onClick={fetchSaldi}
              disabled={loading || !datumVan || !datumTot}
              fullWidth
            >
              Ophalen
            </Button>
          </Grid>
        </Grid>
      </Paper>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {loading ? (
        <Box sx={{ display: "flex", justifyContent: "center", py: 6 }} role="status" aria-live="polite">
          <CircularProgress aria-label="Saldi laden" />
        </Box>
      ) : saldi.length === 0 ? (
        <Paper sx={{ p: 4, textAlign: "center" }}>
          <Typography color="text.secondary">
            Geen saldi gevonden voor de geselecteerde periode.
          </Typography>
        </Paper>
      ) : (
        <TableContainer component={Paper}>
          <Table aria-label="Saldi overzicht">
            <TableHead>
              <TableRow>
                <TableCell>Code</TableCell>
                <TableCell>Omschrijving</TableCell>
                <TableCell align="right">Saldo</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {saldi.map((s, idx) => (
                <TableRow key={`${s.code}-${idx}`}>
                  <TableCell sx={{ fontFamily: "monospace" }}>{s.code}</TableCell>
                  <TableCell>{s.omschrijving}</TableCell>
                  <TableCell
                    align="right"
                    sx={{
                      fontWeight: 600,
                      color: s.saldo >= 0 ? "success.main" : "error.main",
                    }}
                  >
                    {/* +/- prefix ensures info is not conveyed by color alone */}
                    {s.saldo >= 0 ? "+" : ""}{formatCurrency(s.saldo)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Box>
  );
}
