import { Navigate, NavLink, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { useAI } from "./aiContext";
import { LoginPage } from "./pages/LoginPage";
import { FilesPage } from "./pages/FilesPage";
import { GalleryPage } from "./pages/GalleryPage";
import { SearchPage } from "./pages/SearchPage";

export default function App() {
  const { user, loading, logout } = useAuth();
  const ai = useAI();

  if (loading) {
    return (
      <div className="splash">
        <div className="spinner" />
      </div>
    );
  }

  if (!user) {
    return (
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    );
  }

  return (
    <div className="app">
      <header className="app-header">
        <div className="brand">
          <span className="brand-mark">/</span>
          <span>My Personal Cloud</span>
        </div>
        <nav className="app-nav">
          <NavLink to="/files" className={({ isActive }) => (isActive ? "active" : "")}>
            Archivos
          </NavLink>
          <NavLink to="/gallery" className={({ isActive }) => (isActive ? "active" : "")}>
            Galería
          </NavLink>
          {ai?.enabled && (
            <NavLink to="/search" className={({ isActive }) => (isActive ? "active" : "")}>
              Buscar IA
            </NavLink>
          )}
        </nav>
        <div className="user-block">
          <span className="user-name">{user.username}</span>
          <button className="btn btn-ghost" onClick={logout}>
            Salir
          </button>
        </div>
      </header>
      <main className="app-main">
        <Routes>
          <Route path="/files" element={<FilesPage />} />
          <Route path="/gallery" element={<GalleryPage />} />
          <Route path="/search" element={<SearchPage />} />
          <Route path="*" element={<Navigate to="/files" replace />} />
        </Routes>
      </main>
    </div>
  );
}
