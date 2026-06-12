import { useEffect, useState } from "react";
import { api, ShareLink } from "../api";

export function SharesPage() {
  const [shares, setShares] = useState<ShareLink[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [copiedId, setCopiedId] = useState<string | null>(null);

  useEffect(() => {
    loadShares();
  }, []);

  useEffect(() => {
    if (copiedId) {
      const t = setTimeout(() => setCopiedId(null), 2000);
      return () => clearTimeout(t);
    }
  }, [copiedId]);

  const loadShares = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await api.listShares();
      setShares(res.shares);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error loading shares");
    } finally {
      setLoading(false);
    }
  };

  const copyLink = (url: string, id: string) => {
    const fullUrl = `${window.location.origin}${url}`;
    navigator.clipboard.writeText(fullUrl).then(() => setCopiedId(id));
  };

  const revokeShare = async (id: string) => {
    if (!confirm("¿Revocar este enlace? Ya no será accesible.")) return;
    try {
      await api.revokeShare(id);
      setShares((prev) => prev.filter((s) => s.id !== id));
    } catch (err) {
      alert(err instanceof Error ? err.message : "Error revoking share");
    }
  };

  const formatDate = (iso: string) => {
    return new Date(iso).toLocaleString();
  };

  const isExpired = (share: ShareLink) => {
    return new Date(share.expires_at) < new Date();
  };

  return (
    <div className="page">
      <div className="page-header">
        <h2>Enlaces compartidos</h2>
        <span className="muted">{shares.length} enlace(s)</span>
      </div>

      {error && <div className="error">{error}</div>}

      {loading ? (
        <div className="loading">Cargando...</div>
      ) : shares.length === 0 ? (
        <div className="empty-state">
          <p className="muted">No hay enlaces compartidos.</p>
          <p className="muted">
            Ve a Archivos, haz clic en un archivo o carpeta y selecciona "Compartir".
          </p>
        </div>
      ) : (
        <div className="shares-list">
          {shares.map((share) => (
            <div
              key={share.id}
              className={`share-card ${isExpired(share) || !share.is_active ? "share-expired" : ""}`}
            >
              <div className="share-card-header">
                <span className="share-name">{share.name || "Sin nombre"}</span>
                <span className={`share-badge ${share.is_active ? "active" : "revoked"}`}>
                  {!share.is_active ? "Revocado" : isExpired(share) ? "Expirado" : "Activo"}
                </span>
              </div>
              <div className="share-card-meta">
                <span>Creado: {formatDate(share.created_at)}</span>
                <span>Expira: {formatDate(share.expires_at)}</span>
                {share.max_views > 0 && (
                  <span>Vistas: {share.current_views}/{share.max_views}</span>
                )}
                {share.max_views === 0 && (
                  <span>Vistas: {share.current_views} (sin límite)</span>
                )}
              </div>
              <div className="share-card-actions">
                <button
                  className="btn btn-sm"
                  onClick={() => copyLink(share.url, share.id)}
                >
                  {copiedId === share.id ? "✓ Copiado" : "Copiar enlace"}
                </button>
                {share.is_active && !isExpired(share) && (
                  <button
                    className="btn btn-sm btn-danger"
                    onClick={() => revokeShare(share.id)}
                  >
                    Revocar
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
