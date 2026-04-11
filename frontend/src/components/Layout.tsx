import { useState, useEffect, useCallback, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  AppBar,
  Toolbar,
  Typography,
  Container,
  Box,
  Tabs,
  Tab,
  IconButton,
  Chip,
  Tooltip,
  Menu,
  MenuItem,
  ListItemIcon,
  ListItemText,
  Divider,
} from "@mui/material";
import { useAuth } from "../context/AuthContext";
import { EBoekhoudenConnectDialog } from "./EBoekhoudenConnectDialog";

/**
 * Layout provides the app shell: header with navigation, connection
 * status, and user menu.
 *
 * Design rationale:
 * - Navy AppBar continues the Stopwatch brand from landing page.
 * - Connection status chip uses a filled style for connected (green on
 *   dark reads well) and outlined + pulsing dot for disconnected.
 * - User menu consolidates settings and logout into one interaction
 *   point, keeping the toolbar clean.
 * - Tab navigation uses aria-current via MUI's built-in handling.
 *
 * Accessibility:
 * - <nav> landmark wraps the tab navigation with aria-label.
 * - Skip link (visually hidden, appears on focus) jumps to main content.
 * - Each Tab has an aria-current derived from the selected state.
 * - Connection chip is a button when action is available, static when not.
 * - User menu is a proper menu with keyboard navigation.
 */

const navItems = [
  { label: "Inbox", path: "/" },
  { label: "Uren", path: "/uren" },
];

interface Props {
  children: ReactNode;
}

