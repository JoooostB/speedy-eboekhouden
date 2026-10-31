import { useLayoutEffect, useRef, useState } from "react";
import { Tooltip, Typography } from "@mui/material";

/**
 * Renders text truncated with an ellipsis, and wraps it in a tooltip
 * exposing the full string when (and only when) it actually overflows.
 * ResizeObserver keeps the overflow flag correct across viewport changes.
 * Shared by InboxRow and PaymentGroupRow.
 */
export function TruncatedText({
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
