import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";

interface PublicFile {
  id: string;
  name: string;
  mime_type: string;
  size_bytes: number;
  is_image: boolean;
}

interface PublicFolder {
  id: string;
  name: string;
}

interface ShareData {
  type: "file" | "folder";
  file?: PublicFile;
  folder_name?: string;
  folders?: PublicFolder[];
  files?: PublicFile[];
}

export function SharedPage() {
  const { token } = useParams<{ token: string }>();
  const [data, setData] = useState<ShareData | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!token) return;
    setLoading(true);
    fetch(`/api/public/share/${token}`)
      .then((res) => {
        if (!res.ok) return res.json().then((d) => { throw new Error(d.error || "Not found"); });
        return res.json();
      })
      .then((d: ShareData) => {
        setData(d);
        setError("");
      })
      .catch((err) => {
        setError(err.message);
      })
      .finally(() => setLoading(false));
  }, [token]);

  const formatSize = (bytes: number) => {
    const units = ["B", "KB", "MB", "GB"];
    let n = bytes;
    let i = 0;
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
    return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
  };

  if (loading) {
    return (
      <div className="page shared-page">
        <div className="page-header">
          <h2>Cargando...</h2>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="page shared-page">
        <div className="page-header">
          <h2>Enlace no disponible</h2>
        </div>
        <div className="error">{error}</div>
        <p className="muted" style={{ marginTop: "1rem" }}>
          El enlace puede haber expirado, alcanzado el límite de vistas, o sido revocado por el propietario.
        </p>
      </div>
    );
  }

  if (!data) return null;

  const isFile = data.type === "file" && data.file;
  const isFolder = data.type === "folder";

  return (
    <div className="page shared-page">
      <div className="page-header">
        <div className="shared-badge">Contenido compartido</div>
        {isFile && <h2>{data.file!.name}</h2>}
        {isFolder && <h2>{data.folder_name}</h2>}
      </div>

      {isFile && data.file && (
        <div className="shared-file-view">
          <div className="file-info">
            <span className="file-meta">{data.file.mime_type}</span>
            <span className="file-meta">{formatSize(data.file.size_bytes)}</span>
          </div>
          <div className="shared-actions">
            {data.file.is_image ? (
              <a
                href={`/api/public/share/${token}?inline=1`}
                target="_blank"
                rel="noopener noreferrer"
                className="btn btn-primary"
              >
                Ver imagen
              </a>
            ) : data.file.mime_type.startsWith("video/") ? (
              <div className="shared-video">
                <video controls style={{ maxWidth: "100%", maxHeight: "70vh" }}>
                  <source src={`/api/public/share/${token}?inline=1`} type={data.file.mime_type} />
                </video>
              </div>
            ) : null}
            <a
              href={`/api/public/share/${token}?download=1`}
              className="btn"
              download={data.file.name}
            >
              Descargar
            </a>
          </div>
        </div>
      )}

      {isFolder && (
        <div className="shared-folder-view">
          {(!data.folders || data.folders.length === 0) &&
           (!data.files || data.files.length === 0) && (
            <p className="muted">Esta carpeta está vacía.</p>
          )}

          {data.folders && data.folders.length > 0 && (
            <div className="shared-section">
              <h4>Carpetas</h4>
              {data.folders.map((f) => (
                <div key={f.id} className="shared-item">
                  <span className="item-icon">📁</span>
                  <span>{f.name}</span>
                </div>
              ))}
            </div>
          )}

          {data.files && data.files.length > 0 && (
            <div className="shared-section">
              <h4>Archivos</h4>
              {data.files.map((f) => (
                <div key={f.id} className="shared-item">
                  <span className="item-icon">
                    {f.is_image ? "🖼️" : f.mime_type.startsWith("video/") ? "🎬" : "📄"}
                  </span>
                  <span className="item-name">{f.name}</span>
                  <span className="item-size">{formatSize(f.size_bytes)}</span>
                  <a
                    href={`/api/public/share/${token}?file_id=${f.id}`}
                    className="btn btn-small"
                    download={f.name}
                  >
                    Descargar
                  </a>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
