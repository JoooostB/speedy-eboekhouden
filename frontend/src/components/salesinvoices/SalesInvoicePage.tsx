import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Box,
  Typography,
  Alert,
  Button,
  TextField,
  Paper,
  Grid,
  IconButton,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  MenuItem,
  CircularProgress,
  Divider,
} from "@mui/material";
import { api } from "../../api/client";
import type {
  EmailTemplate,
  InvoiceLineItem,
  LedgerAccount,
  Relation,
  SettingsResponse,
} from "../../api/types";
import { useAuth } from "../../context/AuthContext";
import { RelationPicker } from "../shared/RelationPicker";
import { LedgerAccountPicker } from "../shared/LedgerAccountPicker";
import { VATCodePicker } from "../shared/VATCodePicker";
import { useLedgerAccounts } from "../../hooks/useLedgerAccounts";
import { useVATCodes } from "../../hooks/useVATCodes";
import { track } from "../../analytics";

/**
 * SalesInvoicePage lets users create sales invoices (verkoopfacturen)
 * via the e-boekhouden REST API.
 *
 * Accessibility:
 * - All form inputs have visible labels.
 * - Line items table uses proper <th> headings.
 * - Add/remove buttons have descriptive aria-labels.
 * - Error/success states are announced via aria-live.
 * - Icon-only delete buttons have aria-label with row context.
 */

interface LineItemState {
  id: number;
  quantity: number;
  description: string;
  pricePerUnit: number;
  vatCode: string;
  ledgerId: number;
  ledgerAccount: LedgerAccount | null;
}

let nextLineId = 1;

function createEmptyLine(): LineItemState {
  return {
    id: nextLineId++,
    quantity: 1,
    description: "",
    pricePerUnit: 0,
    vatCode: "HOOG_VERK_21",
    ledgerId: 0,
    ledgerAccount: null,
  };
}

