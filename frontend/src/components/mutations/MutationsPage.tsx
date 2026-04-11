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
  Chip,
} from "@mui/material";
import { api } from "../../api/client";
import type { Mutatie, SettingsResponse } from "../../api/types";
import { useAuth } from "../../context/AuthContext";
import { track } from "../../analytics";

/**
 * MutationsPage shows bookkeeping mutations for a selected date range
 * from the e-boekhouden SOAP API.
 *
 * Accessibility:
 * - Date inputs have visible labels.
 * - Mutation amounts are colored AND prefixed with +/- text.
 * - Loading state announced via aria-live.
 * - Table uses proper header hierarchy.
 */

function getDefaultDates() {
  const now = new Date();
  const thirtyDaysAgo = new Date(now);
  thirtyDaysAgo.setDate(thirtyDaysAgo.getDate() - 30);
  return {
    van: thirtyDaysAgo.toISOString().split("T")[0],
    tot: now.toISOString().split("T")[0],
  };
}

export function MutationsPage() {
  const { eboekhoudenConnected } = useAuth();
  const navigate = useNavigate();

  const defaults = getDefaultDates();
  const [datumVan, setDatumVan] = useState(defaults.van);
  const [datumTot, setDatumTot] = useState(defaults.tot);
  const [mutaties, setMutaties] = useState<Mutatie[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [hasSoap, setHasSoap] = useState<boolean | null>(null);

  useEffect(() => {
    api.getSettings()
      .then((s: SettingsResponse) => setHasSoap(s.hasSoapCredentials))
      .catch(() => setHasSoap(false));
  }, []);

  const fetchMutaties = () => {
    if (!datumVan || !datumTot) return;
    setLoading(true);
    setError("");
    api.getSoapMutaties(datumVan, datumTot)
      .then((data) => {
        setMutaties(data || []);
        track("Mutations Viewed");
      })
      .catch((err: any) => setError(err.message || "Ophalen mislukt"))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (hasSoap && eboekhoudenConnected) {
      fetchMutaties();
    }
  }, [hasSoap, eboekhoudenConnected]);

  const formatCurrency = (amount: number) =>
    new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(amount);

  const formatDate = (dateStr: string) => {
    if (!dateStr) return "";
    const d = new Date(dateStr);
    return d.toLocaleDateString("nl-NL", { day: "2-digit", month: "2-digit", year: "numeric" });
  };

  if (hasSoap === false) {
    return (
      <Box>
        <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
          Mutaties
        </Typography>
        <Alert
          severity="warning"
          action={
            <Button color="inherit" size="small" onClick={() => navigate("/instellingen")}>
              Naar instellingen
            </Button>
          }
        >
          SOAP API-gegevens zijn niet ingesteld. Configureer je SOAP-gegevens in de instellingen om mutaties te bekijken.
        </Alert>
      </Box>
    );
  }

  if (hasSoap === null) {
    return (
      <Box>
        <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
          Mutaties
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
        Mutaties
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
              onClick={fetchMutaties}
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
          <CircularProgress aria-label="Mutaties laden" />
        </Box>
      ) : mutaties.length === 0 ? (
        <Paper sx={{ p: 4, textAlign: "center" }}>
          <Typography color="text.secondary">
            Geen mutaties gevonden voor de geselecteerde periode.
          </Typography>
        </Paper>
      ) : (
        <TableContainer component={Paper}>
          <Table aria-label="Mutaties overzicht">
            <TableHead>
              <TableRow>
                <TableCell>MutatieNr</TableCell>
                <TableCell>Datum</TableCell>
                <TableCell>Rekening</TableCell>
                <TableCell>Soort</TableCell>
                <TableCell align="right">Bedrag</TableCell>
                <TableCell>Omschrijving</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {mutaties.map((m, idx) => (
                <TableRow key={`${m.mutatieNr}-${idx}`}>
                  <TableCell sx={{ fontFamily: "monospace" }}>{m.mutatieNr}</TableCell>
                  <TableCell>{formatDate(m.datum)}</TableCell>
                  <TableCell>{m.rekening}</TableCell>
                  <TableCell>
                    <Chip label={m.soort} size="small" variant="outlined" />
                  </TableCell>
                  <TableCell
                    align="right"
                    sx={{
                      fontWeight: 600,
                      color: m.bedrag >= 0 ? "success.main" : "error.main",
                    }}
                  >
                    {m.bedrag >= 0 ? "+" : ""}{formatCurrency(m.bedrag)}
                  </TableCell>
                  <TableCell sx={{ maxWidth: 300, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {m.omschrijving}
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
