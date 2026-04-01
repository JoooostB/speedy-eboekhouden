import { AuthProvider, useAuth } from "./context/AuthContext";
import { LoginForm } from "./components/LoginForm";
import { MFADialog } from "./components/MFADialog";
import { Layout } from "./components/Layout";
import { BulkEntryForm } from "./components/BulkEntryForm";
import { Box, CircularProgress } from "@mui/material";

function AppContent() {
  const { isAuthenticated, needsMFA, checking } = useAuth();

  if (checking) {
    return (
      <Box
        sx={{
          display: "flex",
          justifyContent: "center",
          alignItems: "center",
          minHeight: "100vh",
        }}
      >
        <CircularProgress />
      </Box>
    );
  }

  if (needsMFA) {
    return (
      <>
        <LoginForm />
        <MFADialog />
      </>
    );
  }

  if (!isAuthenticated) {
    return <LoginForm />;
  }

  return (
    <Layout>
      <BulkEntryForm />
    </Layout>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  );
}