export function SalesInvoicePage() {
  useAuth(); // ensure authenticated
  const navigate = useNavigate();
  const { data: ledgerAccounts } = useLedgerAccounts();
  const { data: vatCodes } = useVATCodes();

  const [hasRest, setHasRest] = useState<boolean | null>(null);
  const [templates, setTemplates] = useState<EmailTemplate[]>([]);

  // Form fields
  const [relatie, setRelatie] = useState<Relation | null>(null);
  const [betalingstermijn, setBetalingstermijn] = useState(30);
  const [sjabloonId, setSjabloonId] = useState<number | "">("");
  const [factuurnummer, setFactuurnummer] = useState("");
  const [datum, setDatum] = useState(new Date().toISOString().split("T")[0]);
  const [lines, setLines] = useState<LineItemState[]>([createEmptyLine()]);

  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  useEffect(() => {
    api.getSettings()
      .then((s: SettingsResponse) => setHasRest(s.hasRestAccessToken))
      .catch(() => setHasRest(false));
  }, []);

  // Fetch email templates when REST is available
  useEffect(() => {
    if (!hasRest) return;
    api.getEmailTemplates()
      .then((data) => setTemplates(data || []))
      .catch(() => {});
  }, [hasRest]);

  const updateLine = (id: number, field: keyof LineItemState, value: unknown) => {
    setLines((prev) =>
      prev.map((line) => {
        if (line.id !== id) return line;
        const updated = { ...line, [field]: value };
        // Sync ledgerId when ledgerAccount changes
        if (field === "ledgerAccount" && value) {
          updated.ledgerId = (value as LedgerAccount).id;
        }
        return updated;
      }),
    );
  };

  const addLine = () => setLines((prev) => [...prev, createEmptyLine()]);

  const removeLine = (id: number) => {
    setLines((prev) => {
      const filtered = prev.filter((l) => l.id !== id);
      // Always keep at least one line
      return filtered.length === 0 ? [createEmptyLine()] : filtered;
    });
  };

  const handleSubmit = async () => {
    if (!relatie) return;
    setSubmitting(true);
    setMessage(null);

    const regels: InvoiceLineItem[] = lines
      .filter((l) => l.description && l.pricePerUnit > 0)
      .map((l) => ({
        quantity: l.quantity,
        description: l.description,
        pricePerUnit: l.pricePerUnit,
        vatCode: l.vatCode,
        ledgerId: l.ledgerId,
      }));

    if (regels.length === 0) {
      setMessage({ type: "error", text: "Voeg minimaal een factuurregel toe met een omschrijving en prijs." });
      setSubmitting(false);
      return;
    }

    try {
      await api.createRestInvoice({
        relatieId: relatie.id,
        betalingstermijn,
        sjabloonId: sjabloonId ? Number(sjabloonId) : undefined,
        factuurnummer: factuurnummer || undefined,
        datum,
        regels,
      });
      track("Sales Invoice Created");
      setMessage({ type: "success", text: "Verkoopfactuur succesvol aangemaakt." });
      // Reset form
      setRelatie(null);
      setFactuurnummer("");
      setLines([createEmptyLine()]);
    } catch (err: any) {
      setMessage({ type: "error", text: err.message || "Aanmaken mislukt" });
    } finally {
      setSubmitting(false);
    }
  };

  const lineTotal = (line: LineItemState) => line.quantity * line.pricePerUnit;
  const total = lines.reduce((sum, l) => sum + lineTotal(l), 0);

  const formatCurrency = (amount: number) =>
    new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(amount);

  // REST not configured
  if (hasRest === false) {
    return (
      <Box>
        <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
          Verkoopfactuur aanmaken
        </Typography>
        <Alert
          severity="warning"
          action={
            <Button color="inherit" size="small" onClick={() => navigate("/instellingen")}>
              Naar instellingen
            </Button>
          }
        >
          REST API-token is niet ingesteld. Configureer je REST-token in de instellingen om verkoopfacturen aan te maken.
        </Alert>
      </Box>
    );
  }

  if (hasRest === null) {
    return (
      <Box>
        <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
          Verkoopfactuur aanmaken
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
        Verkoopfactuur aanmaken
      </Typography>

      {message && (
        <Alert severity={message.type} sx={{ mb: 3 }} role="status" aria-live="polite">
          {message.text}
        </Alert>
      )}

      {/* Header fields */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" component="h2" gutterBottom>
          Factuurgegevens
        </Typography>
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, sm: 6 }}>
            <RelationPicker value={relatie} onChange={setRelatie} />
          </Grid>
          <Grid size={{ xs: 12, sm: 6 }}>
            <TextField
              fullWidth
              label="Factuurnummer"
              value={factuurnummer}
              onChange={(e) => setFactuurnummer(e.target.value)}
              size="small"
              helperText="Laat leeg voor automatische nummering"
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 4 }}>
            <TextField
              fullWidth
              label="Factuurdatum"
              type="date"
              value={datum}
              onChange={(e) => setDatum(e.target.value)}
              size="small"
              slotProps={{ inputLabel: { shrink: true } }}
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 4 }}>
            <TextField
              fullWidth
              label="Betalingstermijn (dagen)"
              type="number"
              value={betalingstermijn}
              onChange={(e) => setBetalingstermijn(parseInt(e.target.value) || 0)}
              size="small"
              slotProps={{ htmlInput: { min: 0 } }}
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 4 }}>
            <TextField
              select
              fullWidth
              label="E-mailsjabloon"
              value={sjabloonId}
              onChange={(e) => setSjabloonId(e.target.value ? Number(e.target.value) : "")}
              size="small"
            >
              <MenuItem value="">Geen sjabloon</MenuItem>
              {templates.map((t) => (
                <MenuItem key={t.id} value={t.id}>{t.naam}</MenuItem>
              ))}
            </TextField>
          </Grid>
        </Grid>
      </Paper>

      {/* Line items */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mb: 2 }}>
          <Typography variant="h6" component="h2">
            Factuurregels
          </Typography>
          <Button variant="outlined" size="small" onClick={addLine} aria-label="Factuurregel toevoegen">
            Regel toevoegen
          </Button>
        </Box>

        <TableContainer>
          <Table size="small" aria-label="Factuurregels">
            <TableHead>
              <TableRow>
                <TableCell sx={{ width: 80 }}>Aantal</TableCell>
                <TableCell>Omschrijving</TableCell>
                <TableCell sx={{ width: 120 }}>Prijs p/s</TableCell>
                <TableCell sx={{ width: 180 }}>BTW-code</TableCell>
                <TableCell sx={{ width: 200 }}>Grootboekrekening</TableCell>
                <TableCell align="right" sx={{ width: 100 }}>Totaal</TableCell>
                <TableCell sx={{ width: 48 }}><span className="sr-only">Acties</span></TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {lines.map((line, idx) => (
                <TableRow key={line.id}>
                  <TableCell>
                    <TextField
                      type="number"
                      value={line.quantity}
                      onChange={(e) => updateLine(line.id, "quantity", parseFloat(e.target.value) || 0)}
                      size="small"
                      slotProps={{ htmlInput: { min: 0, step: 0.25, "aria-label": `Aantal regel ${idx + 1}` } }}
                      sx={{ width: "100%" }}
                    />
                  </TableCell>
                  <TableCell>
                    <TextField
                      value={line.description}
                      onChange={(e) => updateLine(line.id, "description", e.target.value)}
                      size="small"
                      fullWidth
                      placeholder="Omschrijving"
                      slotProps={{ htmlInput: { "aria-label": `Omschrijving regel ${idx + 1}` } }}
                    />
                  </TableCell>
                  <TableCell>
                    <TextField
                      type="number"
                      value={line.pricePerUnit}
                      onChange={(e) => updateLine(line.id, "pricePerUnit", parseFloat(e.target.value) || 0)}
                      size="small"
                      slotProps={{ htmlInput: { min: 0, step: 0.01, "aria-label": `Prijs per stuk regel ${idx + 1}` } }}
                      sx={{ width: "100%" }}
                    />
                  </TableCell>
                  <TableCell>
                    <VATCodePicker
                      codes={vatCodes}
                      value={line.vatCode}
                      onChange={(val) => updateLine(line.id, "vatCode", val)}
                    />
                  </TableCell>
                  <TableCell>
                    <LedgerAccountPicker
                      accounts={ledgerAccounts}
                      value={line.ledgerAccount}
                      onChange={(val) => updateLine(line.id, "ledgerAccount", val)}
                      label="Grootboek"
                    />
                  </TableCell>
                  <TableCell align="right" sx={{ fontWeight: 600 }}>
                    {formatCurrency(lineTotal(line))}
                  </TableCell>
                  <TableCell>
                    <IconButton
                      size="small"
                      onClick={() => removeLine(line.id)}
                      aria-label={`Regel ${idx + 1} verwijderen`}
                      disabled={lines.length === 1}
                    >
                      {/* Trash icon - inline SVG */}
                      <Box
                        component="svg"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth={2}
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        aria-hidden="true"
                        sx={{ width: 16, height: 16 }}
                      >
                        <polyline points="3 6 5 6 21 6" />
                        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                      </Box>
                    </IconButton>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>

        <Divider sx={{ my: 2 }} />
        <Box sx={{ display: "flex", justifyContent: "flex-end", gap: 2, alignItems: "center" }}>
          <Typography variant="body1" fontWeight={600}>
            Totaal excl. BTW: {formatCurrency(total)}
          </Typography>
        </Box>
      </Paper>

      {/* Submit */}
      <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
        <Button
          variant="contained"
          size="large"
          onClick={handleSubmit}
          disabled={submitting || !relatie || lines.every((l) => !l.description)}
        >
          {submitting ? "Aanmaken..." : "Factuur aanmaken"}
        </Button>
      </Box>
    </Box>
  );
}
