import { useState, type FormEvent } from "react";
import { useAuth } from "../auth";

export function LoginPage() {
  const { login } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await login(username, password);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al iniciar sesión");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-screen">
      <form className="login-card" onSubmit={onSubmit}>
        <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 4 }}>
          <span className="brand-mark" style={{ width: 40, height: 40, fontSize: 18 }}>~</span>
          <div>
            <h1 style={{ margin: 0, fontSize: 20 }}>My Cloud</h1>
            <p style={{ margin: "2px 0 0", fontSize: 13, color: "var(--muted)" }}>
              Tus archivos. Tu hardware. Tu nube.
            </p>
          </div>
        </div>

        <label className="field">
          <span>Usuario</span>
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Ingresa tu usuario"
            autoFocus
            required
          />
        </label>

        <label className="field">
          <span>Contraseña</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Ingresa tu contraseña"
            required
          />
        </label>

        {error && <div className="error">{error}</div>}

        <button type="submit" className="btn btn-primary" disabled={busy} style={{ marginTop: 4 }}>
          {busy ? (
            <span style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span className="spinner" style={{ width: 16, height: 16, borderWidth: 2 }} />
              Entrando...
            </span>
          ) : (
            "Entrar"
          )}
        </button>
      </form>
    </div>
  );
}
