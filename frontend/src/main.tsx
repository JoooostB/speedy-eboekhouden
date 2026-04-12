import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { CssBaseline, ThemeProvider, createTheme } from "@mui/material";
import App from "./App";

/**
 * Theme tokens — matched to landing page CSS custom properties.
 * See landing/style.css :root for the source of truth.
 */
const theme = createTheme({
  palette: {
    primary: {
      main: "#1565c0",
      dark: "#0d47a1",
      light: "#e3f2fd",
      contrastText: "#ffffff",
    },
    secondary: {
      main: "#b34700",
      dark: "#8a3700",
      contrastText: "#ffffff",
    },
    background: {
      default: "#f8fafc",
      paper: "#ffffff",
    },
    text: {
      primary: "#1a1a2e",
      secondary: "#546e7a",
    },
    success: {
      main: "#2e7d32",
      light: "#e8f5e9",
    },
    error: {
      main: "#c62828",
    },
  },
  typography: {
    fontFamily:
      '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    h4: { fontWeight: 700, letterSpacing: "-0.02em" },
    h5: { fontWeight: 700, letterSpacing: "-0.01em" },
    h6: { fontWeight: 600 },
    button: { textTransform: "none", fontWeight: 600 },
  },
  shape: {
    borderRadius: 12,
  },
  components: {
    MuiButton: {
      styleOverrides: {
        root: {
          borderRadius: 8,
          padding: "10px 24px",
        },
      },
    },
    MuiPaper: {
      styleOverrides: {
        root: {
          borderRadius: 12,
        },
      },
    },
    // AppBar inherits from Paper so the global 12px radius above would give
    // the top navigation rounded corners — including on mobile where it sits
    // flush against the status bar, producing a "floating pill" look that
    // reveals the page background between the AppBar and the device notch.
    // defaultProps: { square: true } tells MUI to skip the radius inheritance.
    MuiAppBar: {
      defaultProps: {
        square: true,
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          borderRadius: 8,
        },
      },
    },
    MuiChip: {
      styleOverrides: {
        root: {
          borderRadius: 8,
        },
      },
    },
    MuiDialog: {
      styleOverrides: {
        paper: {
          borderRadius: 16,
        },
      },
    },
    // Paint the html element with the primary blue so iOS rubber-band
    // overscroll at the top of the page reveals the brand color rather
    // than white. body keeps the app background. The AppBar's own color
    // covers the actual visible header.
    MuiCssBaseline: {
      styleOverrides: {
        html: { backgroundColor: "#1565c0" },
        body: { backgroundColor: "#f8fafc" },
      },
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <App />
    </ThemeProvider>
  </StrictMode>,
);
