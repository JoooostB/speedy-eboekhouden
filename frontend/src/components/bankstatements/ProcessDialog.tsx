import { useEffect, useState } from "react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  MenuItem,
  Box,
  Typography,
  Alert,
  CircularProgress,
  Grid,
  Chip,
} from "@mui/material";
import { api } from "../../api/client";
import type { BankStatementRow, LedgerAccount, OpenPost, Relation, MutatieSoort } from "../../api/types";
import { MUTATIE_SOORT_CODES } from "../../api/types";
import { LedgerAccountPicker } from "../shared/LedgerAccountPicker";
import { RelationPicker } from "../shared/RelationPicker";
import { VATCodePicker } from "../shared/VATCodePicker";
import { useLedgerAccounts } from "../../hooks/useLedgerAccounts";
import { useVATCodes } from "../../hooks/useVATCodes";
import { track } from "../../analytics";

interface Props {
  row: BankStatementRow;
  onClose: () => void;
  onProcessed: (id: number) => void;
}

/** Inline SVG: magic wand icon (replaces @mui/icons-material AutoFixHigh) */
function MagicWandIcon(props: { sx?: object }) {
  return (
    <Box
      component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" sx={{ width: 20, height: 20, ...props.sx }}
    >
      <path d="m15 4-7.68 7.68a2 2 0 0 0 0 2.83l1.17 1.17a2 2 0 0 0 2.83 0L19 8" />
      <path d="m15 4 2-2 4 4-2 2" />
      <path d="m2 2 2.5 2.5" />
      <path d="m7 1 0 3" />
      <path d="m1 7 3 0" />
    </Box>
  );
}

