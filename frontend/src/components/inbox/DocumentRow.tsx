import { useCallback, useLayoutEffect, useRef, useState } from "react";
import {
  Box,
  Button,
  Chip,
  CircularProgress,
  Divider,
  IconButton,
  ListItemText,
  Menu,
  MenuItem,
  Tooltip,
  Typography,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import type { MailDocument } from "../../api/types";
import { api } from "../../api/client";
import {
  canAnalyze,
  documentAriaLabel,
  documentDate,
  documentSubtitle,
  formatDocumentAmount,
  formatShortDate,
  isAnalyzed,
  isCreditnota,
  primaryActionFor,
  type DocumentRowState,
  type PrimaryAction,
} from "../../hooks/mailDocumentsModel";

/** Chip colours. Same tokens as InboxRow; every chip carries a text label
 *  so colour never stands alone. */
const CHIP_TONES = {
  grey: { color: "#475569", bgcolor: "rgba(100, 116, 139, 0.12)" },
  green: { color: "#166534", bgcolor: "rgba(22, 163, 74, 0.1)" },
  amber: { color: "#92400e", bgcolor: "rgba(245, 158, 11, 0.1)" },
  red: { color: "#991b1b", bgcolor: "rgba(220, 38, 38, 0.1)" },
  blue: { color: "#1e40af", bgcolor: "rgba(59, 130, 246, 0.1)" },
} as const;

type ChipTone = keyof typeof CHIP_TONES;

const STATUS_CHIPS: Record<MailDocument["status"], { label: string; tone: ChipTone }> = {
  new: { label: "Nieuw", tone: "grey" },
  matched: { label: "Betaling gevonden", tone: "green" },
  unmatched: { label: "Geen betaling gevonden", tone: "amber" },
  receipt_waiting: { label: "Wacht op afschrift", tone: "amber" },
  linked: { label: "Al gekoppeld", tone: "grey" },
  duplicate: { label: "Mogelijk dubbel", tone: "amber" },
  not_invoice: { label: "Geen factuur?", tone: "grey" },
  junk: { label: "Geen factuur?", tone: "grey" },
  unsupported: { label: "Niet ondersteund", tone: "grey" },
  ignored: { label: "Genegeerd", tone: "grey" },
};

function chipFor(doc: MailDocument, rowState?: DocumentRowState): { label: string; tone: ChipTone; spinner?: boolean } {
  if (rowState?.kind === "analyzing") return { label: "Analyseren…", tone: "blue", spinner: true };
  if (rowState?.kind === "queued") return { label: "In wachtrij", tone: "grey" };
  if (rowState?.kind === "error") return { label: "Analyse mislukt", tone: "red" };
  return STATUS_CHIPS[doc.status];
}

/** The accessible name of the primary button, written out in full. */
function primaryAriaLabel(action: PrimaryAction, doc: MailDocument): string {
  switch (action.kind) {
    case "analyze":
      return documentAriaLabel("Analyseer", doc);
    case "retry":
      return `${documentAriaLabel("Analyseer", doc)} opnieuw`;
    case "book":
      return documentAriaLabel("Boek", doc);
    case "archive":
      return `${documentAriaLabel("Ruim", doc)} op`;
    case "ignore":
      return documentAriaLabel("Negeer", doc);
    case "unignore":
      return `${documentAriaLabel("Negeer", doc)} niet meer`;
    case "wait":
      return `${action.label} ${doc.title}`;
  }
}

interface Props {
  doc: MailDocument;
  rowState?: DocumentRowState;
  /** Whether Claude can be called at all; null when it can. */
  aiBlockedReason: string | null;
  /** An "Alles analyseren" run is active: no extra analyses start. */
  runActive: boolean;
  onAnalyze: (id: number, force?: boolean) => void;
  onIgnore: (id: number) => void;
  onUnignore: (id: number) => void;
  onArchive: (id: number) => void;
  /** Opens the move dialog; the parent owns it so there is one for the list. */
  onRequestMove: (doc: MailDocument) => void;
  /** Opens the delete confirmation; same ownership. */
  onRequestDelete: (doc: MailDocument) => void;
  /** Opens the booking flow. Only when provided do matched and unmatched
   *  rows get their "Boeken" button. */
  onBook?: (doc: MailDocument) => void;
  /** Receives the element that should get focus when the row before it
   *  disappears: the primary button, or on phones the row itself. */
  focusTargetRef?: (id: number, el: HTMLElement | null) => void;
}

/**
 * Renders text truncated with an ellipsis, with the full string in a tooltip
 * only when it actually overflows. Same pattern as InboxRow.
 */
function TruncatedText({
  text,
  variant,
  color,
  sx,
}: {
  text: string;
  variant: "body2" | "caption";
  color?: string;
  sx?: object;
}) {
  const ref = useRef<HTMLElement>(null);
  const [overflowing, setOverflowing] = useState(false);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const check = () => setOverflowing(el.scrollWidth > el.clientWidth);
    check();
    const ro = new ResizeObserver(check);
    ro.observe(el);
    return () => ro.disconnect();
  }, [text]);

  const node = (
    <Typography
      ref={ref}
      variant={variant}
      color={color}
      sx={{
        display: "block",
        overflow: "hidden",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
        ...sx,
      }}
    >
      {text}
    </Typography>
  );

  if (!overflowing) return node;

  return (
    <Tooltip title={text} arrow enterDelay={300} enterNextDelay={100}>
      {node}
    </Tooltip>
  );
}