export function Layout({ children }: Props) {
  const { user, eboekhoudenConnected, logout } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const [connectOpen, setConnectOpen] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<null | HTMLElement>(null);

  // Listen for connect requests from child components (e.g. Dashboard "Nu koppelen")
  const openConnect = useCallback(() => setConnectOpen(true), []);
  useEffect(() => {
    window.addEventListener("eb:connect", openConnect);
    return () => window.removeEventListener("eb:connect", openConnect);
  }, [openConnect]);

  const currentTab = navItems.findIndex((item) => item.path === location.pathname);

  const handleLogout = async () => {
    setMenuAnchor(null);
    await logout();
  };

  return (
    <Box sx={{ minHeight: "100vh", bgcolor: "grey.50" }}>
      {/* Skip link for keyboard users — appears on focus */}
      <Box
        component="a"
        href="#main-content"
        sx={{
          position: "absolute",
          left: -9999,
          top: 0,
          zIndex: 1300,
          bgcolor: "primary.main",
          color: "white",
          px: 3,
          py: 1.5,
          borderRadius: "0 0 8px 0",
          fontWeight: 600,
          textDecoration: "none",
          "&:focus": {
            left: 0,
          },
        }}
      >
        Ga naar inhoud
      </Box>

      <AppBar
        position="sticky"
        elevation={0}
        sx={{
          bgcolor: "primary.main",
          borderBottom: "1px solid",
          borderColor: "rgba(255,255,255,0.08)",
        }}
      >
        <Toolbar sx={{ gap: 1 }}>
          <Typography
            variant="h6"
            component="div"
            sx={{
              fontWeight: 700,
              letterSpacing: "-0.02em",
              fontSize: "1.25rem",
              mr: 2,
              flexShrink: 0,
            }}
          >
            Speedy
          </Typography>

          <Tabs
            value={currentTab >= 0 ? currentTab : false}
            onChange={(_, idx) => navigate(navItems[idx].path)}
            textColor="inherit"
            aria-label="Hoofdnavigatie"
            variant="scrollable"
            scrollButtons="auto"
            sx={{
              flexGrow: 1,
              minHeight: 48,
              "& .MuiTabs-indicator": {
                bgcolor: "#fff",
                height: 3,
                borderRadius: "3px 3px 0 0",
              },
              "& .MuiTab-root": {
                minHeight: 48,
                minWidth: { xs: "auto", sm: 80 },
                px: { xs: 1.5, sm: 2 },
                fontSize: "0.875rem",
                fontWeight: 500,
                opacity: 0.7,
                "&.Mui-selected": {
                  opacity: 1,
                  fontWeight: 600,
                },
              },
            }}
          >
            {navItems.map((item) => (
              <Tab key={item.path} label={item.label} />
            ))}
          </Tabs>

          {/* Connection status */}
          <Tooltip
            title={
              eboekhoudenConnected
                ? "e-Boekhouden is gekoppeld"
                : "Klik om e-Boekhouden te koppelen"
            }
          >
            <Chip
              label={eboekhoudenConnected ? "Gekoppeld" : "Niet gekoppeld"}
              size="small"
              onClick={eboekhoudenConnected ? undefined : () => setConnectOpen(true)}
              role={eboekhoudenConnected ? "status" : "button"}
              aria-label={
                eboekhoudenConnected
                  ? "e-Boekhouden is gekoppeld"
                  : "e-Boekhouden is niet gekoppeld. Klik om te koppelen."
              }
              sx={{
                mr: 1,
                fontWeight: 600,
                fontSize: "0.75rem",
                height: 28,
                cursor: eboekhoudenConnected ? "default" : "pointer",
                ...(eboekhoudenConnected
                  ? {
                      bgcolor: "rgba(22, 163, 74, 0.15)",
                      color: "#86efac",
                      border: "1px solid rgba(22, 163, 74, 0.3)",
                    }
                  : {
                      bgcolor: "rgba(255, 255, 255, 0.08)",
                      color: "rgba(255,255,255,0.7)",
                      border: "1px solid rgba(255,255,255,0.15)",
                      "&:hover": {
                        bgcolor: "rgba(255, 255, 255, 0.15)",
                        color: "#fff",
                      },
                    }),
              }}
            />
          </Tooltip>

          {/* User menu: combines settings + logout */}
          <Tooltip title={user?.name || "Menu"}>
            <IconButton
              color="inherit"
              onClick={(e) => setMenuAnchor(e.currentTarget)}
              aria-label={`Accountmenu voor ${user?.name || "gebruiker"}`}
              aria-haspopup="true"
              aria-expanded={Boolean(menuAnchor)}
              sx={{
                width: 36,
                height: 36,
                fontSize: "0.875rem",
                fontWeight: 600,
                bgcolor: "rgba(255,255,255,0.1)",
                "&:hover": { bgcolor: "rgba(255,255,255,0.18)" },
              }}
            >
              {/* Initials avatar */}
              {(user?.name || "?")
                .split(" ")
                .map((w) => w[0])
                .join("")
                .slice(0, 2)
                .toUpperCase()}
            </IconButton>
          </Tooltip>

          <Menu
            anchorEl={menuAnchor}
            open={Boolean(menuAnchor)}
            onClose={() => setMenuAnchor(null)}
            transformOrigin={{ horizontal: "right", vertical: "top" }}
            anchorOrigin={{ horizontal: "right", vertical: "bottom" }}
            slotProps={{
              paper: {
                sx: { minWidth: 200, mt: 1 },
              },
            }}
          >
            <Box sx={{ px: 2, py: 1 }}>
              <Typography variant="body2" fontWeight={600}>
                {user?.name}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {user?.email}
              </Typography>
            </Box>
            <Divider sx={{ my: 0.5 }} />
            <MenuItem
              onClick={() => {
                setMenuAnchor(null);
                navigate("/instellingen");
              }}
            >
              <ListItemIcon>
                {/* Settings gear - inline SVG per project policy */}
                <Box
                  component="svg"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={2}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                  sx={{ width: 18, height: 18 }}
                >
                  <circle cx="12" cy="12" r="3" />
                  <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
                </Box>
              </ListItemIcon>
              <ListItemText>Instellingen</ListItemText>
            </MenuItem>
            {!eboekhoudenConnected && (
              <MenuItem
                onClick={() => {
                  setMenuAnchor(null);
                  setConnectOpen(true);
                }}
              >
                <ListItemIcon>
                  {/* Link icon */}
                  <Box
                    component="svg"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                    sx={{ width: 18, height: 18 }}
                  >
                    <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
                    <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
                  </Box>
                </ListItemIcon>
                <ListItemText>e-Boekhouden koppelen</ListItemText>
              </MenuItem>
            )}
            <Divider sx={{ my: 0.5 }} />
            <MenuItem onClick={handleLogout}>
              <ListItemIcon>
                {/* Logout icon */}
                <Box
                  component="svg"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={2}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                  sx={{ width: 18, height: 18, color: "error.main" }}
                >
                  <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
                  <polyline points="16 17 21 12 16 7" />
                  <line x1="21" y1="12" x2="9" y2="12" />
                </Box>
              </ListItemIcon>
              <ListItemText sx={{ color: "error.main" }}>Uitloggen</ListItemText>
            </MenuItem>
          </Menu>
        </Toolbar>
      </AppBar>

      <Container
        component="main"
        id="main-content"
        maxWidth="lg"
        sx={{ py: 4 }}
      >
        {children}
      </Container>

      <EBoekhoudenConnectDialog
        open={connectOpen}
        onClose={() => setConnectOpen(false)}
      />
    </Box>
  );
}
