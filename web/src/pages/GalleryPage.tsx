import { useCallback, useEffect, useRef, useState } from "react";
import { api, type PhotoEntry } from "../api";

const PAGE_SIZE = 30;

export function GalleryPage() {
  const [photos, setPhotos] = useState<PhotoEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [active, setActive] = useState<PhotoEntry | null>(null);
  const [hasMore, setHasMore] = useState(true);
  const pageRef = useRef(0);
  const sentinelRef = useRef<HTMLDivElement>(null);

  const load = useCallback(async (pageNum: number, append: boolean) => {
    if (!append) setLoading(true);
    else setLoadingMore(true);

    try {
      const res = await api.listPhotos(PAGE_SIZE, pageNum * PAGE_SIZE);
      if (append) {
        setPhotos((prev) => [...prev, ...res.photos]);
      } else {
        setPhotos(res.photos);
      }
      setHasMore((pageNum + 1) * PAGE_SIZE < res.total);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al cargar fotos");
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, []);

  // Initial load.
  useEffect(() => {
    load(0, false);
  }, [load]);

  // Infinite scroll via IntersectionObserver.
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el || !hasMore || loadingMore) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMore && !loadingMore) {
          pageRef.current += 1;
          load(pageRef.current, true);
        }
      },
      { rootMargin: "200px" }
    );

    observer.observe(el);
    return () => observer.disconnect();
  }, [hasMore, loadingMore, load]);

  return (
    <div className="page">
      <div className="page-header">
        <h2>Galería</h2>
        <div className="muted">{photos.length > 0 ? `${photos.length} fotos` : ""}</div>
      </div>

      {error && <div className="error">{error}</div>}

      {loading ? (
        <div className="empty">Cargando...</div>
      ) : photos.length === 0 ? (
        <div className="empty">
          <p>No hay fotos en tu nube.</p>
          <p className="muted">
            Sube imágenes desde la pestaña &ldquo;Archivos&rdquo; y aparecerán aquí.
          </p>
        </div>
      ) : (
        <>
          <div className="gallery-grid">
            {photos.map((p) => (
              <button
                key={p.id}
                className="gallery-tile"
                onClick={() => setActive(p)}
                title={p.name}
              >
                <img
                  src={api.thumbUrl(p.id, 256)}
                  alt={p.name}
                  loading="lazy"
                />
              </button>
            ))}
          </div>

          {/* Sentinel for infinite scroll */}
          <div ref={sentinelRef} className="gallery-sentinel">
            {loadingMore && <div className="spinner" />}
            {!hasMore && <p className="muted">Todas las fotos cargadas.</p>}
          </div>
        </>
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
