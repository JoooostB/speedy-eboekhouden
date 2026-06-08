import { useEffect, useState } from "react";
import { Autocomplete, Box, TextField, Typography } from "@mui/material";
import { useArchiveFolders, type ArchiveFolderWithPath } from "../../hooks/useArchiveFolders";
import { CreateArchiveFolderDialog } from "./CreateArchiveFolderDialog";

interface Props {
  value: ArchiveFolderWithPath | null;
  onChange: (folder: ArchiveFolderWithPath | null) => void;
  label?: string;
  disabled?: boolean;
  /** Helper text under the input; useful for explaining the opt-in nature. */
  helperText?: string;
}

// Sentinel value used as a special "create new" option in the autocomplete
// list. It is never passed to onChange — selecting it opens
// CreateArchiveFolderDialog instead.
const CREATE_NEW_SENTINEL: ArchiveFolderWithPath = {
  id: -1,
  naam: "__CREATE_NEW__",
  parentId: 0,
  isDeleted: false,
  path: "+ Nieuwe map aanmaken",
};

/**
 * ArchiveFolderPicker — choose a destination in the e-boekhouden digitaal
 * archief for the PDF that's about to be booked. Selection is optional; an
 * empty value means "do not upload to the archive".
 *
 * Shows folders with their full breadcrumb path so a flat Autocomplete dropdown
 * still conveys hierarchy ("Verwerkte facturen / Anthropic"). Deleted folders
 * are filtered out by the underlying hook. The last option in the dropdown
 * is a "+ Nieuwe map aanmaken" sentinel that opens CreateArchiveFolderDialog;
 * after the new folder is created the picker refetches and selects it.
 *
 * Accessibility:
 * - MUI Autocomplete provides the combobox ARIA pattern out of the box
 * - Loading and error states are conveyed visually and via the disabled prop
 * - The "create new" sentinel remains a standard listbox option for keyboard
 *   and screen reader users (no separate button)
 */
export function ArchiveFolderPicker({
  value,
  onChange,
  label = "Archiefmap",
  disabled,
  helperText,
}: Props) {
  const { folders, loading, error, refetch } = useArchiveFolders();
  const [createOpen, setCreateOpen] = useState(false);
  const [inputValue, setInputValue] = useState("");
  // Set after a successful create to "claim" the new folder once the
  // refetched list arrives. Refetching is async and useState updates only
  // appear on the next render, so we can't synchronously read the new
  // folder out of `folders` right after refetch resolves — this pending-id
  // pattern bridges that gap.
  const [pendingFolderId, setPendingFolderId] = useState<number | null>(null);

  useEffect(() => {
    if (pendingFolderId == null) return;
    const found = folders.find((f) => f.id === pendingFolderId);
    if (found) {
      onChange(found);
      setPendingFolderId(null);
    }
  }, [folders, pendingFolderId, onChange]);

  // Append the "create new" sentinel to the end of the options list.
  const optionsWithCreate = [...folders, CREATE_NEW_SENTINEL];

  // Suggested parent for the new folder: parent of the currently selected
  // folder (sibling creation), or null (= root) when nothing is selected.
  // Most users organise per-supplier under one container; defaulting to a
  // sibling matches that workflow.
  const suggestedParent =
    value && value.parentId !== 0
      ? folders.find((f) => f.id === value.parentId) ?? null
      : null;

  return (
    <>
      <Autocomplete
        options={optionsWithCreate}
        value={value}
        onChange={(_, v) => {
          if (v && v.id === CREATE_NEW_SENTINEL.id) {
            setCreateOpen(true);
            return;
          }
          onChange(v);
        }}
        onInputChange={(_, v) => setInputValue(v)}
        getOptionLabel={(opt) => opt.path}
        isOptionEqualToValue={(opt, val) => opt.id === val.id}
        loading={loading}
        disabled={disabled || !!error}
        noOptionsText={error ?? "Geen mappen gevonden"}
        renderOption={(props, option) => {
          if (option.id === CREATE_NEW_SENTINEL.id) {
            return (
              <li {...props} key="__create_new__">
                <Box sx={{ display: "flex", alignItems: "center", gap: 1, color: "primary.main" }}>
                  <Box
                    component="svg"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                    sx={{ width: 16, height: 16, flexShrink: 0 }}
                  >
                    <line x1="12" y1="5" x2="12" y2="19" />
                    <line x1="5" y1="12" x2="19" y2="12" />
                  </Box>
                  <Typography variant="body2" fontWeight={600} color="primary">
                    Nieuwe map aanmaken
                  </Typography>
                </Box>
              </li>
            );
          }
          return (
            <li {...props} key={option.id}>
              <Box>
                <Typography variant="body2">{option.path}</Typography>
              </Box>
            </li>
          );
        }}
        renderInput={(params) => (
          <TextField
            {...params}
            label={label}
            size="small"
            helperText={error ?? helperText}
            error={!!error}
          />
        )}
        size="small"
      />

      <CreateArchiveFolderDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(newId) => {
          // Mark the new folder as the one to auto-select; the effect above
          // will pick it up once refetch has populated the hook's state.
          setPendingFolderId(newId);
          void refetch();
        }}
        folders={folders}
        initialName={inputValue}
        initialParent={suggestedParent}
      />
    </>
  );
}
