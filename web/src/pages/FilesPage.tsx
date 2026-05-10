import { useCallback, useEffect, useRef, useState } from "react";
import { api, formatSize, type FileEntry, type FolderEntry } from "../api";

// ── helpers ───────────────────────────────────────────────────────────────────

function joinPath(parent: string, name: string): string {
  return parent === "/" ? "/" + name : parent + "/" + name;
}

function breadcrumbSegments(p: string): { label: string; path: string }[] {
  const segs = [{ label: "Raíz", path: "/" }];
  if (p === "/") return segs;
  const parts = p.replace(/^\//, "").split("/");
  let acc = "";
  for (const part of parts) {
    acc += "/" + part;
    segs.push({ label: part, path: acc });
  }
  return segs;
}

// ── MoveModal ────────────────────────────────────────────────────────────────

interface MoveModalProps {
  label: string;
  currentPath: string;
  onConfirm: (dest: string) => void;
  onClose: () => void;
}

function MoveModal({ label, currentPath, onConfirm, onClose }: MoveModalProps) {
  const [dest, setDest] = useState(currentPath);
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-card" onClick={(e) => e.stopPropagation()}>
        <h3>Mover "{label}"</h3>
        <p className="muted">Introduce la ruta de destino:</p>
        <input
          className="modal-input"
          value={dest}
          onChange={(e) => setDest(e.target.value)}
          placeholder="/carpeta/destino"
          autoFocus
        />
        <div className="modal-actions">
          <button className="btn btn-ghost" onClick={onClose}>
            Cancelar
          </button>
          <button
            className="btn btn-primary"
            onClick={() => {
              const d = dest.trim() || "/";
              onConfirm(d.startsWith("/") ? d : "/" + d);
            }}
          >
            Mover
          </button>
        </div>
      </div>
    </div>
  );
}

// ── RenameInline ─────────────────────────────────────────────────────────────

interface RenameInlineProps {
  initial: string;
  onConfirm: (name: string) => void;
  onCancel: () => void;
}

function RenameInline({ initial, onConfirm, onCancel }: RenameInlineProps) {
  const [val, setVal] = useState(initial);
  return (
    <span className="rename-inline">
      <input
        className="rename-input"
        value={val}
        onChange={(e) => setVal(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") onConfirm(val.trim());
          if (e.key === "Escape") onCancel();
        }}
        autoFocus
        onClick={(e) => e.stopPropagation()}
      />
      <button
        className="btn btn-primary btn-xs"
        onClick={(e) => {
          e.stopPropagation();
          onConfirm(val.trim());
        }}
      >
        OK
      </button>
      <button
        className="btn btn-ghost btn-xs"
        onClick={(e) => {
          e.stopPropagation();
          onCancel();
        }}
      >
        ✕
      </button>
    </span>
  );
}

// ── FilesPage ─────────────────────────────────────────────────────────────────

type RenameTarget =
  | { kind: "file"; id: string; name: string }
  | { kind: "folder"; id: string; name: string };

type MoveTarget =
  | { kind: "file"; id: string; name: string; parentPath: string }
  | { kind: "folder"; id: string; name: string; parentPath: string };

