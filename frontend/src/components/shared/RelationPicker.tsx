import { useState, useEffect, useCallback } from "react";
import { Autocomplete, Box, IconButton, TextField, Tooltip, Typography } from "@mui/material";
import { api } from "../../api/client";
import { CreateRelationDialog } from "./CreateRelationDialog";
import type { Relation } from "../../api/types";

/**
 * Sentinel value used as a special "create new" option in the autocomplete list.
 * It is never passed to onChange — selecting it opens the CreateRelationDialog instead.
 */
const CREATE_NEW_SENTINEL: Relation = {
  id: -1,
  code: "__CREATE_NEW__",
  bedrijf: "Nieuwe relatie aanmaken",
  grootboekrekeningId: 0,
  iban: "",
};

interface Props {
  value: Relation | null;
  onChange: (relation: Relation | null) => void;
  label?: string;
  disabled?: boolean;
  /** Passed to CreateRelationDialog as the grootboekrekeningId for the new relation */
  grootboekrekeningId?: number;
}

/**
 * RelationPicker — autocomplete for searching existing relations, with an
 * inline option to create a new one via CreateRelationDialog.
 *
 * Accessibility:
 * - Uses MUI Autocomplete which provides combobox ARIA pattern out of the box
 * - The "create new" option is visually distinct (primary color, plus icon)
 *   but remains a standard listbox option for keyboard/screen reader users
 * - CreateRelationDialog handles its own focus management
 */
export function RelationPicker({
  value,
  onChange,
  label = "Relatie",
  disabled,
  grootboekrekeningId,
}: Props) {
  const [options, setOptions] = useState<Relation[]>([]);
  const [inputValue, setInputValue] = useState("");
  const [loading, setLoading] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);

  const search = useCallback(async (query: string) => {
    setLoading(true);
    try {
      const results = await api.searchRelations(query);
      setOptions(results);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (inputValue.length < 2) {
      setOptions([]);
      return;
    }
    const timer = setTimeout(() => search(inputValue), 300);
    return () => clearTimeout(timer);
  }, [inputValue, search]);

  /** Append the "create new" sentinel to the end of options */
  const optionsWithCreate = [...options, CREATE_NEW_SENTINEL];

  return (
    <>
      <Autocomplete
        options={optionsWithCreate}
        value={value}
        onChange={(_, v) => {
          // Intercept the sentinel — open the dialog instead of selecting it
          if (v && v.id === CREATE_NEW_SENTINEL.id) {
            setCreateOpen(true);
            return;
          }
          onChange(v);
        }}
        onInputChange={(_, v) => setInputValue(v)}
        getOptionLabel={(opt) =>
          opt.id === CREATE_NEW_SENTINEL.id ? opt.bedrijf : `${opt.code} - ${opt.bedrijf}`
        }
        isOptionEqualToValue={(opt, val) => opt.id === val.id}
        loading={loading}
        renderOption={(props, option) => {
          if (option.id === CREATE_NEW_SENTINEL.id) {
            return (
              <li {...props} key="__create_new__">
                <Box sx={{ display: "flex", alignItems: "center", gap: 1, color: "primary.main" }}>
                  {/* Plus icon — inline SVG */}
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
                    Nieuwe relatie aanmaken
                  </Typography>
                </Box>
              </li>
            );
          }
          return (
            <li {...props} key={option.id}>
              <Box>
                <Typography variant="body2">
                  {option.code} - {option.bedrijf}
                </Typography>
              </Box>
            </li>
          );
        }}
        renderInput={(params) => (
          <TextField
            {...params}
            label={label}
            size="small"
            slotProps={{
              input: {
                ...params.InputProps,
                endAdornment: (
                  <>
                    {!value && !disabled && (
                      <Tooltip title="Nieuwe relatie aanmaken">
                        <IconButton
                          size="small"
                          onClick={(e) => {
                            e.stopPropagation();
                            setCreateOpen(true);
                          }}
                          aria-label="Nieuwe relatie aanmaken"
                          sx={{ p: 0.5, mr: -0.5 }}
                        >
                          <Box
                            component="svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                            strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
                            aria-hidden="true" sx={{ width: 16, height: 16, color: "primary.main" }}
                          >
                            <line x1="12" y1="5" x2="12" y2="19" />
                            <line x1="5" y1="12" x2="19" y2="12" />
                          </Box>
                        </IconButton>
                      </Tooltip>
                    )}
                    {params.InputProps.endAdornment}
                  </>
                ),
              },
            }}
          />
        )}
        disabled={disabled}
        filterOptions={(x) => x}
        size="small"
      />

      <CreateRelationDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(relation) => {
          onChange(relation);
          setCreateOpen(false);
        }}
        grootboekrekeningId={grootboekrekeningId}
        initialSearch={inputValue}
      />
    </>
  );
}
