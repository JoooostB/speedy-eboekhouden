import { useState, useCallback, useRef, useEffect } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  Step,
  StepLabel,
  Stepper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from "@mui/material";
import { api } from "../../api/client";
import { track } from "../../analytics";
import type { Relation } from "../../api/types";

// ─── Types ───────────────────────────────────────────────────────────────────

interface KvKResult {
  kvkNummer: string;
  bedrijf: string;
  plaats: string;
  adres: string;
  vestigingsnummer: string;
}

interface RelationFormData {
  code: string;
  bedrijf: string;
  kvk: string;
  adres: string;
  postcode: string;
  plaats: string;
  land: string;
  telefoon: string;
  iban: string;
  btwNummer: string;
}

interface Props {
  open: boolean;
  onClose: () => void;
  /** Called with the newly created relation so the parent can select it */
  onCreated: (relation: Relation) => void;
  /** The grootboekrekening ID for crediteuren — passed through to the API */
  grootboekrekeningId?: number;
  /** Pre-fill the KvK search with this company name */
  initialSearch?: string;
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

/**
 * Generate a relation code from a company name.
 * Takes the first word, removes non-alphanumeric chars, uppercases, max 10 chars.
 */
function generateCode(bedrijf: string): string {
  const first = bedrijf.trim().split(/\s+/)[0] ?? "";
  return first.replace(/[^a-zA-Z0-9]/g, "").toUpperCase().slice(0, 10);
}

const EMPTY_FORM: RelationFormData = {
  code: "",
  bedrijf: "",
  kvk: "",
  adres: "",
  postcode: "",
  plaats: "",
  land: "Nederland",
  telefoon: "",
  iban: "",
  btwNummer: "",
};

const STEPS = ["KvK zoeken", "Gegevens invullen"];

// ─── Component ───────────────────────────────────────────────────────────────

/**
 * CreateRelationDialog — two-step dialog to create a new relation (crediteur)
 * in e-boekhouden, optionally pre-filling from KvK (Kamer van Koophandel) data.
 *
 * Step 1: Search KvK by name or number, pick a result.
 * Step 2: Review/edit the pre-filled fields, then submit.
 *
 * Accessibility:
 * - Dialog uses aria-labelledby for the title
 * - Stepper provides visual and programmatic step indication
 * - Search results table uses proper <th> scope="col" via MUI TableHead
 * - Clickable rows have role="button" and keyboard support (Enter/Space)
 * - Loading and error states are announced via aria-live regions
 * - All form inputs have visible labels via MUI TextField
 * - Focus is managed: search input is auto-focused on open
 */
export function CreateRelationDialog({
  open,
  onClose,
  onCreated,
  grootboekrekeningId = 0,
  initialSearch = "",
}: Props) {
  const [activeStep, setActiveStep] = useState(0);

  // Step 1: KvK search state
  const [searchQuery, setSearchQuery] = useState(initialSearch);
  const [searchResults, setSearchResults] = useState<KvKResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState("");

  // Step 2: Form state
  const [form, setForm] = useState<RelationFormData>(EMPTY_FORM);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  const [fetchingAddress, setFetchingAddress] = useState(false);

  // Ref for focusing the search input on open
  const searchInputRef = useRef<HTMLInputElement>(null);

  // Debounce timer ref
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Reset state when dialog opens/closes
  useEffect(() => {
    if (open) {
      setActiveStep(0);
      setSearchQuery(initialSearch);
      setSearchResults([]);
      setSearchError("");
      setForm(EMPTY_FORM);
      setSubmitError("");
      setSubmitting(false);
      setFetchingAddress(false);
      // Auto-search if we have an initial query
      if (initialSearch.length >= 2) {
        performSearch(initialSearch);
      }
    }
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initialSearch]);

  // Focus search input when dialog opens
  useEffect(() => {
    if (open && activeStep === 0) {
      // Small delay to let MUI dialog animation complete
      const timer = setTimeout(() => searchInputRef.current?.focus(), 100);
      return () => clearTimeout(timer);
    }
  }, [open, activeStep]);

  /** Execute KvK search */
  const performSearch = useCallback(async (query: string) => {
    if (query.length < 2) return;
    setSearching(true);
    setSearchError("");
    try {
      const results = await api.searchKvK(query);
      setSearchResults(results);
      if (results.length === 0) {
        setSearchError("Geen resultaten gevonden.");
      }
    } catch (err: any) {
      setSearchError(err.message || "Zoeken mislukt.");
      setSearchResults([]);
    } finally {
      setSearching(false);
    }
  }, []);

  /** Handle search input changes with debounce */
  const handleSearchChange = useCallback(
    (value: string) => {
      setSearchQuery(value);
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (value.length >= 2) {
        debounceRef.current = setTimeout(() => performSearch(value), 400);
      } else {
        setSearchResults([]);
        setSearchError("");
      }
    },
    [performSearch],
  );

  /** Select a KvK result and fetch full address */
  const handleSelectResult = useCallback(
    async (result: KvKResult) => {
      const newForm: RelationFormData = {
        ...EMPTY_FORM,
        bedrijf: result.bedrijf,
        kvk: result.kvkNummer,
        adres: result.adres,
        plaats: result.plaats,
        code: generateCode(result.bedrijf),
      };

      setForm(newForm);
      setActiveStep(1);

      // Fetch detailed address from vestigingsnummer
      if (result.vestigingsnummer) {
        setFetchingAddress(true);
        try {
          const addr = await api.getKvKAddress(result.vestigingsnummer);
          setForm((prev) => ({
            ...prev,
            adres: addr.volledigAdres || `${addr.straatnaam} ${addr.huisnummer}${addr.huisletter || ""}`,
            postcode: addr.postcode || prev.postcode,
            plaats: addr.plaats || prev.plaats,
            land: addr.land || prev.land,
          }));
        } catch {
          // Address fetch failed — keep what we have from the search result
        } finally {
          setFetchingAddress(false);
        }
      }
    },
    [],
  );

  /** Skip KvK search and go straight to manual entry */
  const handleSkipToManual = useCallback(() => {
    setForm(EMPTY_FORM);
    setActiveStep(1);
  }, []);

  /** Go back to step 1 */
  const handleBack = useCallback(() => {
    setActiveStep(0);
    setSubmitError("");
  }, []);

  /** Update a form field */
  const updateForm = useCallback(<K extends keyof RelationFormData>(field: K, value: RelationFormData[K]) => {
    setForm((prev) => ({ ...prev, [field]: value }));
  }, []);

  /** Submit the new relation */
  const handleSubmit = useCallback(async () => {
    // Validation
    if (!form.code.trim()) {
      setSubmitError("Code is verplicht.");
      return;
    }
    if (!form.bedrijf.trim()) {
      setSubmitError("Bedrijfsnaam is verplicht.");
      return;
    }

    setSubmitting(true);
    setSubmitError("");

    try {
      const result = await api.createRelation({
        relatie: {
          bp: "B", // B = crediteur/leverancier
          code: form.code.trim(),
          bedrijf: form.bedrijf.trim(),
          kvk: form.kvk.trim(),
          adres: form.adres.trim(),
          postcode: form.postcode.trim(),
          plaats: form.plaats.trim(),
          land: form.land.trim(),
          telefoon: form.telefoon.trim(),
          iban: form.iban.trim(),
          btwNummer: form.btwNummer.trim(),
          grootboekrekeningId,
        },
      });

      track("Relation Created", {
        hasKvk: form.kvk ? "true" : "false",
      });

      // Build a Relation object the parent can use
      const newRelation: Relation = {
        id: result.id,
        code: result.code || form.code.trim(),
        bedrijf: result.bedrijf || form.bedrijf.trim(),
        grootboekrekeningId,
        iban: form.iban.trim(),
      };

      onCreated(newRelation);
      onClose();
    } catch (err: any) {
      setSubmitError(err.message || "Aanmaken mislukt.");
    } finally {
      setSubmitting(false);
    }
  }, [form, grootboekrekeningId, onCreated, onClose]);

  const canSubmit = form.code.trim().length > 0 && form.bedrijf.trim().length > 0 && !submitting;

  return (
    <Dialog
      open={open}
      onClose={submitting ? undefined : onClose}
      fullWidth
      maxWidth="md"
      aria-labelledby="create-relation-dialog-title"
      disableEscapeKeyDown={submitting}
    >
      <DialogTitle id="create-relation-dialog-title" sx={{ pb: 1 }}>
        <Typography variant="h6" component="span">
          Nieuwe relatie aanmaken
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          Dit maakt een crediteur/leverancier aan in e-boekhouden.
        </Typography>
      </DialogTitle>

      <DialogContent dividers sx={{ p: { xs: 2, sm: 3 } }}>
        {/* Stepper — provides both visual and programmatic step indication */}
        <Stepper activeStep={activeStep} sx={{ mb: 3 }} aria-label="Stappen voor het aanmaken van een relatie">
          {STEPS.map((label) => (
            <Step key={label}>
              <StepLabel>{label}</StepLabel>
            </Step>
          ))}
        </Stepper>

        {/* Step 1: KvK search */}
        {activeStep === 0 && (
          <Box>
            <Box
              component="form"
              onSubmit={(e: React.FormEvent) => {
                e.preventDefault();
                performSearch(searchQuery);
              }}
              sx={{ display: "flex", gap: 1, mb: 2 }}
            >
              <TextField
                inputRef={searchInputRef}
                label="Zoek op bedrijfsnaam of KvK-nummer"
                value={searchQuery}
                onChange={(e) => handleSearchChange(e.target.value)}
                size="small"
                fullWidth
                autoComplete="off"
                slotProps={{
                  input: {
                    "aria-describedby": "kvk-search-hint",
                  },
                }}
              />
              <Button
                type="submit"
                variant="contained"
                disabled={searching || searchQuery.length < 2}
                sx={{ whiteSpace: "nowrap", minWidth: "auto", px: 2 }}
                startIcon={
                  searching ? (
                    <CircularProgress size={16} color="inherit" aria-hidden="true" />
                  ) : (
                    /* Search icon — inline SVG */
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
                      <circle cx="11" cy="11" r="8" />
                      <line x1="21" y1="21" x2="16.65" y2="16.65" />
                    </Box>
                  )
                }
              >
                Zoeken
              </Button>
            </Box>

            <Typography id="kvk-search-hint" variant="caption" color="text.secondary" sx={{ mb: 2, display: "block" }}>
              Zoek in het KvK-register om gegevens automatisch in te vullen.
            </Typography>

            {/* Error / empty state — announced to screen readers */}
            {searchError && (
              <Alert severity="info" sx={{ mb: 2 }} role="status" aria-live="polite">
                {searchError}
              </Alert>
            )}

            {/* Results table */}
            {searchResults.length > 0 && (
              <TableContainer sx={{ maxHeight: 320 }}>
                <Table size="small" aria-label="KvK-zoekresultaten">
                  <TableHead>
                    <TableRow>
                      <TableCell>Bedrijf</TableCell>
                      <TableCell>Plaats</TableCell>
                      <TableCell>KvK-nummer</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {searchResults.map((r) => (
                      <TableRow
                        key={r.vestigingsnummer || r.kvkNummer}
                        hover
                        onClick={() => handleSelectResult(r)}
                        onKeyDown={(e: React.KeyboardEvent) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            handleSelectResult(r);
                          }
                        }}
                        role="button"
                        tabIndex={0}
                        aria-label={`${r.bedrijf}, ${r.plaats}, KvK ${r.kvkNummer} — klik om te selecteren`}
                        sx={{ cursor: "pointer" }}
                      >
                        <TableCell>
                          <Typography variant="body2" fontWeight={600}>
                            {r.bedrijf}
                          </Typography>
                          <Typography variant="caption" color="text.secondary">
                            {r.adres}
                          </Typography>
                        </TableCell>
                        <TableCell>{r.plaats}</TableCell>
                        <TableCell sx={{ fontFamily: "monospace" }}>{r.kvkNummer}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            )}

            <Divider sx={{ my: 2 }} />

            {/* Skip to manual entry */}
            <Button
              variant="text"
              onClick={handleSkipToManual}
              sx={{ textTransform: "none" }}
            >
              Overslaan en handmatig invullen
            </Button>
          </Box>
        )}

        {/* Step 2: Form fields */}
        {activeStep === 1 && (
          <Box>
            {fetchingAddress && (
              <Alert severity="info" sx={{ mb: 2 }} role="status" aria-live="polite">
                <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                  <CircularProgress size={14} aria-hidden="true" />
                  Adresgegevens ophalen...
                </Box>
              </Alert>
            )}

            {submitError && (
              <Alert severity="error" sx={{ mb: 2 }} role="alert" aria-live="assertive">
                {submitError}
              </Alert>
            )}

            {/* Row 1: Code + Bedrijf */}
            <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "200px 1fr" }, gap: 2 }}>
              <TextField
                label="Code"
                value={form.code}
                onChange={(e) => updateForm("code", e.target.value.toUpperCase().slice(0, 10))}
                size="small"
                fullWidth
                required
                disabled={submitting}
                helperText="Max. 10 tekens, hoofdletters"
                slotProps={{
                  htmlInput: { maxLength: 10, style: { textTransform: "uppercase", fontFamily: "monospace" } },
                }}
              />
              <TextField
                label="Bedrijfsnaam"
                value={form.bedrijf}
                onChange={(e) => updateForm("bedrijf", e.target.value)}
                size="small"
                fullWidth
                required
                disabled={submitting}
              />
            </Box>

            {/* Row 2: KvK-nummer */}
            <Box sx={{ mt: 2 }}>
              <TextField
                label="KvK-nummer"
                value={form.kvk}
                onChange={(e) => updateForm("kvk", e.target.value)}
                size="small"
                fullWidth
                disabled={submitting}
                slotProps={{
                  htmlInput: { style: { fontFamily: "monospace" } },
                }}
              />
            </Box>

            {/* Row 3: Adres + Postcode + Plaats */}
            <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "2fr 1fr 1fr" }, gap: 2, mt: 2 }}>
              <TextField
                label="Adres"
                value={form.adres}
                onChange={(e) => updateForm("adres", e.target.value)}
                size="small"
                fullWidth
                disabled={submitting}
              />
              <TextField
                label="Postcode"
                value={form.postcode}
                onChange={(e) => updateForm("postcode", e.target.value)}
                size="small"
                fullWidth
                disabled={submitting}
              />
              <TextField
                label="Plaats"
                value={form.plaats}
                onChange={(e) => updateForm("plaats", e.target.value)}
                size="small"
                fullWidth
                disabled={submitting}
              />
            </Box>

            {/* Row 4: Land */}
            <Box sx={{ mt: 2 }}>
              <TextField
                label="Land"
                value={form.land}
                onChange={(e) => updateForm("land", e.target.value)}
                size="small"
                fullWidth
                disabled={submitting}
              />
            </Box>

            <Divider sx={{ my: 2 }} />

            {/* Optional fields */}
            <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1.5 }}>
              Optioneel
            </Typography>

            <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr 1fr" }, gap: 2 }}>
              <TextField
                label="Telefoon"
                value={form.telefoon}
                onChange={(e) => updateForm("telefoon", e.target.value)}
                size="small"
                fullWidth
                disabled={submitting}
                autoComplete="tel"
              />
              <TextField
                label="IBAN"
                value={form.iban}
                onChange={(e) => updateForm("iban", e.target.value.toUpperCase())}
                size="small"
                fullWidth
                disabled={submitting}
                slotProps={{
                  htmlInput: { style: { fontFamily: "monospace", textTransform: "uppercase" } },
                }}
              />
              <TextField
                label="BTW-nummer"
                value={form.btwNummer}
                onChange={(e) => updateForm("btwNummer", e.target.value.toUpperCase())}
                size="small"
                fullWidth
                disabled={submitting}
                slotProps={{
                  htmlInput: { style: { fontFamily: "monospace", textTransform: "uppercase" } },
                }}
              />
            </Box>
          </Box>
        )}
      </DialogContent>

      <DialogActions sx={{ px: 3, py: 2 }}>
        {activeStep === 1 && (
          <Button onClick={handleBack} disabled={submitting} sx={{ mr: "auto" }}>
            Terug
          </Button>
        )}
        <Button onClick={onClose} disabled={submitting}>
          Annuleren
        </Button>
        {activeStep === 1 && (
          <Button
            variant="contained"
            onClick={handleSubmit}
            disabled={!canSubmit}
            startIcon={
              submitting ? (
                <CircularProgress size={16} color="inherit" aria-hidden="true" />
              ) : (
                /* Plus icon — inline SVG */
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
                  <line x1="12" y1="5" x2="12" y2="19" />
                  <line x1="5" y1="12" x2="19" y2="12" />
                </Box>
              )
            }
          >
            {submitting ? "Aanmaken..." : "Aanmaken"}
          </Button>
        )}
      </DialogActions>
    </Dialog>
  );
}
