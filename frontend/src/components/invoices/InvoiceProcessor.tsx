import { useState, useCallback } from "react";
import {
  Box,
  Typography,
  Paper,
  Button,
  Alert,
  CircularProgress,
  TextField,
  Grid,
} from "@mui/material";
import { api } from "../../api/client";
import { useAuth } from "../../context/AuthContext";
import type { InvoiceData, LedgerAccount, Relation } from "../../api/types";
import { MUTATIE_SOORT_CODES } from "../../api/types";
import { LedgerAccountPicker } from "../shared/LedgerAccountPicker";
import { RelationPicker } from "../shared/RelationPicker";
import { VATCodePicker } from "../shared/VATCodePicker";
import { useLedgerAccounts } from "../../hooks/useLedgerAccounts";
import { useVATCodes } from "../../hooks/useVATCodes";
import { track } from "../../analytics";

export function InvoiceProcessor() {
  const { eboekhoudenConnected } = useAuth();
  const { data: ledgerAccounts } = useLedgerAccounts();
  const { data: vatCodes } = useVATCodes();

  const [file, setFile] = useState<File | null>(null);
  const [analyzing, setAnalyzing] = useState(false);
  const [invoice, setInvoice] = useState<InvoiceData | null>(null);
  const [_pdfBase64, setPdfBase64] = useState(""); // stored for future archive upload
  const [error, setError] = useState("");
  const [success, setSuccess] = useState<{ mutNr: number } | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Editable fields
  const [leverancier, setLeverancier] = useState("");
  const [factuurnummer, setFactuurnummer] = useState("");
  const [datum, setDatum] = useState("");
  const [bedragExcl, setBedragExcl] = useState(0);
  const [bedragIncl, setBedragIncl] = useState(0);
  const [btwBedrag, setBtwBedrag] = useState(0);
  const [btwCode, setBtwCode] = useState("HOOG_INK_21");
  const [omschrijving, setOmschrijving] = useState("");
  const [tegenRekening, setTegenRekening] = useState<LedgerAccount | null>(null);
  const [relatie, setRelatie] = useState<Relation | null>(null);

  const handleFileSelect = useCallback(async (selectedFile: File) => {
    setFile(selectedFile);
    setError("");
    setSuccess(null);
    setAnalyzing(true);

    try {
      const result = await api.analyzeInvoice(selectedFile);
      const inv = result.invoice;
      setInvoice(inv);
      setPdfBase64(""); // PDF stored in R2, referenced by uploadKey

      track("Invoice Analyzed", { confidence: String(Math.round(inv.confidence * 100)) });

      // Populate editable fields from AI extraction
      setLeverancier(inv.leverancier);
      setFactuurnummer(inv.factuurnummer);
      setDatum(inv.datum);
      setBedragExcl(inv.bedragExclBtw);
      setBedragIncl(inv.bedragInclBtw);
      setBtwBedrag(inv.btwBedrag);
      setBtwCode(inv.btwCode);
      setOmschrijving(inv.omschrijving);

      // Match grootboekrekening
      const match = ledgerAccounts.find((a) => a.code === inv.grootboekcode);
      if (match) setTegenRekening(match);
    } catch (err: any) {
      track("Invoice Analyze Error", { reason: err.message || "unknown" });
      setError(err.message || "Factuuranalyse mislukt");
    } finally {
      setAnalyzing(false);
    }
  }, [ledgerAccounts]);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    const droppedFile = e.dataTransfer.files[0];
    if (droppedFile?.type === "application/pdf") {
      handleFileSelect(droppedFile);
    }
  }, [handleFileSelect]);

  const handleSubmit = async () => {
    if (!tegenRekening) {
      setError("Selecteer een tegenrekening");
      return;
    }

    setError("");
    setSubmitting(true);
    try {
      // First upload to archive, then create mutation
      // For now, create the mutation directly (archive linking is a follow-up)
      const payload = {
        mutatie: {
          rekening: 16, // Default crediteuren account — user should override
          relatieId: relatie?.id,
          datum,
          termijn: 30,
          factuur: factuurnummer,
          soort: MUTATIE_SOORT_CODES.FactuurOntvangen,
          inEx: "EX",
          omschrijving: omschrijving.slice(0, 200),
        },
        mutatieRegels: [{
          index: 0,
          bedrag: bedragExcl,
          tegenRekening: tegenRekening.id,
          bedragExclusief: bedragExcl,
          bedragInclusief: bedragIncl,
          btwCode,
          btw: btwBedrag,
        }],
      };

      const result = await api.submitInvoice(payload);
      track("Invoice Submitted");
      setSuccess({ mutNr: result.mutNr });
    } catch (err: any) {
      track("Invoice Submit Error", { reason: err.message || "unknown" });
      setError(err.message || "Factuur indienen mislukt");
    } finally {
      setSubmitting(false);
    }
  };

  const reset = () => {
    setFile(null);
    setInvoice(null);
    setPdfBase64("");
    setError("");
    setSuccess(null);
    setLeverancier("");
    setFactuurnummer("");
    setDatum("");
    setBedragExcl(0);
    setBedragIncl(0);
    setBtwBedrag(0);
    setBtwCode("HOOG_INK_21");
    setOmschrijving("");
    setTegenRekening(null);
    setRelatie(null);
  };

  if (!eboekhoudenConnected) {
    return (
      <Alert
        severity="warning"
        action={
          <Button
            color="inherit"
            size="small"
            onClick={() => window.dispatchEvent(new CustomEvent("eb:connect"))}
            sx={{ fontWeight: 600, whiteSpace: "nowrap" }}
          >
            Verbinden
          </Button>
        }
      >
        Verbind met e-Boekhouden om facturen te verwerken.
      </Alert>
    );
  }

  if (success) {
    return (
      <Box sx={{ textAlign: "center", py: 6 }}>
        <Box
          component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
          strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
          sx={{ width: 64, height: 64, color: "success.main", mb: 2 }}
        >
          <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
          <polyline points="22 4 12 14.01 9 11.01" />
        </Box>
        <Typography variant="h5" gutterBottom>Factuur verwerkt</Typography>
        <Typography color="text.secondary" gutterBottom>
          Mutatienummer: {success.mutNr}
        </Typography>
        <Button variant="contained" onClick={reset} sx={{ mt: 2 }}>
          Volgende factuur
        </Button>
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h4" component="h1" gutterBottom fontWeight={600}>
        Facturen verwerken
      </Typography>

      {error && (
        <Alert
          severity="error"
          sx={{ mb: 2 }}
          action={
            error.includes("no_api_key") || error.includes("API-sleutel") ? (
              <Button
                color="inherit"
                size="small"
                href="https://console.anthropic.com/settings/keys"
                target="_blank"
                rel="noopener noreferrer"
                sx={{ fontWeight: 600, whiteSpace: "nowrap" }}
              >
                Sleutel aanmaken
              </Button>
            ) : undefined
          }
        >
          {error}
        </Alert>
      )}

      {!invoice ? (
        <Paper
          sx={{
            border: "2px dashed",
            borderColor: "grey.300",
            borderRadius: 3,
            bgcolor: "grey.50",
            cursor: "pointer",
            transition: "border-color 0.2s, background-color 0.2s",
            "&:hover": { borderColor: "primary.light", bgcolor: "primary.light" },
            "&:focus-within": { borderColor: "primary.main", bgcolor: "primary.light" },
          }}
          onDragOver={(e: React.DragEvent) => { e.preventDefault(); }}
          onDrop={handleDrop}
          role="region"
          aria-label="Factuur uploaden"
        >
          <label htmlFor="invoice-upload" style={{ display: "block", cursor: "pointer", padding: "64px 24px", textAlign: "center" }}>
            {analyzing ? (
              <Box sx={{ display: "flex", flexDirection: "column", alignItems: "center" }}>
                <CircularProgress sx={{ mb: 2 }} aria-label="Factuur wordt geanalyseerd" />
                <Typography>Factuur wordt geanalyseerd door AI...</Typography>
              </Box>
            ) : (
              <Box sx={{ display: "flex", flexDirection: "column", alignItems: "center" }}>
                <Box
                  component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                  strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
                  sx={{ width: 48, height: 48, color: "primary.main", mb: 2 }}
                >
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                  <polyline points="17 8 12 3 7 8" />
                  <line x1="12" y1="3" x2="12" y2="15" />
                </Box>
                <Typography variant="h6" gutterBottom>
                  Sleep een PDF-factuur hierheen
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                  of klik om een bestand te selecteren
                </Typography>
                <Button variant="outlined" component="span" tabIndex={-1}>
                  Bestand kiezen
                </Button>
              </Box>
            )}
            <input
              id="invoice-upload"
              type="file"
              accept=".pdf"
              hidden
              onChange={(e) => e.target.files?.[0] && handleFileSelect(e.target.files[0])}
            />
          </label>
        </Paper>
      ) : (
        <Paper sx={{ p: 3 }}>
          <Box sx={{ display: "flex", alignItems: "center", mb: 3 }}>
            <Typography variant="h6" sx={{ flexGrow: 1 }}>
              {file?.name}
            </Typography>
            {invoice.confidence && (
              <Typography variant="body2" color="text.secondary">
                AI-betrouwbaarheid: {Math.round(invoice.confidence * 100)}%
              </Typography>
            )}
          </Box>

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, sm: 6 }}>
              <TextField fullWidth label="Leverancier" value={leverancier} onChange={(e) => setLeverancier(e.target.value)} size="small" />
            </Grid>
            <Grid size={{ xs: 12, sm: 6 }}>
              <RelationPicker value={relatie} onChange={setRelatie} label="Relatie (leverancier)" />
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <TextField fullWidth label="Factuurnummer" value={factuurnummer} onChange={(e) => setFactuurnummer(e.target.value)} size="small" />
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <TextField fullWidth label="Datum" type="date" value={datum} onChange={(e) => setDatum(e.target.value)} size="small" slotProps={{ inputLabel: { shrink: true } }} />
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <LedgerAccountPicker accounts={ledgerAccounts} value={tegenRekening} onChange={setTegenRekening} label="Tegenrekening" />
            </Grid>
            <Grid size={{ xs: 12, sm: 3 }}>
              <TextField fullWidth label="Bedrag excl. BTW" type="number" value={bedragExcl} onChange={(e) => setBedragExcl(parseFloat(e.target.value) || 0)} size="small" />
            </Grid>
            <Grid size={{ xs: 12, sm: 3 }}>
              <TextField fullWidth label="BTW-bedrag" type="number" value={btwBedrag} onChange={(e) => setBtwBedrag(parseFloat(e.target.value) || 0)} size="small" />
            </Grid>
            <Grid size={{ xs: 12, sm: 3 }}>
              <TextField fullWidth label="Bedrag incl. BTW" type="number" value={bedragIncl} onChange={(e) => setBedragIncl(parseFloat(e.target.value) || 0)} size="small" />
            </Grid>
            <Grid size={{ xs: 12, sm: 3 }}>
              <VATCodePicker codes={vatCodes} value={btwCode} onChange={setBtwCode} />
            </Grid>
            <Grid size={12}>
              <TextField fullWidth label="Omschrijving" value={omschrijving} onChange={(e) => setOmschrijving(e.target.value)} size="small" slotProps={{ htmlInput: { maxLength: 200 } }} />
            </Grid>
          </Grid>

          <Box sx={{ display: "flex", justifyContent: "flex-end", mt: 3, gap: 1 }}>
            <Button onClick={reset}>Annuleren</Button>
            <Button variant="contained" onClick={handleSubmit} disabled={submitting}>
              {submitting ? "Boeken..." : "Factuur boeken"}
            </Button>
          </Box>
        </Paper>
      )}
    </Box>
  );
}
