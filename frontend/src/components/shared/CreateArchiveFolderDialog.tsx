import { useEffect, useState } from "react";
import {
  Alert,
  Autocomplete,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
} from "@mui/material";
import { api } from "../../api/client";
import type { ArchiveFolderWithPath } from "../../hooks/useArchiveFolders";

interface Props {
  open: boolean;
  onClose: () => void;
  /** Called with the newly-created folder's id after a successful create.
   *  Caller is responsible for refetching the folder list to surface the
   *  new entry to other components. */
  onCreated: (newFolderId: number) => void;
  /** Existing folders to pick the parent from. The user can also choose to
   *  create at the root level (parentFolderId = 0). */
  folders: ArchiveFolderWithPath[];
  /** Pre-fill the name field — typically the inputValue the user just typed
   *  in the picker so they don't have to retype. */
  initialName?: string;
  /** Pre-fill the parent picker — typically the parent of the folder
   *  currently selected in the picker so the new folder is a sibling. */
  initialParent?: ArchiveFolderWithPath | null;
}

// Sentinel used in the parent Autocomplete to represent "create at root level".
// Modelled on the existing RelationPicker pattern so the dropdown stays a flat
// list rather than a separate radio/button. Using a real ArchiveFolderWithPath
// shape with id 0 keeps the Autocomplete generic happy without resorting to
// a union type.
const ROOT_OPTION: ArchiveFolderWithPath = {
  id: 0,
  naam: "(Hoofdmap)",
  parentId: -1,
  isDeleted: false,
  path: "(Hoofdmap)",
};

/**
 * CreateArchiveFolderDialog — minimal "new folder" prompt that creates a
 * digitaal-archief folder via /api/v1/archive/folders. The picker that opens
 * this dialog is responsible for selecting the new folder after creation;
 * we just return the new id.
 */
export function CreateArchiveFolderDialog({
  open,
  onClose,
  onCreated,
  folders,
  initialName,
  initialParent,
}: Props) {
  const [name, setName] = useState(initialName ?? "");
  const [parent, setParent] = useState<ArchiveFolderWithPath | null>(initialParent ?? ROOT_OPTION);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset the form whenever the dialog re-opens so a previous failed attempt
  // doesn't bleed into the next session.
  useEffect(() => {
    if (open) {
      setName(initialName ?? "");
      setParent(initialParent ?? ROOT_OPTION);
      setError(null);
    }
  }, [open, initialName, initialParent]);

  const handleSubmit = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Naam is verplicht");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const resp = await api.createArchiveFolder({
        parentFolderId: parent?.id ?? 0,
        name: trimmed,
      });
      onCreated(resp.id);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Aanmaken mislukt");
    } finally {
      setSubmitting(false);
    }
  };

  const parentOptions = [ROOT_OPTION, ...folders];

  return (
    <Dialog open={open} onClose={submitting ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle>Nieuwe archiefmap</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <TextField
            label="Naam"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            size="small"
            fullWidth
            disabled={submitting}
            inputProps={{ maxLength: 100 }}
          />
          <Autocomplete
            options={parentOptions}
            value={parent}
            onChange={(_, v) => setParent(v)}
            getOptionLabel={(opt) => opt.path}
            isOptionEqualToValue={(opt, val) => opt.id === val.id}
            disabled={submitting}
            renderInput={(params) => (
              <TextField
                {...params}
                label="Bovenliggende map"
                size="small"
                helperText="Kies waar de nieuwe map onder komt te staan."
              />
            )}
          />
          {error && <Alert severity="error">{error}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>
          Annuleren
        </Button>
        <Button onClick={handleSubmit} variant="contained" disabled={submitting || !name.trim()}>
          Aanmaken
        </Button>
      </DialogActions>
    </Dialog>
  );
}
