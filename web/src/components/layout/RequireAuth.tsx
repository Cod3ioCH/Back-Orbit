import { Navigate, Outlet } from "react-router-dom";
import { useAuth } from "@/lib/auth-context";
import { FullPageSpinner } from "@/components/layout/FullPageSpinner";

// RequireAuth gates the authenticated part of the app: it waits for the
// setup/session queries to resolve, then routes the user to whichever step
// they're missing (initial setup, login) before rendering protected routes.
export function RequireAuth() {
  const { isLoading, setupComplete, user } = useAuth();

  if (isLoading) {
    return <FullPageSpinner />;
  }
  if (setupComplete === false) {
    return <Navigate to="/setup" replace />;
  }
  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return <Outlet />;
}
