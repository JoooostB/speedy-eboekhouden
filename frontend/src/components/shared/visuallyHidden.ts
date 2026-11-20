/**
 * Hidden on screen, still read by screen readers: for live regions and
 * other text that only assistive technology needs.
 *
 * The sizes are strings on purpose. In MUI's `sx` a bare `width: 1` means
 * 100%, so `{ position: "absolute", width: 1, height: 1 }` is an invisible
 * box as large as its positioned ancestor. Inside a dialog that box sticks
 * out of the paper and gives it scrollbars and an empty strip below the
 * actions. `clip` hides the box but does not take it out of the scroll area.
 */
export const visuallyHidden = {
  position: "absolute",
  width: "1px",
  height: "1px",
  margin: "-1px",
  padding: 0,
  border: 0,
  overflow: "hidden",
  clip: "rect(0 0 0 0)",
  whiteSpace: "nowrap",
} as const;