/**
 * DocumentRow — one file from the mail folder, in the visual language of
 * InboxRow: date | amount | title + subtitle | status chip | primary action
 * | ⋮ menu. The status decides the chip, the note under the title and the
 * primary action (see mailDocumentsModel); the menu holds the rest.
 *
 * On phones the row is two lines (title + amount, date · chip) and tapping
 * the row performs the primary action; the ⋮ button keeps a 44 px target.
 *
 * Accessibility: the row is a <li>; every chip has a text label; the
 * primary button's name says what it does to which document; a row being
 * analysed is aria-busy; a disabled primary action keeps a tooltip through
 * its wrapper span.
 */
export function DocumentRow({
  doc,
  rowState,
  aiBlockedReason,
  runActive,
  onAnalyze,
  onIgnore,
  onUnignore,
  onArchive,
  onRequestMove,
  onRequestDelete,
  onBook,
  focusTargetRef,
}: Props) {
  const theme = useTheme();
  const isXs = useMediaQuery(theme.breakpoints.down("sm"));
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);

  const busy = rowState?.kind === "analyzing" || rowState?.kind === "queued";
  const analyzeBlocked = !!aiBlockedReason || runActive || busy || !canAnalyze(doc);
  const action = primaryActionFor(doc, rowState, !!onBook);
  const chip = chipFor(doc, rowState);
  const amount = formatDocumentAmount(doc);
  const creditnota = isCreditnota(doc);
  const date = formatShortDate(documentDate(doc));
  const subtitle = documentSubtitle(doc, rowState);

  // An analyse action on a row is also blocked when AI is blocked; the
  // reason goes in the tooltip.
  const actionDisabled =
    !action ||
    !!action.disabled ||
    ((action.kind === "analyze" || action.kind === "retry") && (!!aiBlockedReason || runActive));
  const actionReason = !action
    ? null
    : action.disabled
      ? action.reason ?? null
      : action.kind === "analyze" || action.kind === "retry"
        ? aiBlockedReason ?? (runActive ? "Wacht tot de huidige analyse klaar is" : null)
        : null;

  const runPrimary = useCallback(() => {
    if (!action || actionDisabled) return;
    switch (action.kind) {
      case "analyze":
      case "retry":
        onAnalyze(doc.id);
        break;
      case "book":
        onBook?.(doc);
        break;
      case "archive":
        onArchive(doc.id);
        break;
      case "ignore":
        onIgnore(doc.id);
        break;
      case "unignore":
        onUnignore(doc.id);
        break;
    }
  }, [action, actionDisabled, doc, onAnalyze, onArchive, onBook, onIgnore, onUnignore]);

  const closeMenu = () => setMenuAnchor(null);
  const menuAction = (fn: () => void) => () => {
    closeMenu();
    fn();
  };

  const openPreview = () => {
    window.open(api.getDocumentContentUrl(doc.id), "_blank", "noopener,noreferrer");
  };

  const chipNode = (
    <Chip
      label={chip.label}
      size="small"
      icon={chip.spinner ? <CircularProgress size={12} color="inherit" aria-hidden="true" /> : undefined}
      sx={{
        fontWeight: 600,
        fontSize: "0.6875rem",
        height: 24,
        flexShrink: 0,
        bgcolor: CHIP_TONES[chip.tone].bgcolor,
        color: CHIP_TONES[chip.tone].color,
        "& .MuiChip-icon": { color: "inherit", ml: 0.75 },
      }}
    />
  );

  const creditnotaChip = creditnota ? (
    <Chip
      label="Creditnota"
      size="small"
      sx={{
        fontWeight: 600,
        fontSize: "0.6875rem",
        height: 24,
        flexShrink: 0,
        bgcolor: CHIP_TONES.blue.bgcolor,
        color: CHIP_TONES.blue.color,
      }}
    />
  ) : null;

  const primaryButton = action ? (
    <Tooltip title={actionReason ?? ""} arrow disableHoverListener={!actionReason} disableFocusListener={!actionReason}>
      <Box component="span" sx={{ display: "inline-flex", flexShrink: 0 }}>
        <Button
          ref={(el: HTMLButtonElement | null) => focusTargetRef?.(doc.id, el)}
          variant={action.kind === "book" ? "contained" : "outlined"}
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            runPrimary();
          }}
          disabled={actionDisabled}
          aria-label={primaryAriaLabel(action, doc)}
          aria-busy={rowState?.kind === "analyzing" || undefined}
          sx={{ fontWeight: 600, minHeight: 32, minWidth: 96, whiteSpace: "nowrap" }}
        >
          {action.label}
        </Button>
      </Box>
    </Tooltip>
  ) : null;

  const menuButton = (
    <IconButton
      onClick={(e) => {
        e.stopPropagation();
        setMenuAnchor(e.currentTarget);
      }}
      aria-label={`Meer acties voor ${doc.title}`}
      aria-haspopup="menu"
      aria-expanded={menuAnchor ? true : undefined}
      aria-controls={menuAnchor ? `document-menu-${doc.id}` : undefined}
      sx={{ width: 44, height: 44, flexShrink: 0 }}
    >
      {/* Vertical ellipsis */}
      <Box
        component="svg"
        viewBox="0 0 24 24"
        fill="currentColor"
        aria-hidden="true"
        sx={{ width: 20, height: 20 }}
      >
        <circle cx="12" cy="5" r="1.8" />
        <circle cx="12" cy="12" r="1.8" />
        <circle cx="12" cy="19" r="1.8" />
      </Box>
    </IconButton>
  );

  const menu = (
    <Menu
      id={`document-menu-${doc.id}`}
      anchorEl={menuAnchor}
      open={!!menuAnchor}
      onClose={closeMenu}
      anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
      transformOrigin={{ vertical: "top", horizontal: "right" }}
    >
      {doc.mediaType !== "" && (
        <MenuItem onClick={menuAction(openPreview)}>
          <ListItemText primary="Bekijken" secondary="Opent in een nieuw tabblad" />
        </MenuItem>
      )}
      {doc.status === "ignored" ? (
        <MenuItem onClick={menuAction(() => onUnignore(doc.id))}>Niet meer negeren</MenuItem>
      ) : (
        <MenuItem onClick={menuAction(() => onIgnore(doc.id))}>Negeren</MenuItem>
      )}
      {doc.status === "junk" && (
        <MenuItem onClick={menuAction(() => onAnalyze(doc.id))} disabled={analyzeBlocked}>
          Toch analyseren
        </MenuItem>
      )}
      {(doc.status === "duplicate" || doc.status === "not_invoice") && onBook && (
        <MenuItem onClick={menuAction(() => onBook(doc))}>Toch boeken</MenuItem>
      )}
      {canAnalyze(doc) && isAnalyzed(doc) && (
        <MenuItem onClick={menuAction(() => onAnalyze(doc.id, true))} disabled={analyzeBlocked}>
          Opnieuw analyseren
        </MenuItem>
      )}
      <MenuItem onClick={menuAction(() => onRequestMove(doc))}>Verplaatsen naar…</MenuItem>
      {doc.status !== "linked" && <Divider />}
      {doc.status !== "linked" && (
        <MenuItem onClick={menuAction(() => onRequestDelete(doc))} sx={{ color: "error.main" }}>
          Verwijderen uit e-Boekhouden
        </MenuItem>
      )}
    </Menu>
  );

  const rowSx = {
    listStyle: "none",
    borderBottom: "1px solid",
    borderColor: "divider",
    bgcolor: "background.paper",
    transition: "background-color 0.2s",
    "@media (prefers-reduced-motion: reduce)": { transition: "none" },
    "&:last-child": { borderBottom: 0 },
  } as const;

  if (isXs) {
    // Phone: the row is the primary action; the menu sits beside it.
    const tappable = !!action && !actionDisabled;
    return (
      <Box component="li" sx={rowSx} data-document-id={doc.id} aria-busy={rowState?.kind === "analyzing" || undefined}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, pl: 1.5, pr: 0.5, py: 1 }}>
          <Box
            ref={(el: HTMLElement | null) => focusTargetRef?.(doc.id, el)}
            role={tappable ? "button" : undefined}
            tabIndex={tappable ? 0 : undefined}
            aria-label={action && tappable ? primaryAriaLabel(action, doc) : undefined}
            onClick={tappable ? runPrimary : undefined}
            onKeyDown={(e) => {
              if (tappable && (e.key === "Enter" || e.key === " ")) {
                e.preventDefault();
                runPrimary();
              }
            }}
            sx={{
              flex: 1,
              minWidth: 0,
              cursor: tappable ? "pointer" : "default",
              borderRadius: 1,
              py: 0.5,
              "&:focus-visible": { outline: "2px solid", outlineColor: "primary.main", outlineOffset: 2 },
            }}
          >
            <Box sx={{ display: "flex", alignItems: "baseline", gap: 1 }}>
              <TruncatedText text={doc.title} variant="body2" sx={{ fontWeight: 500, flex: 1 }} />
              {amount && (
                <Typography
                  variant="body2"
                  sx={{
                    fontWeight: 600,
                    fontVariantNumeric: "tabular-nums",
                    fontSize: "0.875rem",
                    flexShrink: 0,
                    color: creditnota ? "success.dark" : "text.primary",
                  }}
                >
                  {amount}
                </Typography>
              )}
            </Box>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, mt: 0.5, flexWrap: "wrap" }}>
              {date && (
                <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
                  {date}
                </Typography>
              )}
              {date && (
                <Typography variant="caption" color="text.secondary" aria-hidden="true">
                  ·
                </Typography>
              )}
              {chipNode}
              {creditnotaChip}
              {action && !tappable && actionReason && (
                <Typography variant="caption" color="text.secondary">
                  {actionReason}
                </Typography>
              )}
            </Box>
            {rowState?.kind === "error" && (
              <Typography variant="caption" color="error.main" sx={{ display: "block", mt: 0.5 }}>
                {subtitle}
              </Typography>
            )}
          </Box>
          {menuButton}
        </Box>
        {menu}
      </Box>
    );
  }

  return (
    <Box component="li" sx={rowSx} data-document-id={doc.id} aria-busy={rowState?.kind === "analyzing" || undefined}>
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 2,
          pl: 2.5,
          pr: 1,
          py: 1,
        }}
      >
        {/* Date */}
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ minWidth: 52, fontSize: "0.8125rem", flexShrink: 0 }}
        >
          {date}
        </Typography>

        {/* Amount: "–" keeps the column while a document is unanalysed */}
        <Typography
          variant="body2"
          aria-hidden={amount ? undefined : true}
          sx={{
            minWidth: 88,
            textAlign: "right",
            fontWeight: 600,
            fontVariantNumeric: "tabular-nums",
            fontSize: "0.875rem",
            whiteSpace: "nowrap",
            flexShrink: 0,
            color: amount ? (creditnota ? "success.dark" : "text.primary") : "text.disabled",
          }}
        >
          {amount ?? "–"}
        </Typography>

        {/* Title + subtitle */}
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <TruncatedText text={doc.title} variant="body2" sx={{ fontWeight: 500 }} />
          {subtitle && (
            <TruncatedText
              text={subtitle}
              variant="caption"
              color={rowState?.kind === "error" ? "error.main" : "text.secondary"}
            />
          )}
        </Box>

        {creditnotaChip}
        {chipNode}
        {primaryButton}
        {menuButton}
      </Box>
      {menu}
    </Box>
  );
}
