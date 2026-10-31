import { useState } from "react";
import { Box, Button, Checkbox, Chip, Collapse, IconButton, Tooltip, Typography } from "@mui/material";
import type { InboxClassification, LedgerAccount, PaymentGroup } from "../../api/types";
import { formatMoney, formatShortDate } from "../../hooks/mailDocumentsModel";
import { koppelenGroupLabel } from "./inboxBooking";
import {
  GROUP_CHIP_LABELS,
  GROUP_CHIP_TOOLTIPS,
  accountLabel,
  groupButtonLabel,
  groupProgress,
  groupSummaryLine,
  groupTitle,
} from "./paymentGroups";
import { TruncatedText } from "./TruncatedText";

interface Props {
  group: PaymentGroup;
  /** The group's lines that are still in the list. */
  lines: InboxClassification[];
  /** True when every line of the group is selected for the batch bar. */
  checked: boolean;
  /** Selects or deselects all lines of the group at once. */
  onToggle: (ids: number[], select: boolean) => void;
  ledgerAccounts: LedgerAccount[];
  /** Opens the group dialog. */
  onOpen: (group: PaymentGroup, lines: InboxClassification[]) => void;
  /** Collapsed by default on a phone. */
  defaultExpanded: boolean;
}

/** Chip colours per state. Text label always, never colour alone. */
const CHIP_STYLE: Record<PaymentGroup["state"], { color: string; bgcolor: string }> = {
  complete: { color: "#0f766e", bgcolor: "rgba(13, 148, 136, 0.12)" },
  partial: { color: "#92400e", bgcolor: "rgba(245, 158, 11, 0.12)" },
  ambiguous: { color: "#1e40af", bgcolor: "rgba(59, 130, 246, 0.12)" },
};

/**
 * PaymentGroupRow — one entry of the Afschriften list for several incoming
 * lines that together pay one sales invoice. The member lines are not in
 * the flat list; they are read-only sub-rows here (date, amount, own
 * account, description). "Koppelen (n)" opens PaymentGroupDialog; an
 * ambiguous group offers "Kiezen" instead and has no checkbox, because the
 * batch bar cannot book lines without an invoice.
 *
 * Accessibility: the row is a <li>; the sub-rows are a nested <ul> named
 * after the invoice; the expand button carries aria-expanded and
 * aria-controls; the progress bar is aria-hidden because the summary line
 * says the same in words; the button's accessible name names the invoice.
 */
export function PaymentGroupRow({ group, lines, checked, onToggle, ledgerAccounts, onOpen, defaultExpanded }: Props) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const panelId = `payment-group-panel-${group.id}`;
  const headerId = `payment-group-header-${group.id}`;
  const ambiguous = group.state === "ambiguous";
  const chip = CHIP_STYLE[group.state];
  const summary = groupSummaryLine(group, lines.length);
  const progress = groupProgress(group);
  const total = lines.reduce((acc, l) => acc + l.bedrag, 0);

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
          gap: { xs: 1, sm: 2 },
          px: { xs: 1.5, sm: 2.5 },
          py: 1.5,
        }}
      >
        {ambiguous ? (
          <Box sx={{ width: 28, flexShrink: 0 }} aria-hidden="true" />
        ) : (
          <Checkbox
            checked={checked}
            onChange={() => onToggle(lines.map((l) => l.id), !checked)}
            inputProps={{ "aria-label": `Selecteer de ${lines.length} betalingen van factuur ${group.factuurnummer}` }}
            size="small"
            sx={{ p: 0.5 }}
          />
        )}

        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap", mb: 0.25 }}>
            <Tooltip title={GROUP_CHIP_TOOLTIPS[group.state]} arrow>
              <Chip
                label={GROUP_CHIP_LABELS[group.state]}
                size="small"
                sx={{ fontWeight: 600, fontSize: "0.6875rem", height: 22, cursor: "help", ...chip }}
              />
            </Tooltip>
            <Typography variant="body2" sx={{ fontWeight: 600, minWidth: 0 }} noWrap>
              {groupTitle(group)}
            </Typography>
          </Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
            {summary}
          </Typography>
          {!ambiguous && (
            <Box
              aria-hidden="true"
              sx={{ mt: 0.75, height: 4, borderRadius: 2, bgcolor: "action.hover", overflow: "hidden", maxWidth: 240 }}
            >
              <Box sx={{ width: `${progress}%`, height: "100%", bgcolor: group.state === "complete" ? "#0f766e" : "#b45309" }} />
            </Box>
          )}
        </Box>

        <Typography
          variant="body2"
          sx={{
            fontWeight: 600,
            fontVariantNumeric: "tabular-nums",
            color: "success.dark",
            flexShrink: 0,
            display: { xs: "none", sm: "block" },
          }}
        >
          {formatMoney(total)}
        </Typography>

        <Button
          variant={ambiguous ? "outlined" : "contained"}
          size="small"
          onClick={() => onOpen(group, lines)}
          aria-label={ambiguous ? `Kies de factuur voor ${lines.length} betalingen` : koppelenGroupLabel(lines.length, group.factuurnummer, group.relatieNaam)}
          sx={{ flexShrink: 0, fontWeight: 600, minHeight: 32 }}
        >
          {ambiguous ? groupButtonLabel(group) : `Koppelen (${lines.length})`}
        </Button>

        <IconButton
          size="small"
          onClick={() => setExpanded((prev) => !prev)}
          aria-expanded={expanded}
          aria-controls={panelId}
          aria-label={expanded ? "Betalingen inklappen" : "Betalingen uitklappen"}
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

      <Collapse in={expanded}>
        <Box
          component="ul"
          id={panelId}
          aria-label={group.factuurnummer ? `Betalingen voor factuur ${group.factuurnummer}` : "Betalingen van deze groep"}
          sx={{ m: 0, p: 0, pb: 1, pl: { xs: 1.5, sm: 7 }, pr: { xs: 1.5, sm: 2.5 } }}
        >
          {lines.map((l) => {
            const account = accountLabel(l, ledgerAccounts);
            return (
              <Box
                component="li"
                key={l.id}
                sx={{
                  listStyle: "none",
                  display: "grid",
                  gridTemplateColumns: { xs: "auto 1fr", sm: "56px 110px 170px 1fr" },
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
                  sx={{ fontWeight: 600, fontVariantNumeric: "tabular-nums", fontSize: "0.875rem", textAlign: { sm: "right" } }}
                >
                  {formatMoney(l.bedrag)}
                </Typography>
                <Typography variant="caption" color="text.secondary" sx={{ gridColumn: { xs: "1 / -1", sm: "auto" } }} noWrap>
                  {account || "Eigen rekening onbekend"}
                </Typography>
                <Box sx={{ minWidth: 0, gridColumn: { xs: "1 / -1", sm: "auto" } }}>
                  <TruncatedText text={l.omschrijving} variant="caption" color="text.secondary" />
                </Box>
              </Box>
            );
          })}
        </Box>
      </Collapse>
    </Box>
  );
}
