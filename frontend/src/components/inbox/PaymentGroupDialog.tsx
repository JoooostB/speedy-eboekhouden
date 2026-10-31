import { useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  FormControlLabel,
  FormHelperText,
  FormLabel,
  Radio,
  RadioGroup,
  TextField,
  Typography,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import type { InboxClassification, InboxProcessResult, LedgerAccount, OpenPostGridItem, PaymentGroup } from "../../api/types";
import { api } from "../../api/client";
import { errorClass, track } from "../../analytics";
import { formatMoney } from "../../hooks/mailDocumentsModel";
import {
  DIALOG_INTRO,
  DIALOG_TITLES,
  type DialogMode,
  type GroupInvoice,
  accountLabel,
  addableLines,
  buildGroupPayload,
  candidateFitLabel,
  countResults,
  filterAddable,
  formatLineDate,
  groupInvoice,
  includeLabel,
  invoiceFromCandidate,
  invoiceFromOpenPost,
  lineStatus,
  openPostOptionLabel,
  primaryBlocker,
  primaryLabel,
  resultAlert,
  sortOpenPostsForPicker,
  sumLines,
  summaryStatus,
} from "./paymentGroups";

interface Props {
  open: boolean;
  onClose: () => void;
  /** The group from the classify response, or null when the user composes
   *  one by hand (batch bar, "Hoort bij een openstaande factuur"). */
  group: PaymentGroup | null;
  /** The lines to start with: the group's members or the selection. */
  initialLines: InboxClassification[];
  /** Every unprocessed line, for "Betaling toevoegen". */
  allLines: InboxClassification[];
  ledgerAccounts: LedgerAccount[];
  /** Called after every booking attempt with the per-line results and the
   *  invoice; the caller marks the booked lines and shows the snackbar once
   *  everything is booked. */
  onBooked: (results: Map<number, InboxProcessResult>, invoice: GroupInvoice, bookedLines: InboxClassification[]) => void;
}

/**
 * PaymentGroupDialog — books several incoming bank lines as separate
 * factuurbetalingen on one sales invoice. Three ways to get the invoice:
 * fixed (a complete or partial group), a radio group of candidates (an
 * ambiguous group) or a picker over the open sales invoices (manual). The
 * lines can be left out or added; a sticky summary says what stays open and
 * blocks an overpayment. After a partial failure the dialog stays open with
 * a status per line and "Opnieuw proberen" for the failed ones only.
 *
 * Accessibility: fullscreen on a phone; the summary has role="status"; the
 * error alert takes focus (same pattern as BookingConfirmDialog); every
 * checkbox names its line; the candidates are a fieldset of radios.
 */
export function PaymentGroupDialog({ open, onClose, group, initialLines, allLines, ledgerAccounts, onBooked }: Props) {
  const theme = useTheme();
  const isXs = useMediaQuery(theme.breakpoints.down("sm"));

  const [invoice, setInvoice] = useState<GroupInvoice | null>(null);
  const [picking, setPicking] = useState(false);
  const [lines, setLines] = useState<InboxClassification[]>([]);
  const [included, setIncluded] = useState<Set<number>>(new Set());
  const [adding, setAdding] = useState(false);
  const [query, setQuery] = useState("");
  const [openPosts, setOpenPosts] = useState<OpenPostGridItem[] | null>(null);
  const [openPostsError, setOpenPostsError] = useState<string | null>(null);
  const [processing, setProcessing] = useState(false);
  const [results, setResults] = useState<Map<number, InboxProcessResult>>(new Map());
  const [error, setError] = useState<string | null>(null);
  const errorRef = useRef<HTMLDivElement | null>(null);

  const mode: DialogMode = group ? group.state : "manual";

  // Reset on open.
  useEffect(() => {
    if (!open) return;
    const fixed = group ? groupInvoice(group) : null;
    setInvoice(fixed);
    setPicking(!group);
    setLines(initialLines);
    setIncluded(new Set(initialLines.map((l) => l.id)));
    setAdding(false);
    setQuery("");
    setResults(new Map());
    setError(null);
  }, [open, group, initialLines]);

  // The open sales invoices, loaded once the picker is needed.
  useEffect(() => {
    if (!open || !picking || openPosts !== null) return;
    let cancelled = false;
    setOpenPostsError(null);
    api
      .getOpenPosts("Debiteur")
      .then((posts) => {
        if (!cancelled) setOpenPosts(posts);
      })
      .catch((err) => {
        if (!cancelled) setOpenPostsError(err instanceof Error ? err.message : "Openstaande facturen laden mislukt");
      });
    return () => {
      cancelled = true;
    };
  }, [open, picking, openPosts]);

  useEffect(() => {
    if (!open) setOpenPosts(null);
  }, [open]);

  // Focus the alert when an error appears.
  useEffect(() => {
    if (!error || !errorRef.current) return;
    errorRef.current.scrollIntoView({ block: "start", behavior: "smooth" });
    errorRef.current.focus();
  }, [error]);

  const includedLines = useMemo(() => lines.filter((l) => included.has(l.id) && results.get(l.id)?.status !== "ok"), [lines, included, results]);
  const sum = sumLines(includedLines);
  const status = summaryStatus(invoice, sum);
  const retry = results.size > 0 && countResults(results).failed > 0;
  const blocker = primaryBlocker(invoice, includedLines.length, sum);
  const pickerOptions = useMemo(() => (openPosts ? sortOpenPostsForPicker(openPosts, lines) : []), [openPosts, lines]);
  const addable = useMemo(() => filterAddable(addableLines(allLines, new Set(lines.map((l) => l.id)), invoice), query), [allLines, lines, invoice, query]);

  const toggle = (id: number) =>
    setIncluded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const addLine = (line: InboxClassification) => {
    setLines((prev) => [...prev, line]);
    setIncluded((prev) => new Set(prev).add(line.id));
    setAdding(false);
    setQuery("");
  };

  const chooseCandidate = (index: number) => {
    const c = group?.candidates[index];
    if (!c) return;
    setInvoice(invoiceFromCandidate(c));
    // Pre-tick the lines that fit this post; the others stay but unticked.
    setIncluded(new Set(lines.filter((l) => c.lineIds.includes(l.id)).map((l) => l.id)));
  };

  const book = async () => {
    if (!invoice || includedLines.length === 0 || blocker) return;
    setProcessing(true);
    setError(null);
    // includedLines already leaves out what is booked, so a retry books
    // exactly the failed (and any newly added) lines.
    const payload = buildGroupPayload(includedLines, invoice);
    try {
      const res = await api.processInboxBatch(payload);
      const next = new Map(results);
      res.results.forEach((r, i) => next.set(payload[i].id, r));
      setResults(next);
      const { ok, failed } = countResults(next);
      track("Inbox Payment Group", { state: mode, count: payload.length, ok, failed });
      const booked = lines.filter((l) => next.get(l.id)?.status === "ok");
      onBooked(next, invoice, booked);
      const alert = resultAlert(next);
      if (alert) setError(alert);
      else onClose();
    } catch (err) {
      track("Inbox Payment Group", { state: mode, count: payload.length, ok: 0, failed: payload.length, error: errorClass(err) });
      setError(`Koppelen mislukt: ${err instanceof Error ? err.message : "onbekende fout"}. Er is niets geboekt.`);
    } finally {
      setProcessing(false);
    }
  };

  const candidateIndex = group && invoice ? group.candidates.findIndex((c) => c.relatieId === invoice.relatieId && c.factuurnummer === invoice.factuurnummer) : -1;

  // "Andere factuur kiezen": back to the candidates of an ambiguous group,
  // otherwise to the picker over all open sales invoices.
  const changeInvoice = () => {
    if (mode === "ambiguous" && !picking) setInvoice(null);
    else setPicking(true);
  };

  return (
    <Dialog open={open} onClose={() => !processing && onClose()} fullScreen={isXs} maxWidth="md" fullWidth aria-labelledby="payment-group-title">
      <DialogTitle id="payment-group-title" sx={{ fontWeight: 600 }}>
        {DIALOG_TITLES[mode]}
      </DialogTitle>
      <DialogContent dividers sx={{ pb: 0 }}>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {DIALOG_INTRO}
        </Typography>

        {error && (
          <Box ref={errorRef} tabIndex={-1} sx={{ outline: "none", mb: 2 }}>
            <Alert severity={countResults(results).ok > 0 ? "warning" : "error"}>{error}</Alert>
          </Box>
        )}

        {/* The invoice: fixed, chosen among candidates, or picked by hand. */}
        {!picking && group && mode === "ambiguous" && !invoice && (
          <FormControl component="fieldset" sx={{ mb: 2, width: "100%" }}>
            <FormLabel component="legend" sx={{ fontWeight: 600, color: "text.primary", mb: 0.5 }}>
              Welke factuur betalen deze ontvangsten?
            </FormLabel>
            <RadioGroup value={candidateIndex} onChange={(e) => chooseCandidate(Number(e.target.value))}>
              {group.candidates.map((c, i) => (
                <FormControlLabel
                  key={`${c.relatieId}-${c.factuurnummer}`}
                  value={i}
                  control={<Radio size="small" />}
                  label={
                    <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
                      <Typography variant="body2">
                        {c.relatieNaam} · factuur {c.factuurnummer} · {formatLineDate(c.factuurdatum)} · {formatMoney(c.teOntvangen)} open
                      </Typography>
                      <Typography variant="caption" sx={{ fontWeight: 600, color: candidateFitLabel(c, lines) === "Past precies" ? "#0f766e" : "#92400e" }}>
                        {candidateFitLabel(c, lines)}
                      </Typography>
                    </Box>
                  }
                />
              ))}
            </RadioGroup>
            <Button size="small" variant="text" onClick={() => setPicking(true)} sx={{ alignSelf: "flex-start", mt: 0.5 }}>
              Andere factuur kiezen
            </Button>
          </FormControl>
        )}

        {picking && (
          <Box sx={{ mb: 2 }}>
            {openPostsError ? (
              <Alert severity="error">{openPostsError}</Alert>
            ) : openPosts === null ? (
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }} role="status">
                <CircularProgress size={16} aria-hidden="true" />
                <Typography variant="body2" color="text.secondary">
                  Openstaande verkoopfacturen ophalen…
                </Typography>
              </Box>
            ) : pickerOptions.length === 0 ? (
              <Alert severity="info">Geen openstaande verkoopfacturen gevonden in e-Boekhouden.</Alert>
            ) : (
              <Autocomplete
                options={pickerOptions}
                getOptionLabel={openPostOptionLabel}
                isOptionEqualToValue={(a, b) => a.mutNr === b.mutNr}
                value={pickerOptions.find((p) => invoice && p.relatieId === invoice.relatieId && p.mutFactuur === invoice.factuurnummer) ?? null}
                onChange={(_, p) => {
                  setInvoice(p ? invoiceFromOpenPost(p) : null);
                  if (p) setPicking(false);
                }}
                renderInput={(params) => <TextField {...params} label="Openstaande verkoopfactuur" size="small" autoFocus />}
                noOptionsText="Geen factuur gevonden"
              />
            )}
          </Box>
        )}

        {invoice && !picking && (
          <Box
            component="dl"
            sx={{
              m: 0,
              mb: 2,
              p: 1.5,
              borderRadius: 2,
              bgcolor: "grey.50",
              display: "grid",
              gridTemplateColumns: { xs: "1fr", sm: "repeat(3, 1fr)" },
              gap: 1,
              "& dt": { fontSize: "0.75rem", color: "text.secondary" },
              "& dd": { m: 0, fontWeight: 600, fontSize: "0.875rem" },
            }}
          >
            <Box>
              <dt>Relatie</dt>
              <dd>{invoice.relatieNaam || "Onbekende relatie"}</dd>
            </Box>
            <Box>
              <dt>Factuur</dt>
              <dd>{invoice.factuurnummer}</dd>
            </Box>
            <Box>
              <dt>Factuurdatum</dt>
              <dd>{formatLineDate(invoice.factuurdatum)}</dd>
            </Box>
            <Box>
              <dt>Factuurbedrag</dt>
              <dd>{formatMoney(invoice.sBedrag)}</dd>
            </Box>
            <Box>
              <dt>Al betaald</dt>
              <dd>{formatMoney(invoice.sBetaald)}</dd>
            </Box>
            <Box>
              <dt>Nog open</dt>
              <dd>{formatMoney(invoice.teOntvangen)}</dd>
            </Box>
            <Box sx={{ gridColumn: "1 / -1" }}>
              <Button size="small" variant="text" onClick={changeInvoice} disabled={processing}>
                Andere factuur kiezen
              </Button>
            </Box>
          </Box>
        )}

        {/* The lines */}
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          Betalingen
        </Typography>
        <Box component="ul" sx={{ m: 0, p: 0, mb: 1 }} aria-label="Betalingen in deze groep">
          {lines.map((l) => {
            const account = accountLabel(l, ledgerAccounts);
            const result = results.get(l.id);
            const booked = result?.status === "ok";
            return (
              <Box
                component="li"
                key={l.id}
                sx={{
                  listStyle: "none",
                  display: "grid",
                  gridTemplateColumns: { xs: "auto 1fr", sm: "auto 110px 110px 170px 1fr" },
                  columnGap: 1.5,
                  rowGap: 0.25,
                  alignItems: "center",
                  py: 0.75,
                  borderBottom: "1px solid",
                  borderColor: "divider",
                  opacity: booked ? 0.7 : 1,
                }}
              >
                <Checkbox
                  size="small"
                  checked={included.has(l.id)}
                  onChange={() => toggle(l.id)}
                  disabled={processing || booked}
                  inputProps={{ "aria-label": includeLabel(l, account) }}
                  sx={{ p: 0.5 }}
                />
                <Typography variant="body2" color="text.secondary">
                  {formatLineDate(l.datum)}
                </Typography>
                <Typography variant="body2" sx={{ fontWeight: 600, fontVariantNumeric: "tabular-nums", textAlign: { sm: "right" }, gridColumn: { xs: "2", sm: "auto" } }}>
                  {formatMoney(l.bedrag)}
                </Typography>
                <Typography variant="caption" color="text.secondary" sx={{ gridColumn: { xs: "2", sm: "auto" } }} noWrap>
                  {account || "Eigen rekening onbekend"}
                </Typography>
                <Box sx={{ minWidth: 0, gridColumn: { xs: "2", sm: "auto" } }}>
                  <Typography variant="caption" color="text.secondary" noWrap sx={{ display: "block" }}>
                    {l.omschrijving}
                  </Typography>
                  {result && (
                    <Typography variant="caption" sx={{ fontWeight: 600, color: booked ? "success.dark" : "error.dark", display: "block" }}>
                      {lineStatus(result)}
                    </Typography>
                  )}
                </Box>
              </Box>
            );
          })}
        </Box>

        {adding ? (
          <Box sx={{ mb: 2 }}>
            <TextField
              label="Zoek op bedrag of omschrijving"
              size="small"
              fullWidth
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              sx={{ mb: 1 }}
            />
            {addable.length === 0 ? (
              <Typography variant="body2" color="text.secondary">
                Er zijn geen andere ontvangsten om toe te voegen.
              </Typography>
            ) : (
              <Box component="ul" sx={{ m: 0, p: 0, maxHeight: 200, overflowY: "auto" }} aria-label="Ontvangsten om toe te voegen">
                {addable.map((l) => (
                  <Box component="li" key={l.id} sx={{ listStyle: "none" }}>
                    <Button
                      variant="text"
                      size="small"
                      onClick={() => addLine(l)}
                      sx={{ justifyContent: "flex-start", textAlign: "left", width: "100%", textTransform: "none", fontWeight: 400 }}
                    >
                      {formatLineDate(l.datum)} · {formatMoney(l.bedrag)} · {l.omschrijving}
                    </Button>
                  </Box>
                ))}
              </Box>
            )}
            <Button size="small" variant="text" onClick={() => setAdding(false)} sx={{ mt: 0.5 }}>
              Annuleren
            </Button>
          </Box>
        ) : (
          <Button size="small" variant="text" onClick={() => setAdding(true)} disabled={processing} sx={{ mb: 2 }}>
            Betaling toevoegen
          </Button>
        )}

        {/* Sticky summary */}
        <Box
          role="status"
          sx={{
            position: "sticky",
            bottom: 0,
            py: 1.5,
            mx: -3,
            px: 3,
            bgcolor: "background.paper",
            borderTop: "1px solid",
            borderColor: "divider",
          }}
        >
          <Typography variant="body2" sx={{ fontWeight: 600, color: status.blocking ? "error.dark" : "text.primary" }}>
            {status.text}
          </Typography>
        </Box>
      </DialogContent>
      <DialogActions sx={{ px: 3, py: 2, flexWrap: "wrap", gap: 1 }}>
        {blocker && !status.blocking && (
          <FormHelperText sx={{ flex: "1 1 100%", m: 0 }}>{blocker}</FormHelperText>
        )}
        <Button onClick={onClose} disabled={processing}>
          Sluiten
        </Button>
        <Button variant="contained" onClick={book} disabled={processing || !!blocker} sx={{ fontWeight: 600, minWidth: 180 }}>
          {processing ? <CircularProgress size={18} color="inherit" aria-label="Wordt geboekt" /> : primaryLabel(includedLines.length, retry)}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
