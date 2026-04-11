import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { AuthProvider, useAuth } from "./context/AuthContext";
import { PasskeyLogin } from "./components/auth/PasskeyLogin";
import { PasskeyRecovery } from "./components/auth/PasskeyRecovery";
import { Layout } from "./components/Layout";
import { BookkeepingInbox } from "./components/inbox/BookkeepingInbox";
import { BulkEntryForm } from "./components/BulkEntryForm";
import { SettingsPage } from "./components/settings/SettingsPage";
import { Box, CircularProgress, Typography, Fade } from "@mui/material";

function AppContent() {
  const { user, checking } = useAuth();

  if (checking) {
    return (
      <Fade in timeout={200}>
        <Box
          sx={{
            display: "flex",
            flexDirection: "column",
            justifyContent: "center",
            alignItems: "center",
            minHeight: "100vh",
            bgcolor: "#0f172a",
          }}
          role="status"
          aria-live="polite"
        >
          <Typography
            variant="h5"
            sx={{ color: "white", fontWeight: 700, letterSpacing: "-0.02em", mb: 3 }}
          >
            Speedy e-Boekhouden
          </Typography>
          <CircularProgress
            size={28}
            thickness={3}
            sx={{ color: "rgba(255,255,255,0.5)" }}
            aria-label="Sessie wordt gecontroleerd"
          />
        </Box>
      </Fade>
    );
  }

  if (!user) {
    return (
      <Fade in timeout={300}>
        <Box>
          <Routes>
            <Route path="/herstel" element={<PasskeyRecovery />} />
            <Route path="*" element={<PasskeyLogin />} />
          </Routes>
        </Box>
      </Fade>
    );
  }

  return (
    <Fade in timeout={300}>
      <Box>
        <Layout>
          <Routes>
            <Route path="/" element={<BookkeepingInbox />} />
            <Route path="/uren" element={<BulkEntryForm />} />
            <Route path="/instellingen" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Layout>
      </Box>
    </Fade>
  );
}

export default function App() {
  return (
    <BrowserRouter basename="/app">
      <AuthProvider>
        <AppContent />
      </AuthProvider>
    </BrowserRouter>
  );
}
