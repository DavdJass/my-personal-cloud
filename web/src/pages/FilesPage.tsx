import { useCallback, useEffect, useRef, useState } from "react";
import { api, formatSize, type FileEntry } from "../api";

export function FilesPage() {
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.listFiles("/");
      setFiles(res.files);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al cargar archivos");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  async function onFilesSelected(list: FileList | null) {
    if (!list || list.length === 0) return;
    setUploading(true);
    setError(null);
    try {
      for (const file of Array.from(list)) {
        await api.uploadFile(file, "/");
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al subir");
    } finally {
      setUploading(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  async function onDelete(id: string, name: string) {
    if (!confirm(`¿Eliminar "${name}"?`)) return;
    try {
      await api.deleteFile(id);
      setFiles((prev) => prev.filter((f) => f.id !== id));
    } catch (err) {
      alert(err instanceof Error ? err.message : "Error al eliminar");
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <h2>Archivos</h2>
        <div className="actions">
          <input
            ref={inputRef}
            type="file"
            multiple
            hidden
            onChange={(e) => onFilesSelected(e.target.files)}
          />
          <button
            className="btn btn-primary"
            onClick={() => inputRef.current?.click()}
            disabled={uploading}
          >
            {uploading ? "Subiendo..." : "Subir archivos"}
          </button>
        </div>
      </div>

      {error && <div className="error">{error}</div>}

      {loading ? (
        <div className="empty">Cargando...</div>
      ) : files.length === 0 ? (
        <div className="empty">
          <p>No tienes archivos aún.</p>
          <p className="muted">Sube tu primer archivo para empezar.</p>
        </div>
      ) : (
        <div className="file-table">
          <div className="file-row file-head">
            <div>Nombre</div>
            <div>Tipo</div>
            <div>Tamaño</div>
            <div>Fecha</div>
            <div></div>
          </div>
          {files.map((f) => (
            <div className="file-row" key={f.id}>
              <div className="file-name">
                <span className="file-icon">{f.is_image ? "[IMG]" : "[FILE]"}</span>
                <span>{f.name}</span>
              </div>
              <div className="muted">{f.mime_type}</div>
              <div>{formatSize(f.size_bytes)}</div>
              <div className="muted">
                {new Date(f.created_at).toLocaleString()}
              </div>
              <div className="file-actions">
                <a
                  className="btn btn-ghost"
                  href={api.downloadUrl(f.id)}
                  target="_blank"
                  rel="noreferrer"
                >
                  Descargar
                </a>
                <button
                  className="btn btn-danger"
                  onClick={() => onDelete(f.id, f.name)}
                >
                  Eliminar
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
