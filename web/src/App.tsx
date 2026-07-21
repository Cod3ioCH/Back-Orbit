import { Navigate, Route, Routes } from "react-router-dom";
import { RequireAuth } from "@/components/layout/RequireAuth";
import { AppShell } from "@/components/layout/AppShell";
import { ComingSoon } from "@/components/ComingSoon";
import { SetupPage } from "@/pages/SetupPage";
import { LoginPage } from "@/pages/LoginPage";
import { OverviewPage } from "@/pages/OverviewPage";
import { ProjectsPage } from "@/pages/ProjectsPage";
import { ProjectDetailPage } from "@/pages/ProjectDetailPage";
import { ActivityPage } from "@/pages/ActivityPage";
import { RepositoriesPage } from "@/pages/RepositoriesPage";
import { RestorePage } from "@/pages/RestorePage";
import { SnapshotsPage } from "@/pages/SnapshotsPage";

export default function App() {
  return (
    <Routes>
      <Route path="/setup" element={<SetupPage />} />
      <Route path="/login" element={<LoginPage />} />

      <Route element={<RequireAuth />}>
        <Route element={<AppShell />}>
          <Route index element={<OverviewPage />} />
          <Route path="projects" element={<ProjectsPage />} />
          <Route path="projects/:id" element={<ProjectDetailPage />} />
          <Route
            path="plans"
            element={
              <ComingSoon
                title="Backup Plans"
                description="Scheduling, retention, and hook configuration for backup plans arrive in a later phase."
              />
            }
          />
          <Route path="snapshots" element={<SnapshotsPage />} />
          <Route path="restore" element={<RestorePage />} />
          <Route path="repositories" element={<RepositoriesPage />} />
          <Route path="activity" element={<ActivityPage />} />
          <Route
            path="secrets"
            element={
              <ComingSoon
                title="Secrets"
                description="The encrypted secret store arrives in a later phase."
              />
            }
          />
          <Route
            path="alerts"
            element={
              <ComingSoon
                title="Alerts"
                description="Monitoring, health scores, and alert configuration arrive in a later phase."
              />
            }
          />
          <Route
            path="settings"
            element={
              <ComingSoon
                title="Settings"
                description="System-wide configuration arrives in a later phase."
              />
            }
          />
        </Route>
      </Route>

      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
