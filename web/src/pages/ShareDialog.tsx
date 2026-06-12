import { useState, useEffect } from "react";
import { api, ShareLink } from "../api";

interface ShareDialogProps {
  itemId: string;
  itemName: string;
  itemType: "file" | "folder";
  onClose: () => void;
}

export function ShareDialog({ itemId, itemName, itemType, onClose }: ShareDialogProps) {
  const [expireHours, setExpireHours] = useState(1);
  const [maxViews, setMaxViews] = useState(0);
  const [loading, setLoading] = useState(false);
  const [share, setShare] = useState<ShareLink | null>(null);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

  // Reset copied state after 2 seconds
  useEffect(() => {
    if (copied) {
      const t = setTimeout(() => setCopied(false), 2000);
      return () => clearTimeout(t);
    }
  }, [copied]);

  const createLink = async () => {
    setLoading(true);
    setError("");
    try {
      const opts: { file_id?: string; folder_id?: string; expires_in_hours: number; max_views: number } = {
        expires_in_hours: expireHours,
        max_views: maxViews,
      };
      if (itemType === "file") {
        opts.file_id = itemId;
      } else {
        opts.folder_id = itemId;
      }
      const result = await api.createShare(opts);
      setShare(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error creating share link");
    } finally {
      setLoading(false);
    }
  };

  const shareUrl = share
    ? `${window.location.origin}${share.url}`
    : "";

  const copyLink = () => {
    if (shareUrl) {
      navigator.clipboard.writeText(shareUrl).then(() => setCopied(true));
    }
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h3>Compartir: {itemName}</h3>
          <button className="modal-close" onClick={onClose}>&times;</button>
        </div>
        <div className="modal-body">
          {error && <div className="error">{error}</div>}

          {!share ? (
            <>
              <div className="form-group">
                <label>Expira en:</label>
                <select
                  value={expireHours}
                  onChange={(e) => setExpireHours(Number(e.target.value))}
                >
                  <option value={1}>1 hora</option>
                  <option value={6}>6 horas</option>
                  <option value={24}>1 día</option>
                  <option value={72}>3 días</option>
                  <option value={168}>1 semana</option>
                </select>
              </div>

              <div className="form-group">
                <label>Límite de vistas:</label>
                <select
                  value={maxViews}
                  onChange={(e) => setMaxViews(Number(e.target.value))}
                >
                  <option value={0}>Sin límite</option>
                  <option value={1}>1 vista</option>
                  <option value={5}>5 vistas</option>
                  <option value={10}>10 vistas</option>
                  <option value={50}>50 vistas</option>
                  <option value={100}>100 vistas</option>
                </select>
              </div>

              <button
                className="btn btn-primary"
                onClick={createLink}
                disabled={loading}
                style={{ width: "100%", marginTop: "1rem" }}
              >
                {loading ? "Creando..." : "Generar enlace"}
              </button>
            </>
          ) : (
            <div className="share-result">
              <p className="muted">Enlace compartible:</p>
              <div className="share-url-box">
                <input
                  type="text"
                  value={shareUrl}
                  readOnly
                  onClick={(e) => (e.target as HTMLInputElement).select()}
                />
                <button className="btn" onClick={copyLink}>
                  {copied ? "✓ Copiado" : "Copiar"}
                </button>
              </div>
              <div className="share-info">
                <span>Expira: {new Date(share.expires_at).toLocaleString()}</span>
                {share.max_views > 0 && (
                  <span>Máx. vistas: {share.max_views}</span>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
