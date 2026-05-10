import { useEffect, useState } from "react";
import { api, type PhotoEntry } from "../api";

export function GalleryPage() {
  const [photos, setPhotos] = useState<PhotoEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [active, setActive] = useState<PhotoEntry | null>(null);

  useEffect(() => {
    setLoading(true);
    api
      .listPhotos()
      .then((res) => setPhotos(res.photos))
      .catch((err) =>
        setError(err instanceof Error ? err.message : "Error al cargar fotos")
      )
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="page">
      <div className="page-header">
        <h2>Galería</h2>
        <div className="muted">{photos.length} fotos</div>
      </div>

      {error && <div className="error">{error}</div>}

      {loading ? (
        <div className="empty">Cargando...</div>
      ) : photos.length === 0 ? (
        <div className="empty">
          <p>No hay fotos en tu nube.</p>
          <p className="muted">
            Sube imágenes desde la pestaña "Archivos" y aparecerán aquí.
          </p>
        </div>
      ) : (
        <div className="gallery-grid">
          {photos.map((p) => (
            <button
              key={p.id}
              className="gallery-tile"
              onClick={() => setActive(p)}
              title={p.name}
            >
              <img src={api.thumbUrl(p.id, 256)} alt={p.name} loading="lazy" />
            </button>
          ))}
        </div>
      )}

      {active && (
        <div className="lightbox" onClick={() => setActive(null)}>
          <img
            src={api.fullUrl(active.id)}
            alt={active.name}
            onClick={(e) => e.stopPropagation()}
          />
          <div className="lightbox-caption">{active.name}</div>
          <button className="lightbox-close" onClick={() => setActive(null)}>
            Cerrar
          </button>
        </div>
      )}
    </div>
  );
}
