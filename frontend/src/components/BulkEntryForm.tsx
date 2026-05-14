import { useState, useCallback } from "react";
import {
  Box,
  Button,
  Typography,
  Alert,
  Divider,
  TextField,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
} from "@mui/material";
import SendIcon from "@mui/icons-material/Send";
import { api } from "../api/client";
import type { Employee, Project, Activity, EntryResult, BulkEntry } from "../api/types";
import { useEmployees } from "../hooks/useEmployees";
import { useProjects } from "../hooks/useProjects";
import { useActivities } from "../hooks/useActivities";
import { useHourOverview } from "../hooks/useHourOverview";
import { strictMatchKey, formatHours } from "./hours/hourOverlay";
import { EmployeeSelector } from "./EmployeeSelector";
import { ProjectSelector } from "./ProjectSelector";
import { ActivitySelector } from "./ActivitySelector";
import { HoursInput } from "./HoursInput";
import { MonthCalendar } from "./MonthCalendar";
import { SubmitResults } from "./SubmitResults";
import { track } from "../analytics";

export function BulkEntryForm() {
  const { employees, loading: empLoading } = useEmployees();
  const { projects, loading: projLoading } = useProjects();
  const { activities, loading: actLoading } = useActivities();

  const [selectedEmployees, setSelectedEmployees] = useState<Employee[]>([]);
  const [selectedProject, setSelectedProject] = useState<Project | null>(null);
  const [selectedActivity, setSelectedActivity] = useState<Activity | null>(null);
  const [hours, setHours] = useState("8.00");
  const [description, setDescription] = useState("");
  const [selectedDates, setSelectedDates] = useState<Set<string>>(new Set());

  const [results, setResults] = useState<EntryResult[] | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  // Visible calendar range — updated by MonthCalendar when the user
  // navigates between months. Drives the hour-overview fetch so badges
  // and duplicate warnings stay in sync with whatever month is on screen.
  const [visibleRange, setVisibleRange] = useState<{ from: string; to: string } | null>(null);

  // State for the duplicate-confirmation dialog: a list of conflicts to
  // surface ("Joost · 2026-04-01 · PUP · Consultancy") plus a resolved
  // submit handler that runs after the user clicks Toch indienen.
  const [duplicateConflicts, setDuplicateConflicts] = useState<string[] | null>(null);

  const totalEntries = selectedEmployees.length * selectedDates.size;
  const totalHours = totalEntries * parseFloat(hours || "0");

  // Pull previously-booked hours for the visible calendar range. Filtered
  // to the currently-selected employees so the badge counts are scoped to
  // who you're booking for (matches the user's mental model — "is THIS
  // person already booked on this day").
  const { overlay: hourOverlay, loading: overlayLoading } = useHourOverview({
    from: visibleRange?.from ?? null,
    to: visibleRange?.to ?? null,
    employeeIds: selectedEmployees.map((e) => e.id),
  });

  const canSubmit =
    selectedEmployees.length > 0 &&
    selectedProject !== null &&
    selectedActivity !== null &&
    selectedDates.size > 0 &&
    parseFloat(hours) > 0 &&
    !submitting;

  /** Detect strict-match duplicates between the current selection and the
   *  already-booked hours overlay. Returns the list of human-readable
   *  conflict strings (empty when there are none) so the confirmation
   *  dialog can show exactly what would be doubled. */
  const findDuplicateConflicts = useCallback((): string[] => {
    if (!selectedProject || !selectedActivity) return [];
    const conflicts: string[] = [];
    for (const emp of selectedEmployees) {
      for (const date of selectedDates) {
        const key = strictMatchKey({
          date,
          employeeName: emp.naam,
          projectLabel: selectedProject.naam,
          activityLabel: selectedActivity.naam,
        });
        if (hourOverlay.strictKeys.has(key)) {
          conflicts.push(
            `${date} · ${emp.naam} · ${selectedProject.naam} · ${selectedActivity.naam}`,
          );
        }
      }
    }
    return conflicts;
  }, [selectedEmployees, selectedProject, selectedActivity, selectedDates, hourOverlay]);

  const performSubmit = async () => {
    if (!selectedProject || !selectedActivity) return;

    setError("");
    setResults(null);
    setSubmitting(true);

    const dates = Array.from(selectedDates).sort();
    const entries: BulkEntry[] = selectedEmployees.map((emp) => ({
      employeeId: emp.id,
      projectId: selectedProject.id,
      activityId: selectedActivity.id,
      hours: parseFloat(hours).toFixed(2),
      dates,
      description,
    }));

    try {
      const res = await api.submitHours({ entries });
      setResults(res.results);
      const ok = res.results.filter((r) => r.status === "ok").length;
      const fail = res.results.filter((r) => r.status === "error").length;
      track("Hours Submitted", {
        total: String(res.results.length),
        succeeded: String(ok),
        failed: String(fail),
        employees: String(selectedEmployees.length),
        days: String(selectedDates.size),
      });
    } catch (err) {
      track("Hours Submit Error", { reason: err instanceof Error ? err.message : "unknown" });
      setError(err instanceof Error ? err.message : "Submission failed");
    } finally {
      setSubmitting(false);
    }
  };

  /** Top-level submit handler — guards on strict-match duplicates before
   *  hitting the API. If any conflicts exist, opens the confirmation
   *  dialog; the dialog's "Toch indienen" button calls performSubmit. */
  const handleSubmit = () => {
    const conflicts = findDuplicateConflicts();
    if (conflicts.length > 0) {
      setDuplicateConflicts(conflicts);
      track("Hours Duplicate Warning Shown", { count: String(conflicts.length) });
      return;
    }
    performSubmit();
  };

  const handleReset = () => {
    setResults(null);
    setSelectedEmployees([]);
    setSelectedDates(new Set());
    setDescription("");
    setError("");
  };

  // After successful submission, show only results
  if (results && results.every((r) => r.status === "ok")) {
    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
        <SubmitResults results={results} loading={false} total={totalEntries} onReset={handleReset} />
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <EmployeeSelector
        employees={employees}
        selected={selectedEmployees}
        onChange={setSelectedEmployees}
        loading={empLoading}
      />

      <ProjectSelector
        projects={projects}
        selected={selectedProject}
        onChange={setSelectedProject}
        loading={projLoading}
      />

      <ActivitySelector
        activities={activities}
        selected={selectedActivity}
        onChange={setSelectedActivity}
        loading={actLoading}
      />

      <HoursInput value={hours} onChange={setHours} />

      <TextField
        label="Omschrijving (optioneel)"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        fullWidth
        multiline
        rows={2}
      />

      <MonthCalendar
        selectedDates={selectedDates}
        onChange={setSelectedDates}
        bookedHours={hourOverlay.byDate}
        bookedDetails={hourOverlay.byDateDetails}
        onVisibleRangeChange={(from, to) => setVisibleRange({ from, to })}
      />

      {selectedEmployees.length > 0 && hourOverlay.byDate.size > 0 && !overlayLoading && (
        <Typography variant="caption" color="text.secondary">
          {hourOverlay.byDate.size} {hourOverlay.byDate.size === 1 ? "dag" : "dagen"} in deze maand
          {selectedEmployees.length === 1 ? " heeft" : " hebben"} al uren — zichtbaar als badge in de
          rechterbovenhoek van de cel.
        </Typography>
      )}

      <Divider />

      {selectedDates.size > 0 && selectedEmployees.length > 0 && (
        <Typography variant="body2" color="text.secondary">
          {selectedDates.size} dagen &times; {selectedEmployees.length} medewerker
          {selectedEmployees.length > 1 ? "s" : ""} &times; {hours}u ={" "}
          <strong>{totalHours.toFixed(2)} uur totaal</strong>
        </Typography>
      )}

      {error && <Alert severity="error">{error}</Alert>}

      <Button
        variant="contained"
        size="large"
        startIcon={<SendIcon />}
        onClick={handleSubmit}
        disabled={!canSubmit}
      >
        {submitting ? "Bezig..." : `Indienen (${totalEntries} items)`}
      </Button>

      <SubmitResults results={results} loading={submitting} total={totalEntries} onReset={handleReset} />

      {/* Duplicate-confirmation dialog. Listed conflicts let the user
          see exactly which (date · employee · project · activity)
          combinations already exist in e-boekhouden before deciding to
          either go back and adjust the selection, or override. */}
      <Dialog
        open={duplicateConflicts !== null}
        onClose={() => !submitting && setDuplicateConflicts(null)}
        maxWidth="sm"
        fullWidth
        aria-labelledby="duplicate-hours-title"
      >
        <DialogTitle id="duplicate-hours-title" sx={{ fontWeight: 600 }}>
          {duplicateConflicts && duplicateConflicts.length === 1
            ? "1 dubbele uren-boeking gevonden"
            : `${duplicateConflicts?.length ?? 0} dubbele uren-boekingen gevonden`}
        </DialogTitle>
        <DialogContent dividers>
          <DialogContentText sx={{ mb: 2 }}>
            Deze combinaties van datum · medewerker · project · activiteit zijn al eerder in
            e-Boekhouden geboekt. Doorgaan maakt nieuwe uren-regels aan bovenop wat er al staat —
            dat betekent dubbele uren.
          </DialogContentText>
          <Box
            sx={{
              maxHeight: 240,
              overflow: "auto",
              border: "1px solid",
              borderColor: "divider",
              borderRadius: 1,
              p: 1.5,
              bgcolor: "grey.50",
            }}
          >
            {duplicateConflicts?.map((line, i) => (
              <Typography
                key={i}
                variant="caption"
                component="div"
                sx={{ fontFamily: "monospace", lineHeight: 1.7 }}
              >
                {line}
              </Typography>
            ))}
          </Box>
          <DialogContentText sx={{ mt: 2 }}>
            Totaal te boeken in deze actie: <strong>{formatHours(totalHours)}</strong>{" "}
            ({totalEntries} regels). Pas je selectie aan om de dubbele dagen te deselecteren,
            of ga toch door als je weet wat je doet.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDuplicateConflicts(null)} disabled={submitting}>
            Terug om aan te passen
          </Button>
          <Button
            color="warning"
            variant="contained"
            disabled={submitting}
            onClick={() => {
              setDuplicateConflicts(null);
              track("Hours Duplicate Override", { count: String(duplicateConflicts?.length ?? 0) });
              performSubmit();
            }}
          >
            Toch indienen
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
