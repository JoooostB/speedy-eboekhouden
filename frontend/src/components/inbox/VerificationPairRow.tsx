import { useState } from "react";
import { Box, Button, Checkbox, Chip, Collapse, IconButton, Tooltip, Typography } from "@mui/material";
import type { InboxClassification, LedgerAccount } from "../../api/types";
import { formatMoney, formatShortDate } from "../../hooks/mailDocumentsModel";
import { VERIFICATION_CHIP_LABELS, VERIFICATION_CHIP_SX, VERIFICATION_CHIP_TOOLTIPS, amountAriaLabel, ledgerLabel } from "./inboxBooking";
import { TruncatedText } from "./TruncatedText";
import { type VerificationPair, boekenPairLabel, pairButtonLabel, pairSummaryLine, pairTotal } from "./verificationPairs";

interface Props {
  pair: VerificationPair;
  /** The pair's lines that are still in the list. */
  lines: InboxClassification[];
  /** True when both lines are selected for the batch bar. */
  checked: boolean;
  /** Selects or deselects both lines at once. */
  onToggle: (ids: number[], select: boolean) => void;
  ledgerAccounts: LedgerAccount[];
  /** Opens the confirm dialog (owned by BookkeepingInbox) with the lines. */
  onOpen: (pair: VerificationPair, lines: InboxClassification[]) => void;
  /** Collapsed by default on a phone. */
  defaultExpanded: boolean;
}

/**
 * VerificationPairRow — one entry of the Afschriften list for a card check
 * and its refund (−€ 1,00 and +€ 1,00 under the same reference). The two
 * lines are not in the flat list; they are read-only sub-rows here. "Boeken
 * (2)" asks BookkeepingInbox to open the confirm dialog with both lines; the
 * dialog state lives there so it survives a booked leg leaving this row.
 *
 * Accessibility: the row is a <li>; the chip is a button described by its
 * tooltip; the sub-rows are a nested <ul>; the amounts are read out as
 * "Afgeschreven/Bijgeschreven € 1,00" with the visible sign hidden from the
 * reader; the net total is decoration (the summary line says it in words).
 */
export function VerificationPairRow({ pair, lines, checked, onToggle, ledgerAccounts, onOpen, defaultExpanded }: Props) {
  const [expanded, setExpanded] = useState(defaultExpanded);

  const panelId = `verification-pair-panel-${pair.id}`;
  const headerId = `verification-pair-header-${pair.id}`;
  const kind = lines[0]?.verification?.kind ?? "card_check";
  const kindLabel = VERIFICATION_CHIP_LABELS[kind];
  const ledger = ledgerLabel(lines[0]?.grootboekcode ?? "", ledgerAccounts);
  const total = pairTotal(lines);

  return (
    <Box
      component="li"
      sx={{
        listStyle: "none",
        borderBottom: "1px solid",
        borderColor: "divider",
        bgcolor: "background.paper",
        "&:last-child": { borderBottom: 0 },
      }}
    >
      <Box
        id={headerId}
        sx={{
          display: "flex",
          alignItems: "center",
          flexWrap: { xs: "wrap", sm: "nowrap" },
          gap: { xs: 1, sm: 2 },
          px: { xs: 1.5, sm: 2.5 },
          py: 1.5,
        }}
      >
        <Checkbox
          checked={checked}
          onChange={() => onToggle(lines.map((l) => l.id), !checked)}
          inputProps={{ "aria-label": `Selecteer de ${lines.length} regels van deze ${kindLabel.toLowerCase()}` }}
          size="small"
          sx={{ p: 0.5 }}
        />

        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap", mb: 0.25 }}>
            <Tooltip title={VERIFICATION_CHIP_TOOLTIPS[kind]} arrow enterDelay={300} describeChild>
              <Chip component="button" type="button" label={kindLabel} size="small" sx={VERIFICATION_CHIP_SX} />
            </Tooltip>
            <Box sx={{ minWidth: 0, flex: 1 }}>
              <TruncatedText text={lines[0]?.omschrijving ?? ""} variant="body2" sx={{ fontWeight: 600 }} />
            </Box>
          </Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
            {pairSummaryLine(lines)}
            {ledger ? ` · boeken op ${ledger}` : ""}
          </Typography>
        </Box>

        <Typography
          variant="body2"
          aria-hidden="true"
          sx={{
            fontWeight: 600,
            fontVariantNumeric: "tabular-nums",
            color: "text.secondary",
            flexShrink: 0,
            display: { xs: "none", sm: "block" },
          }}
        >
          {formatMoney(total)}
        </Typography>

        {/* On a phone the button wraps under the description instead of
            squeezing it. */}
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexBasis: { xs: "100%", sm: "auto" }, justifyContent: "flex-end" }}>
          <Button
            variant="contained"
            size="small"
            onClick={() => onOpen(pair, lines)}
            aria-label={boekenPairLabel(lines.length, false, ledger)}
            sx={{ flexShrink: 0, fontWeight: 600, minHeight: 32 }}
          >
            {pairButtonLabel(lines.length, false)}
          </Button>

          <IconButton
            size="small"
            onClick={() => setExpanded((prev) => !prev)}
            aria-expanded={expanded}
            aria-controls={panelId}
            aria-label={expanded ? "Regels inklappen" : "Regels uitklappen"}
            sx={{
              p: 0.5,
              transition: "transform 0.2s",
              transform: expanded ? "rotate(180deg)" : "rotate(0deg)",
              "@media (prefers-reduced-motion: reduce)": { transition: "none" },
            }}
          >
            <Box
              component="svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth={2}
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
              sx={{ width: 18, height: 18 }}
            >
              <polyline points="6 9 12 15 18 9" />
            </Box>
          </IconButton>
        </Box>
      </Box>

      <Collapse in={expanded}>
        <Box
          component="ul"
          id={panelId}
          aria-label={`Regels van deze ${kindLabel.toLowerCase()}`}
          sx={{ m: 0, p: 0, pb: 1, pl: { xs: 1.5, sm: 7 }, pr: { xs: 1.5, sm: 2.5 } }}
        >
          {lines.map((l) => (
            <Box
              component="li"
              key={l.id}
              sx={{
                listStyle: "none",
                display: "grid",
                gridTemplateColumns: { xs: "auto 1fr", sm: "56px 110px 1fr" },
                columnGap: { xs: 1, sm: 2 },
                rowGap: 0.25,
                alignItems: "center",
                py: 0.75,
                borderTop: "1px dashed",
                borderColor: "divider",
              }}
            >
              <Typography variant="body2" color="text.secondary" sx={{ fontSize: "0.8125rem" }}>
                {formatShortDate(l.datum)}
              </Typography>
              <Typography
                variant="body2"
                aria-label={amountAriaLabel(l.bedrag)}
                sx={{
                  fontWeight: 600,
                  fontVariantNumeric: "tabular-nums",
                  fontSize: "0.875rem",
                  textAlign: { sm: "right" },
                  color: l.bedrag < 0 ? "error.main" : "success.dark",
                }}
              >
                <Box component="span" aria-hidden="true">
                  {formatMoney(l.bedrag)}
                </Box>
              </Typography>
              <Box sx={{ minWidth: 0, gridColumn: { xs: "1 / -1", sm: "auto" } }}>
                <TruncatedText text={l.omschrijving} variant="caption" color="text.secondary" />
              </Box>
            </Box>
          ))}
        </Box>
      </Collapse>
    </Box>
  );
}
