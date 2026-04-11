import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Box,
  Typography,
  Alert,
  Button,
  Tabs,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  CircularProgress,
  Chip,
} from "@mui/material";
import { api } from "../../api/client";
import type { OpenPost, SettingsResponse } from "../../api/types";
import { useAuth } from "../../context/AuthContext";
import { track } from "../../analytics";

/**
 * OpenItemsPage shows outstanding debtor/creditor items from
 * the e-boekhouden SOAP API.
 *
 * Accessibility:
 * - Tab interface uses role="tablist" / role="tab" / role="tabpanel"
 *   via MUI's built-in ARIA handling.
 * - Overdue items are indicated with both color AND a text chip,
 *   satisfying WCAG 1.4.1 (Use of Color).
 * - Loading state announced via aria-live region.
 * - Table uses <th scope="col"> via MUI defaults for header cells.
 */

type Soort = "Debiteuren" | "Crediteuren";

export function OpenItemsPage() {
  const { eboekhoudenConnected } = useAuth();
  const navigate = useNavigate();

  const [soort, setSoort] = useState<Soort>("Debiteuren");
  const [items, setItems] = useState<OpenPost[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [hasSoap, setHasSoap] = useState<boolean | null>(null);

  // Check if SOAP credentials are configured
  useEffect(() => {
    api.getSettings()
      .then((s: SettingsResponse) => setHasSoap(s.hasSoapCredentials))
      .catch(() => setHasSoap(false));
  }, []);

  // Fetch open items when soort changes
  useEffect(() => {
    if (!hasSoap || !eboekhoudenConnected) return;

    setLoading(true);
    setError("");
    api.getOpenPosten(soort)
      .then((data) => {
        setItems(data || []);
        track("Open Items Viewed", { soort });
      })
      .catch((err: any) => setError(err.message || "Ophalen mislukt"))
      .finally(() => setLoading(false));
  }, [soort, hasSoap, eboekhoudenConnected]);

  const isOverdue = (item: OpenPost) => {
    if (!item.vervalDatum) return false;
    return new Date(item.vervalDatum) < new Date();
  };

  const formatCurrency = (amount: number) =>
    new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(amount);

  const formatDate = (dateStr: string) => {
    if (!dateStr) return "";
    const d = new Date(dateStr);
    return d.toLocaleDateString("nl-NL", { day: "2-digit", month: "2-digit", year: "numeric" });
  };

  // SOAP not configured — show guidance
  if (hasSoap === false) {
    return (
      <Box>
        <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
          Openstaande posten
        </Typography>
        <Alert
          severity="warning"
          action={
            <Button color="inherit" size="small" onClick={() => navigate("/instellingen")}>
              Naar instellingen
            </Button>
          }
        >
          SOAP API-gegevens zijn niet ingesteld. Configureer je SOAP-gegevens in de instellingen om openstaande posten te bekijken.
        </Alert>
      </Box>
    );
  }

  // Still checking settings
  if (hasSoap === null) {
    return (
      <Box>
        <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
          Openstaande posten
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
        Openstaande posten
      </Typography>

      {/* Tab toggle for Debiteuren / Crediteuren */}
      <Box sx={{ borderBottom: 1, borderColor: "divider", mb: 3 }}>
        <Tabs
          value={soort}
          onChange={(_, val) => setSoort(val as Soort)}
          aria-label="Soort openstaande posten"
        >
          <Tab value="Debiteuren" label="Debiteuren" id="tab-debiteuren" aria-controls="tabpanel-openitems" />
          <Tab value="Crediteuren" label="Crediteuren" id="tab-crediteuren" aria-controls="tabpanel-openitems" />
        </Tabs>
      </Box>

      {/* Content panel */}
      <Box
        role="tabpanel"
        id="tabpanel-openitems"
        aria-labelledby={soort === "Debiteuren" ? "tab-debiteuren" : "tab-crediteuren"}
      >
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

        {loading ? (
          <Box sx={{ display: "flex", justifyContent: "center", py: 6 }} role="status" aria-live="polite">
            <CircularProgress aria-label="Openstaande posten laden" />
          </Box>
        ) : items.length === 0 ? (
          <Paper sx={{ p: 4, textAlign: "center" }}>
            <Typography color="text.secondary">
              Geen openstaande posten gevonden voor {soort.toLowerCase()}.
            </Typography>
          </Paper>
        ) : (
          <TableContainer component={Paper}>
            <Table aria-label={`Openstaande posten ${soort.toLowerCase()}`}>
              <TableHead>
                <TableRow>
                  <TableCell>Factuurnr</TableCell>
                  <TableCell>Relatie</TableCell>
                  <TableCell>Datum</TableCell>
                  <TableCell align="right">Bedrag</TableCell>
                  <TableCell align="right">Openstaand</TableCell>
                  <TableCell>Status</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {items.map((item, idx) => {
                  const overdue = isOverdue(item);
                  return (
                    <TableRow
                      key={`${item.factuurnummer}-${idx}`}
                      sx={overdue ? { bgcolor: "error.50" } : undefined}
                    >
                      <TableCell>{item.factuurnummer}</TableCell>
                      <TableCell>{item.relatie}</TableCell>
                      <TableCell>{formatDate(item.datum)}</TableCell>
                      <TableCell align="right">{formatCurrency(item.bedrag)}</TableCell>
                      <TableCell align="right" sx={{ fontWeight: 600 }}>
                        {formatCurrency(item.openstaand)}
                      </TableCell>
                      <TableCell>
                        {overdue ? (
                          <Chip label="Vervallen" color="error" size="small" />
                        ) : (
                          <Chip label="Lopend" color="default" size="small" variant="outlined" />
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </Box>
    </Box>
  );
}
