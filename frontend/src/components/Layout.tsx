import type { ReactNode } from "react";
import { AppBar, Toolbar, Typography, Button, Container, Box } from "@mui/material";
import LogoutIcon from "@mui/icons-material/Logout";
import { useAuth } from "../context/AuthContext";
import { track } from "../analytics";

interface Props {
  children: ReactNode;
}

export function Layout({ children }: Props) {
  const { logout } = useAuth();

  return (
    <Box sx={{ minHeight: "100vh", bgcolor: "grey.50" }}>
      <AppBar position="static">
        <Toolbar>
          <Typography variant="h6" sx={{ flexGrow: 1 }}>
            Speedy e-Boekhouden
          </Typography>
          <Button color="inherit" startIcon={<LogoutIcon />} onClick={() => { track("Logout"); logout(); }}>
            Uitloggen
          </Button>
        </Toolbar>
      </AppBar>
      <Container maxWidth="md" sx={{ py: 4 }}>
        {children}
      </Container>
    </Box>
  );
}
