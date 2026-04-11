import { useEffect, useState } from "react";
import {
  Box,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Chip,
  IconButton,
  Tooltip,
  Alert,
  Button,
  Skeleton,
} from "@mui/material";
import RefreshIcon from "@mui/icons-material/Refresh";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import { api } from "../../api/client";
import { useAuth } from "../../context/AuthContext";
import type { BankStatementRow } from "../../api/types";
import { ProcessDialog } from "./ProcessDialog";

export function BankStatementList() {
  const { eboekhoudenConnected } = useAuth();
  const [rows, setRows] = useState<BankStatementRow[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selectedRow, setSelectedRow] = useState<BankStatementRow | null>(null);

  const loadData = async () => {
    if (!eboekhoudenConnected) return;
    setLoading(true);
    setError("");
    try {
      const res = await api.getBankStatements();
      setRows(res.items);
      setTotalCount(res.totalCount);
    } catch (err: any) {
      setError(err.message || "Afschriften konden niet worden geladen");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, [eboekhoudenConnected]);

  const handleProcessed = (processedId: number) => {
    setRows((prev) => prev.filter((r) => r.id !== processedId));
    setTotalCount((prev) => prev - 1);
    setSelectedRow(null);
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
        Verbind met e-Boekhouden om afschriftregels te bekijken.
      </Alert>
    );
  }

  const formatAmount = (amount: number) => {
    const formatted = Math.abs(amount).toFixed(2);
    return amount >= 0 ? `+\u20AC${formatted}` : `-\u20AC${formatted}`;
  };

  const formatDate = (dateStr: string) => {
    if (!dateStr) return "-";
    try {
      return new Date(dateStr).toLocaleDateString("nl-NL");
    } catch {
      return dateStr;
    }
  };

  return (
    <Box>
      <Box sx={{ display: "flex", alignItems: "center", mb: 3 }}>
        <Typography variant="h4" component="h1" fontWeight={600} sx={{ flexGrow: 1 }}>
          Afschriften
        </Typography>
        <Chip label={`${totalCount} onverwerkt`} color="primary" sx={{ mr: 1 }} />
        <Tooltip title="Verversen">
          <IconButton onClick={loadData} disabled={loading}>
            <RefreshIcon />
          </IconButton>
        </Tooltip>
      </Box>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Datum</TableCell>
              <TableCell>Omschrijving</TableCell>
              <TableCell align="right">Bedrag</TableCell>
              <TableCell>Rekening</TableCell>
              <TableCell>Status</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              Array.from({ length: 5 }).map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton /></TableCell>
                  <TableCell><Skeleton /></TableCell>
                  <TableCell><Skeleton /></TableCell>
                  <TableCell><Skeleton /></TableCell>
                  <TableCell><Skeleton /></TableCell>
                </TableRow>
              ))
            ) : rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} align="center">
                  <Box sx={{ py: 4, display: "flex", flexDirection: "column", alignItems: "center" }}>
                    <CheckCircleIcon sx={{ fontSize: 48, color: "success.main", mb: 1 }} />
                    <Typography color="text.secondary">Alle afschriftregels zijn verwerkt</Typography>
                  </Box>
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => (
                <TableRow
                  key={row.id}
                  hover
                  onClick={() => setSelectedRow(row)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      setSelectedRow(row);
                    }
                  }}
                  tabIndex={0}
                  role="button"
                  aria-label={`${formatDate(row.mutDatum)}: ${row.mutOmschrijving}, ${formatAmount(row.mutBedrag)}`}
                  sx={{ cursor: "pointer", "&:focus-visible": { outline: "2px solid", outlineColor: "primary.main", outlineOffset: -2 } }}
                >
                  <TableCell>{formatDate(row.mutDatum)}</TableCell>
                  <TableCell sx={{ maxWidth: 400, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {row.mutOmschrijving}
                  </TableCell>
                  <TableCell align="right" sx={{ color: row.mutBedrag >= 0 ? "success.main" : "error.main", fontWeight: 600 }}>
                    {formatAmount(row.mutBedrag)}
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2" sx={{ fontFamily: "monospace" }}>{row.rekening}</Typography>
                  </TableCell>
                  <TableCell>
                    {row.verwerkFailureReason ? (
                      <Chip label="Fout" color="error" size="small" />
                    ) : (
                      <Chip label="Open" size="small" />
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </TableContainer>

      {selectedRow && (
        <ProcessDialog
          row={selectedRow}
          onClose={() => setSelectedRow(null)}
          onProcessed={handleProcessed}
        />
      )}
    </Box>
  );
}