export function ProcessDialog({ row, onClose, onProcessed }: Props) {
  const { data: ledgerAccounts } = useLedgerAccounts();
  const { data: vatCodes } = useVATCodes();

  const [soort, setSoort] = useState<MutatieSoort>(row.mutBedrag < 0 ? "GeldUitgegeven" : "GeldOntvangen");
  const [tegenRekening, setTegenRekening] = useState<LedgerAccount | null>(null);
  const [relatie, setRelatie] = useState<Relation | null>(null);
  const [btwCode, setBtwCode] = useState("GEEN");
  const [omschrijving, setOmschrijving] = useState(row.mutOmschrijving.slice(0, 200));
  const [factuurnummer, setFactuurnummer] = useState(row.mutFactuur || "");
  const [bedrag, setBedrag] = useState(Math.abs(row.mutBedrag));
  const [btw, setBtw] = useState(0);

  const [aiLoading, setAiLoading] = useState(false);
  const [aiResult, setAiResult] = useState<{ grootboekcode: string; btwCode: string; soort: string; confidence: number } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  // Open items matching
  const [matchingItems, setMatchingItems] = useState<OpenPost[]>([]);
  const [matchApplied, setMatchApplied] = useState(false);

  // Try AI classification
  const classifyWithAI = async () => {
    setAiLoading(true);
    try {
      const result = await api.classifyTransaction({
        omschrijving: row.mutOmschrijving,
        bedrag: row.mutBedrag,
        datum: row.mutDatum?.split("T")[0] || "",
      });
      setAiResult(result);
      track("AI Classify", { confidence: String(Math.round(result.confidence * 100)) });

      // Auto-fill from AI suggestion
      if (result.btwCode) setBtwCode(result.btwCode);
      if (result.soort) setSoort(result.soort as MutatieSoort);
      if (result.grootboekcode) {
        const match = ledgerAccounts.find((a) => a.code === result.grootboekcode);
        if (match) setTegenRekening(match);
      }
    } catch {
      // AI not available
    } finally {
      setAiLoading(false);
    }
  };

  useEffect(() => {
    if (ledgerAccounts.length > 0) {
      classifyWithAI();
    }
  }, [ledgerAccounts.length]);

  // Try to match against open items when SOAP credentials are available
  useEffect(() => {
    const amount = Math.abs(row.mutBedrag);
    const soortPosten = row.mutBedrag > 0 ? "Debiteuren" : "Crediteuren";

    api.getOpenPosten(soortPosten as "Debiteuren" | "Crediteuren")
      .then((items) => {
        // Match on amount (within 1 cent tolerance for rounding)
        const matches = (items || []).filter(
          (item) => Math.abs(Math.abs(item.openstaand) - amount) < 0.02,
        );
        setMatchingItems(matches);
      })
      .catch(() => {
        // SOAP not configured or error — silently skip matching
      });
  }, [row.mutBedrag]);

  const handleSubmit = async () => {
    setError("");
    setSubmitting(true);
    try {
      const payload = {
        mutatie: {
          rekening: row.grootboekId,
          datum: row.mutDatum?.split("T")[0] || "",
          soort: MUTATIE_SOORT_CODES[soort],
          omschrijving: omschrijving.slice(0, 200),
        },
        mutatieRegels: [{
          index: 0,
          bedrag,
          btw,
          btwCode,
          ...(tegenRekening ? { tegenRekening: tegenRekening.id } : {}),
          ...(relatie ? { relatieId: relatie.id } : {}),
          ...(factuurnummer ? { factuur: factuurnummer } : {}),
        }],
        importId: row.id,
      };

      await api.processBankStatement(row.id, payload);
      track("Bank Statement Processed", { soort, aiUsed: aiResult ? "true" : "false" });
      onProcessed(row.id);
    } catch (err: any) {
      track("Bank Statement Error", { reason: err.message || "unknown" });
      setError(err.message || "Verwerking mislukt");
    } finally {
      setSubmitting(false);
    }
  };

  const formatAmount = (amount: number) =>
    amount >= 0 ? `+\u20AC${amount.toFixed(2)}` : `-\u20AC${Math.abs(amount).toFixed(2)}`;

  return (
    <Dialog open onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>
        Afschriftregel verwerken
      </DialogTitle>
      <DialogContent>
        <Box sx={{ mb: 3, p: 2, bgcolor: "grey.50", borderRadius: 1 }}>
          <Typography variant="body2" color="text.secondary">Omschrijving</Typography>
          <Typography variant="body1" gutterBottom>{row.mutOmschrijving}</Typography>
          <Typography variant="h5" sx={{ color: row.mutBedrag >= 0 ? "success.main" : "error.main" }}>
            {formatAmount(row.mutBedrag)}
          </Typography>
        </Box>

        {aiResult && (
          <Alert
            severity="info"
            icon={<MagicWandIcon />}
            sx={{ mb: 2 }}
            role="status"
            aria-live="polite"
          >
            AI-suggestie toegepast (betrouwbaarheid: {Math.round(aiResult.confidence * 100)}%). Controleer de ingevulde velden.
          </Alert>
        )}

        {/* Open items matches */}
        {matchingItems.length > 0 && !matchApplied && (
          <Alert
            severity="info"
            sx={{ mb: 2 }}
            role="status"
            aria-live="polite"
          >
            <Typography variant="body2" fontWeight={600} gutterBottom>
              Mogelijke {matchingItems.length === 1 ? "match" : "matches"} gevonden:
            </Typography>
            {matchingItems.map((item, idx) => (
              <Box key={idx} sx={{ display: "flex", alignItems: "center", gap: 1, mt: 0.5 }}>
                <Typography variant="body2">
                  Factuur {item.factuurnummer} van {item.relatie} ({new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" }).format(item.openstaand)})
                </Typography>
                <Chip
                  label="Toepassen"
                  size="small"
                  color="primary"
                  onClick={() => {
                    setFactuurnummer(item.factuurnummer);
                    // Set soort based on direction
                    if (row.mutBedrag > 0) {
                      setSoort("FactuurbetalingOntvangen");
                    } else {
                      setSoort("FactuurbetalingVerstuurd");
                    }
                    setMatchApplied(true);
                    track("Open Item Match Applied");
                  }}
                  aria-label={`Match toepassen: factuur ${item.factuurnummer} van ${item.relatie}`}
                  sx={{ cursor: "pointer" }}
                />
              </Box>
            ))}
          </Alert>
        )}

        {matchApplied && (
          <Alert severity="success" sx={{ mb: 2 }} role="status">
            Openstaande post is gekoppeld. Controleer de ingevulde velden.
          </Alert>
        )}

        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

        <Grid container spacing={2}>
          <Grid size={{ xs: 12, sm: 6 }}>
            <TextField
              select
              fullWidth
              label="Type boeking"
              value={soort}
              onChange={(e) => setSoort(e.target.value as MutatieSoort)}
              size="small"
            >
              <MenuItem value="GeldUitgegeven">Geld uitgegeven</MenuItem>
              <MenuItem value="GeldOntvangen">Geld ontvangen</MenuItem>
              <MenuItem value="FactuurbetalingVerstuurd">Factuurbetaling verstuurd</MenuItem>
              <MenuItem value="FactuurbetalingOntvangen">Factuurbetaling ontvangen</MenuItem>
              <MenuItem value="FactuurOntvangen">Factuur ontvangen</MenuItem>
              <MenuItem value="FactuurVerstuurd">Factuur verstuurd</MenuItem>
              <MenuItem value="Memoriaal">Memoriaal</MenuItem>
            </TextField>
          </Grid>

          <Grid size={{ xs: 12, sm: 6 }}>
            <LedgerAccountPicker
              accounts={ledgerAccounts}
              value={tegenRekening}
              onChange={setTegenRekening}
              label="Tegenrekening"
            />
          </Grid>

          <Grid size={{ xs: 12, sm: 6 }}>
            <RelationPicker
              value={relatie}
              onChange={setRelatie}
            />
          </Grid>

          <Grid size={{ xs: 12, sm: 6 }}>
            <VATCodePicker
              codes={vatCodes}
              value={btwCode}
              onChange={setBtwCode}
            />
          </Grid>

          <Grid size={{ xs: 12, sm: 6 }}>
            <TextField
              fullWidth
              label="Bedrag"
              type="number"
              value={bedrag}
              onChange={(e) => setBedrag(parseFloat(e.target.value) || 0)}
              size="small"
              slotProps={{ input: { startAdornment: <Typography sx={{ mr: 1 }}>&euro;</Typography> } }}
            />
          </Grid>

          <Grid size={{ xs: 12, sm: 6 }}>
            <TextField
              fullWidth
              label="BTW-bedrag"
              type="number"
              value={btw}
              onChange={(e) => setBtw(parseFloat(e.target.value) || 0)}
              size="small"
              slotProps={{ input: { startAdornment: <Typography sx={{ mr: 1 }}>&euro;</Typography> } }}
            />
          </Grid>

          <Grid size={{ xs: 12, sm: 6 }}>
            <TextField
              fullWidth
              label="Factuurnummer"
              value={factuurnummer}
              onChange={(e) => setFactuurnummer(e.target.value)}
              size="small"
            />
          </Grid>

          <Grid size={12}>
            <TextField
              fullWidth
              label="Omschrijving"
              value={omschrijving}
              onChange={(e) => setOmschrijving(e.target.value)}
              size="small"
              slotProps={{ htmlInput: { maxLength: 200 } }}
            />
          </Grid>
        </Grid>
      </DialogContent>
      <DialogActions>
        {aiLoading && <CircularProgress size={20} sx={{ mr: 1 }} />}
        <Button
          startIcon={<MagicWandIcon />}
          onClick={classifyWithAI}
          disabled={aiLoading}
          size="small"
        >
          AI-suggestie
        </Button>
        <Box sx={{ flexGrow: 1 }} />
        <Button onClick={onClose}>Annuleren</Button>
        <Button
          variant="contained"
          onClick={handleSubmit}
          disabled={submitting || !tegenRekening}
        >
          {submitting ? "Verwerken..." : "Verwerken"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
