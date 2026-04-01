import { useState } from "react";
import {
  Box,
  Button,
  Typography,
  Alert,
  Divider,
  TextField,
} from "@mui/material";
import SendIcon from "@mui/icons-material/Send";
import { api } from "../api/client";
import type { Employee, Project, Activity, EntryResult, BulkEntry } from "../api/types";
import { useEmployees } from "../hooks/useEmployees";
import { useProjects } from "../hooks/useProjects";
import { useActivities } from "../hooks/useActivities";
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

  const totalEntries = selectedEmployees.length * selectedDates.size;
  const totalHours = totalEntries * parseFloat(hours || "0");

  const canSubmit =
    selectedEmployees.length > 0 &&
    selectedProject !== null &&
    selectedActivity !== null &&
    selectedDates.size > 0 &&
    parseFloat(hours) > 0 &&
    !submitting;

  const handleSubmit = async () => {
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

      <MonthCalendar selectedDates={selectedDates} onChange={setSelectedDates} />

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
    </Box>
  );
}
