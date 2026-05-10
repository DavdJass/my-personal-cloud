import { useEffect, useState, type FormEvent } from "react";
import { api, type AISearchResult, type AIStatus } from "../api";

export function SearchPage() {
  const [status, setStatus] = useState<AIStatus | null>(null);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<AISearchResult[]>([]);
  const [searched, setSearched] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .aiStatus()
      .then(setStatus)
      .catch(() => setStatus({ enabled: false, model: "" }));
  }, []);

  async function onSearch(e: FormEvent) {
    e.preventDefault();
    if (!query.trim()) return;
    setLoading(true);
    setError(null);
    setSearched(true);
    try {
      const res = await api.aiSearch(query.trim());
      setResults(res.results);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error en la búsqueda");
      setResults([]);
    } finally {
      setLoading(false);
    }
  }

  if (!status) {
    return (
      <div className="page">
        <div className="empty">Comprobando disponibilidad de la IA...</div>
      </div>
    );
  }

  if (!status.enabled) {
    return (
      <div className="page">
        <div className="page-header">
          <h2>Búsqueda con IA</h2>
        </div>
        <div className="empty">
          <p>La búsqueda con IA no está activada.</p>
          <p className="muted">
            Define la variable de entorno <code>DEEPSEEK_API_KEY</code> y
            reinicia el servidor para habilitar esta función.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="page">
      <div className="page-header">
        <h2>Búsqueda con IA</h2>
        <div className="muted">Modelo: {status.model}</div>
      </div>

      <form className="search-form" onSubmit={onSearch}>
        <input
          className="search-input"
          type="text"
          placeholder="Describe lo que buscas, por ejemplo: 'fotos de la playa' o 'la factura de luz'..."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          autoFocus
        />
        <button className="btn btn-primary" type="submit" disabled={loading}>
          {loading ? "Buscando..." : "Buscar"}
        </button>
      </form>

      {error && <div className="error">{error}</div>}

      {!searched ? (
        <div className="empty">
          <p>Escribe lo que quieres encontrar y la IA buscará entre tus archivos indexados.</p>
          <p className="muted">
            Solo se buscan archivos que hayas marcado con la opción "Habilitar
            búsqueda con IA" al subirlos.
          </p>
        </div>
      ) : results.length === 0 && !loading ? (
        <div className="empty">
          <p>No se encontraron resultados.</p>
          <p className="muted">Prueba con otra descripción o indexa más archivos.</p>
        </div>
      ) : (
        <div className="search-results">
          {results.map((r) => (
            <div className="search-result" key={r.file_id}>
              <div className="result-icon">{r.is_image ? "🖼" : "📄"}</div>
              <div className="result-info">
                <div className="result-name">{r.name}</div>
                <div className="muted result-path">{r.parent_path}</div>
                <div className="muted result-mime">{r.mime_type}</div>
              </div>
              <div className="result-score">
                <div className="score-value">{Math.round(r.score * 100)}%</div>
                <div className="muted score-label">coincidencia</div>
              </div>
              <div className="result-actions">
                <a
                  className="btn btn-ghost btn-sm"
                  href={api.downloadUrl(r.file_id)}
                  target="_blank"
                  rel="noreferrer"
                >
                  Descargar
                </a>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
