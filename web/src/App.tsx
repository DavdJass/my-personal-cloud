import { Navigate, NavLink, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { LoginPage } from "./pages/LoginPage";
import { FilesPage } from "./pages/FilesPage";
import { GalleryPage } from "./pages/GalleryPage";
import { SharedPage } from "./pages/SharedPage";
import { SharesPage } from "./pages/SharesPage";
import { BackupPage } from "./pages/BackupPage";
import { useTheme } from "./theme";
import { ToastProvider } from "./toast";

export default function App() {
  return (
    <ToastProvider>
      <AppInner />
    </ToastProvider>
  );
}

function AppRoutes() {
  const { user, logout } = useAuth();
  const { toggle, isDark } = useTheme();

  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="app">
      <header className="app-header">
        <div className="brand">
          <span className="brand-mark">~</span>
          <span>My Cloud</span>
        </div>
        <nav className="app-nav">
          <NavLink to="/files" className={({ isActive }) => (isActive ? "active" : "")}>
            Archivos
          </NavLink>
          <NavLink to="/gallery" className={({ isActive }) => (isActive ? "active" : "")}>
            Galería
          </NavLink>
          <NavLink to="/shares" className={({ isActive }) => (isActive ? "active" : "")}>
            Enlaces
          </NavLink>
          <NavLink to="/backup" className={({ isActive }) => (isActive ? "active" : "")}>
            Backup
          </NavLink>
        </nav>
        <div className="user-block">
          <button
            className="btn btn-ghost btn-sm theme-toggle"
            onClick={toggle}
            title={isDark ? "Activar modo claro" : "Activar modo oscuro"}
          >
            {isDark ? "\u2600\uFE0F" : "\u{1F319}"}
          </button>
          <span className="user-avatar">{user.username.charAt(0).toUpperCase()}</span>
          <span className="user-name">{user.username}</span>
          <button className="btn btn-ghost btn-sm" onClick={logout}>
            Salir
          </button>
        </div>
      </header>
      <main className="app-main">
        <Routes>
          <Route path="/files" element={<FilesPage />} />
          <Route path="/gallery" element={<GalleryPage />} />
          <Route path="/shares" element={<SharesPage />} />
          <Route path="/backup" element={<BackupPage />} />
          <Route path="*" element={<Navigate to="/files" replace />} />
        </Routes>
      </main>
    </div>
  );
}

function AppInner() {
  const { loading } = useAuth();

  if (loading) {
    return (
      <div className="splash">
        <div className="spinner" />
      </div>
    );
  }

  return (
    <Routes>
      {/*
        Share page renders standalone (no app chrome) so it works
        whether the user is logged in or not.
      */}
      <Route path="/share/:token" element={<SharedPage />} />

      {/*
        Login page — redirect to files if already authenticated.
      */}
      <Route
        path="/login"
        element={
          <ProtectedRoute redirectTo="/files" inverted>
            <LoginPage />
          </ProtectedRoute>
        }
      />

      {/*
        Everything else: app chrome if authenticated, else redirect to login.
      */}
      <Route path="/*" element={<AppRoutes />} />
    </Routes>
  );
}

/** Wraps children with auth guard.
 *  - normal mode: redirects to `redirectTo` if NOT authenticated.
 *  - inverted mode (for login): redirects if IS authenticated.
 */
function ProtectedRoute({
  children,
  redirectTo,
  inverted,
}: {
  children: React.ReactNode;
  redirectTo: string;
  inverted?: boolean;
}) {
  const { user } = useAuth();
  const authed = !!user;

  if (inverted ? authed : !authed) {
    return <Navigate to={redirectTo} replace />;
  }

  return <>{children}</>;
}
