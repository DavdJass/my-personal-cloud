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
  const [previewFile, setPreviewFile] = useState<PublicFile | null>(null);

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
      <div className="shared-page">
        <div className="shared-header">
          <span className="shared-logo">My Personal Cloud</span>
        </div>
        <div className="shared-card" style={{ padding: "40px", textAlign: "center" }}>
          <div className="spinner" style={{ margin: "0 auto" }} />
          <p style={{ marginTop: 12, color: "var(--muted)" }}>Cargando...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="shared-page">
        <div className="shared-header">
          <span className="shared-logo">My Personal Cloud</span>
        </div>
        <div className="shared-card">
          <div className="shared-card-body" style={{ textAlign: "center", padding: "40px 20px" }}>
            <span style={{ fontSize: 48 }}>🔗</span>
            <h2 style={{ marginTop: 12 }}>Enlace no disponible</h2>
            <p className="muted" style={{ marginTop: 8 }}>
              {error}
            </p>
            <p className="muted" style={{ marginTop: 4, fontSize: 13 }}>
              El enlace puede haber expirado, alcanzado el límite de vistas,
              o sido revocado por el propietario.
            </p>
          </div>
        </div>
        <div className="shared-footer">
          My Personal Cloud &mdash; Tus archivos. Tu hardware. Tu nube.
        </div>
      </div>
    );
  }

  if (!data) return null;

  const isFile = data.type === "file" && data.file;
  const isFolder = data.type === "folder";

  const contentUrl = () =>
    `/api/public/share/${token}?content=1`;

  return (
    <div className="shared-page">
      <div className="shared-header">
        <span className="shared-logo">My Personal Cloud</span>
        <span className="shared-header-muted">contenido compartido</span>
      </div>

      <div className="shared-card">
        {isFile && data.file && (
          <>
            <div className="shared-card-header">
              <div className="shared-badge">Archivo compartido</div>
              <h2>{data.file.name}</h2>
            </div>

            {/* ── Preview ── */}
            {data.file.is_image ? (
              <div className="shared-inline-preview">
                <img src={contentUrl()} alt={data.file.name} className="shared-img" />
              </div>
            ) : data.file.mime_type.startsWith("video/") ? (
              <div className="shared-inline-preview">
                <video controls style={{ maxWidth: "100%", maxHeight: "70vh" }}>
                  <source src={contentUrl()} type={data.file.mime_type} />
                </video>
              </div>
            ) : (
              <div className="shared-no-preview">
                <span className="no-preview-icon">📄</span>
                <p>Vista previa no disponible para este tipo de archivo.</p>
              </div>
            )}

            {/* ── Meta / download bar ── */}
            <div className="shared-meta-bar">
              <div className="file-info">
                <span className="file-meta">{data.file.mime_type}</span>
                <span className="file-meta">{formatSize(data.file.size_bytes)}</span>
              </div>
              <a href={contentUrl()} className="btn" download={data.file.name}>
                Descargar
              </a>
            </div>
          </>
        )}

        {isFolder && (
          <>
            <div className="shared-card-header">
              <div className="shared-badge">Carpeta compartida</div>
              <h2>{data.folder_name}</h2>
            </div>

            <div className="shared-folder-view">
              {(!data.folders || data.folders.length === 0) &&
               (!data.files || data.files.length === 0) && (
                <div className="shared-empty">
                  <span style={{ fontSize: 40 }}>📁</span>
                  <p style={{ marginTop: 8 }}>Esta carpeta está vacía.</p>
                </div>
              )}

              {data.folders && data.folders.length > 0 && (
                <div className="shared-section">
                  <h4>Carpetas</h4>
                  {data.folders.map((f) => (
                    <div key={f.id} className="shared-item" style={{ cursor: "default" }}>
                      <span className="item-icon">📁</span>
                      <span className="item-name">{f.name}</span>
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
                      <div className="shared-item-actions">
                        {(f.is_image || f.mime_type.startsWith("video/")) && (
                          <button
                            className="btn btn-tiny"
                            onClick={() => setPreviewFile(f)}
                          >
                            Vista previa
                          </button>
                        )}
                        <a
                          href={`/api/public/share/${token}?file_id=${f.id}`}
                          className="btn btn-tiny"
                          download={f.name}
                        >
                          Descargar
                        </a>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </>
        )}
      </div>

      <div className="shared-footer">
        My Personal Cloud &mdash; Tus archivos. Tu hardware. Tu nube.
      </div>

      {/* ── Preview modal ── */}
      {previewFile && (
        <div className="modal-overlay" onClick={() => setPreviewFile(null)}>
          <div className="modal modal-preview" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>{previewFile.name}</h3>
              <button className="modal-close" onClick={() => setPreviewFile(null)}>
                &times;
              </button>
            </div>
            <div className="modal-body preview-body">
              {previewFile.is_image ? (
                <img
                  src={`/api/public/share/${token}?file_id=${previewFile.id}&preview=1`}
                  alt={previewFile.name}
                  className="preview-img"
                />
              ) : (
                <video controls className="preview-video">
                  <source
                    src={`/api/public/share/${token}?file_id=${previewFile.id}&preview=1`}
                    type={previewFile.mime_type}
                  />
                </video>
              )}
              <div className="preview-footer-alt">
                <span>{previewFile.mime_type}</span>
                <span>{formatSize(previewFile.size_bytes)}</span>
                <a
                  href={`/api/public/share/${token}?file_id=${previewFile.id}`}
                  className="btn btn-sm"
                  download={previewFile.name}
                >
                  Descargar
                </a>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