export function FilesPage() {
  const [currentPath, setCurrentPath] = useState("/");
  const [folders, setFolders] = useState<FolderEntry[]>([]);
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const [renaming, setRenaming] = useState<RenameTarget | null>(null);
  const [moving, setMoving] = useState<MoveTarget | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const refresh = useCallback(async (p: string) => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.listFiles(p);
      setFolders(res.folders ?? []);
      setFiles(res.files ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al cargar");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh(currentPath);
  }, [currentPath, refresh]);

  function navigate(p: string) {
    setCurrentPath(p);
    setRenaming(null);
    setMoving(null);
  }

  // ── upload ──
  async function onFilesSelected(list: FileList | null) {
    if (!list || list.length === 0) return;
    setUploading(true);
    setError(null);
    try {
      for (const file of Array.from(list)) {
        await api.uploadFile(file, currentPath);
      }
      await refresh(currentPath);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al subir");
    } finally {
      setUploading(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  // ── new folder ──
  async function onNewFolder() {
    const name = prompt("Nombre de la nueva carpeta:");
    if (!name?.trim()) return;
    try {
      await api.createFolder(name.trim(), currentPath);
      await refresh(currentPath);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al crear carpeta");
    }
  }

  // ── rename ──
  async function onRenameConfirm(newName: string) {
    if (!renaming || !newName) return;
    try {
      if (renaming.kind === "file") {
        await api.patchFile(renaming.id, { name: newName });
      } else {
        await api.patchFolder(renaming.id, { name: newName });
      }
      setRenaming(null);
      await refresh(currentPath);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al renombrar");
      setRenaming(null);
    }
  }

  // ── move ──
  async function onMoveConfirm(dest: string) {
    if (!moving) return;
    try {
      if (moving.kind === "file") {
        await api.patchFile(moving.id, { parent_path: dest });
      } else {
        await api.patchFolder(moving.id, { parent_path: dest });
      }
      setMoving(null);
      await refresh(currentPath);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al mover");
      setMoving(null);
    }
  }

  // ── delete file ──
  async function onDeleteFile(id: string, name: string) {
    if (!confirm(`¿Eliminar el archivo "${name}"?`)) return;
    try {
      await api.deleteFile(id);
      setFiles((prev) => prev.filter((f) => f.id !== id));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al eliminar");
    }
  }

  // ── delete folder ──
  async function onDeleteFolder(id: string, name: string) {
    if (
      !confirm(
        `¿Eliminar la carpeta "${name}" y todo su contenido? Esta acción no se puede deshacer.`
      )
    )
      return;
    try {
      await api.deleteFolder(id);
      setFolders((prev) => prev.filter((f) => f.id !== id));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al eliminar carpeta");
    }
  }

  const segs = breadcrumbSegments(currentPath);
  const isEmpty = !loading && folders.length === 0 && files.length === 0;

  return (
    <div className="page">
      {/* header */}
      <div className="page-header">
        <h2>Archivos</h2>
        <div className="actions">
          <button className="btn btn-ghost" onClick={onNewFolder}>
            + Carpeta
          </button>
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

      {/* breadcrumb */}
      <nav className="breadcrumb">
        {segs.map((s, i) => (
          <span key={s.path} className="bc-seg">
            {i > 0 && <span className="bc-sep">/</span>}
            {i < segs.length - 1 ? (
              <button className="bc-btn" onClick={() => navigate(s.path)}>
                {s.label}
              </button>
            ) : (
              <span className="bc-current">{s.label}</span>
            )}
          </span>
        ))}
      </nav>

      {error && <div className="error">{error}</div>}

      {loading ? (
        <div className="empty">Cargando...</div>
      ) : isEmpty ? (
        <div className="empty">
          <p>Esta carpeta está vacía.</p>
          <p className="muted">Crea una carpeta o sube archivos para empezar.</p>
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

          {/* folders */}
          {folders.map((folder) => (
            <div className="file-row" key={folder.id}>
              <div className="file-name">
                <span className="file-icon folder-icon">📁</span>
                {renaming?.id === folder.id ? (
                  <RenameInline
                    initial={folder.name}
                    onConfirm={onRenameConfirm}
                    onCancel={() => setRenaming(null)}
                  />
                ) : (
                  <button
                    className="folder-link"
                    onClick={() => navigate(joinPath(folder.parent_path, folder.name))}
                  >
                    {folder.name}
                  </button>
                )}
              </div>
              <div className="muted">Carpeta</div>
              <div className="muted">—</div>
              <div className="muted">
                {new Date(folder.created_at).toLocaleDateString()}
              </div>
              <div className="file-actions">
                <button
                  className="btn btn-ghost btn-sm"
                  onClick={() =>
                    setRenaming({ kind: "folder", id: folder.id, name: folder.name })
                  }
                >
                  Renombrar
                </button>
                <button
                  className="btn btn-ghost btn-sm"
                  onClick={() =>
                    setMoving({
                      kind: "folder",
                      id: folder.id,
                      name: folder.name,
                      parentPath: folder.parent_path,
                    })
                  }
                >
                  Mover
                </button>
                <button
                  className="btn btn-danger btn-sm"
                  onClick={() => onDeleteFolder(folder.id, folder.name)}
                >
                  Eliminar
                </button>
              </div>
            </div>
          ))}

          {/* files */}
          {files.map((f) => (
            <div className="file-row" key={f.id}>
              <div className="file-name">
                <span className="file-icon">{f.is_image ? "🖼" : "📄"}</span>
                {renaming?.id === f.id ? (
                  <RenameInline
                    initial={f.name}
                    onConfirm={onRenameConfirm}
                    onCancel={() => setRenaming(null)}
                  />
                ) : (
                  <span>{f.name}</span>
                )}
              </div>
              <div className="muted file-mime">{f.mime_type}</div>
              <div>{formatSize(f.size_bytes)}</div>
              <div className="muted">
                {new Date(f.created_at).toLocaleDateString()}
              </div>
              <div className="file-actions">
                <a
                  className="btn btn-ghost btn-sm"
                  href={api.downloadUrl(f.id)}
                  target="_blank"
                  rel="noreferrer"
                >
                  Descargar
                </a>
                <button
                  className="btn btn-ghost btn-sm"
                  onClick={() =>
                    setRenaming({ kind: "file", id: f.id, name: f.name })
                  }
                >
                  Renombrar
                </button>
                <button
                  className="btn btn-ghost btn-sm"
                  onClick={() =>
                    setMoving({
                      kind: "file",
                      id: f.id,
                      name: f.name,
                      parentPath: f.parent_path,
                    })
                  }
                >
                  Mover
                </button>
                <button
                  className="btn btn-danger btn-sm"
                  onClick={() => onDeleteFile(f.id, f.name)}
                >
                  Eliminar
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {moving && (
        <MoveModal
          label={moving.name}
          currentPath={moving.parentPath}
          onConfirm={onMoveConfirm}
          onClose={() => setMoving(null)}
        />
      )}
    </div>
  );
}
