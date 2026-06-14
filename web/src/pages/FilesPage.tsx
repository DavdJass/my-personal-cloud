import {
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { api, formatSize, formatDate, type FileEntry, type FolderEntry } from "../api";
import { useToast } from "../toast";
import { ShareDialog } from "./ShareDialog";

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

function isImageMime(m: string): boolean {
  return m.startsWith("image/");
}

function isVideoMime(m: string): boolean {
  return m.startsWith("video/");
}

type SortField = "name" | "mime_type" | "size_bytes" | "created_at";
type SortDir = "asc" | "desc";

// ── CreateFolderModal ─────────────────────────────────────────────────────────

interface CreateFolderModalProps {
  onConfirm: (name: string) => void;
  onClose: () => void;
}

function CreateFolderModal({ onConfirm, onClose }: CreateFolderModalProps) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");

  function handleConfirm() {
    const trimmed = name.trim();
    if (!trimmed) {
      setError("El nombre no puede estar vacío");
      return;
    }
    onConfirm(trimmed);
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-card" onClick={(e) => e.stopPropagation()}>
        <h3>Nueva carpeta</h3>
        <p className="muted">Introduce el nombre de la carpeta:</p>
        <input
          className="modal-input"
          value={name}
          onChange={(e) => { setName(e.target.value); setError(""); }}
          onKeyDown={(e) => {
            if (e.key === "Enter") handleConfirm();
            if (e.key === "Escape") onClose();
          }}
          placeholder="nombre de la carpeta"
          autoFocus
        />
        {error && <div className="modal-error">{error}</div>}
        <div className="modal-actions">
          <button className="btn btn-ghost" onClick={onClose}>
            Cancelar
          </button>
          <button className="btn btn-primary" onClick={handleConfirm}>
            Crear
          </button>
        </div>
      </div>
    </div>
  );
}

// ── MoveModal (folder browser) ───────────────────────────────────────────────

interface MoveModalProps {
  label: string;
  currentPath: string;
  onConfirm: (dest: string) => void;
  onClose: () => void;
}

function MoveModal({ label, currentPath, onConfirm, onClose }: MoveModalProps) {
  const [browsePath, setBrowsePath] = useState(currentPath === "/" ? "/" : currentPath);
  const [subfolders, setSubfolders] = useState<{ id: string; name: string }[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api.listFiles(browsePath, 200, 0).then((res) => {
      if (!cancelled) {
        setSubfolders(res.folders ?? []);
        setLoading(false);
      }
    }).catch(() => {
      if (!cancelled) setLoading(false);
    });
    return () => { cancelled = true; };
  }, [browsePath]);

  function goUp() {
    if (browsePath === "/") return;
    const parent = browsePath.substring(0, browsePath.lastIndexOf("/"));
    setBrowsePath(parent || "/");
  }

  function segs(p: string) {
    const parts = [{ label: "Raíz", path: "/" }];
    if (p === "/") return parts;
    const split = p.replace(/^\//, "").split("/");
    let acc = "";
    for (const s of split) {
      acc += "/" + s;
      parts.push({ label: s, path: acc });
    }
    return parts;
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-card move-card" onClick={(e) => e.stopPropagation()}>
        <h3>Mover &ldquo;{label}&rdquo;</h3>
        <p className="muted">Selecciona la carpeta de destino:</p>

        {/* Breadcrumb / current location */}
        <nav className="move-breadcrumb">
          {browsePath !== "/" && (
            <button className="btn btn-ghost btn-xs move-up-btn" onClick={goUp} title="Subir">
              {"↑"}
            </button>
          )}
          {segs(browsePath).map((s, i) => (
            <span key={s.path} className="bc-seg">
              {i > 0 && <span className="bc-sep">/</span>}
              {i < segs(browsePath).length - 1 ? (
                <button className="bc-btn" onClick={() => setBrowsePath(s.path)}>{s.label}</button>
              ) : (
                <span className="bc-current">{s.label}</span>
              )}
            </span>
          ))}
        </nav>

        {/* Folder list */}
        <div className="move-folder-list">
          {loading ? (
            <div className="muted" style={{ padding: "12px" }}>Cargando...</div>
          ) : subfolders.length === 0 ? (
            <div className="muted" style={{ padding: "12px" }}>No hay subcarpetas</div>
          ) : (
            subfolders.map((f) => (
              <button
                key={f.id}
                className="move-folder-item"
                onClick={() => {
                  const next = browsePath === "/" ? "/" + f.name : browsePath + "/" + f.name;
                  setBrowsePath(next);
                }}
              >
                <span className="file-icon">{"📁"}</span>
                <span>{f.name}</span>
              </button>
            ))
          )}
        </div>

        <div className="modal-actions">
          <button className="btn btn-ghost" onClick={onClose}>Cancelar</button>
          <button className="btn btn-primary" onClick={() => onConfirm(browsePath)}>
            Mover aquí
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
        {"✕"}
      </button>
    </span>
  );
}

// ── UploadProgress ────────────────────────────────────────────────────────────

interface UploadItem {
  name: string;
  pct: number;
}

function UploadProgress({ items }: { items: UploadItem[] }) {
  if (items.length === 0) return null;
  return (
    <div className="upload-progress-bar">
      {items.map((it, i) => (
        <div key={i} className="upload-item">
          <span className="upload-name">{it.name}</span>
          <div className="progress-track">
            <div className="progress-fill" style={{ width: it.pct + "%" }} />
          </div>
          <span className="upload-pct">{it.pct}%</span>
        </div>
      ))}
    </div>
  );
}

// ── PreviewModal ──────────────────────────────────────────────────────────────

function PreviewModal({ file, onClose }: { file: FileEntry; onClose: () => void }) {
  if (isImageMime(file.mime_type)) {
    return (
      <div className="modal-backdrop preview-modal" onClick={onClose}>
        <div className="preview-card" onClick={(e) => e.stopPropagation()}>
          <img
            src={api.fullUrl(file.id)}
            alt={file.name}
            className="preview-img"
          />
          <div className="preview-footer">
            <span>{file.name}</span>
            <span className="muted">{formatSize(file.size_bytes)}</span>
          </div>
          <button className="btn btn-ghost preview-close" onClick={onClose}>
            Cerrar
          </button>
        </div>
      </div>
    );
  }
  if (isVideoMime(file.mime_type)) {
    return (
      <div className="modal-backdrop preview-modal" onClick={onClose}>
        <div className="preview-card" onClick={(e) => e.stopPropagation()}>
          <video controls autoPlay className="preview-video" preload="auto">
            <source src={api.streamUrl(file.id)} type={file.mime_type} />
          </video>
          <div className="preview-footer">
            <span>{file.name}</span>
            <span className="muted">{formatSize(file.size_bytes)}</span>
          </div>
          <button className="btn btn-ghost preview-close" onClick={onClose}>
            Cerrar
          </button>
        </div>
      </div>
    );
  }
  return null;
}

// ── Sort controls ────────────────────────────────────────────────────────────

interface SortControlsProps {
  field: SortField;
  dir: SortDir;
  onChange: (field: SortField, dir: SortDir) => void;
}

function SortControls({ field, dir, onChange }: SortControlsProps) {
  const cols: { key: SortField; label: string }[] = [
    { key: "name", label: "Nombre" },
    { key: "mime_type", label: "Tipo" },
    { key: "size_bytes", label: "Tamaño" },
    { key: "created_at", label: "Fecha" },
  ];

  return (
    <div className="file-row file-head">
      {cols.map((c) => (
        <div
          key={c.key}
          className="sort-header"
          onClick={() => {
            if (field === c.key) {
              onChange(c.key, dir === "asc" ? "desc" : "asc");
            } else {
              onChange(c.key, "asc");
            }
          }}
        >
          {c.label}
          {field === c.key && <span className="sort-arrow">{dir === "asc" ? " ↑" : " ↓"}</span>}
        </div>
      ))}
      <div></div>
    </div>
  );
}

// ── TrashView ─────────────────────────────────────────────────────────────────

function TrashView({ onBack }: { onBack: () => void }) {
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const { toast } = useToast();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listTrash(200, 0);
      setFiles(res.files);
    } catch (err) {
      toast(err instanceof Error ? err.message : "Error al cargar papelera", "error");
    } finally {
      setLoading(false);
    }
  }, [toast]);

  useEffect(() => { load(); }, [load]);

  async function restore(id: string) {
    try {
      await api.restoreFile(id);
      toast("Archivo restaurado", "success");
      load();
    } catch (err) {
      toast(err instanceof Error ? err.message : "Error al restaurar", "error");
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <h2>Papelera</h2>
        <div className="actions">
          <button className="btn btn-ghost" onClick={onBack}>
            {"←"} Volver
          </button>
          {files.length > 0 && (
            <button className="btn btn-danger" onClick={async () => {
              if (!confirm("Vaciar papelera? Esto eliminará los archivos permanentemente.")) return;
              try {
                await api.emptyTrash();
                toast("Papelera vaciada", "success");
                load();
              } catch (err) {
                toast(err instanceof Error ? err.message : "Error", "error");
              }
            }}>
              Vaciar papelera
            </button>
          )}
        </div>
      </div>
      {loading ? (
        <div className="empty">Cargando...</div>
      ) : files.length === 0 ? (
        <div className="empty"><p>La papelera está vacía.</p></div>
      ) : (
        <div className="file-table">
          <div className="file-row file-head">
            <div>Nombre</div>
            <div>Ruta original</div>
            <div>Eliminado</div>
            <div>Tamaño</div>
            <div></div>
          </div>
          {files.map((f) => (
            <div className="file-row" key={f.id}>
              <div className="file-name">
                <span className="file-icon">{f.is_image ? "🖼️" : "📄"}</span>
                <span>{f.name}</span>
              </div>
              <div className="muted">{f.parent_path}</div>
              <div className="muted">{f.deleted_at ? formatDate(f.deleted_at) : ""}</div>
              <div>{formatSize(f.size_bytes)}</div>
              <div className="file-actions">
                <button className="btn btn-primary btn-sm" onClick={() => restore(f.id)}>
                  Restaurar
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── FilesPage ─────────────────────────────────────────────────────────────────

type RenameTarget =
  | { kind: "file"; id: string; name: string }
  | { kind: "folder"; id: string; name: string };

type MoveTarget =
  | { kind: "file"; id: string; name: string; parentPath: string }
  | { kind: "folder"; id: string; name: string; parentPath: string };

type ViewMode = "list" | "grid";

export function FilesPage() {
  const [currentPath, setCurrentPath] = useState("/");
  const [folders, setFolders] = useState<FolderEntry[]>([]);
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadQueue, setUploadQueue] = useState<UploadItem[]>([]);
  const [renaming, setRenaming] = useState<RenameTarget | null>(null);
  const [moving, setMoving] = useState<MoveTarget | null>(null);
  const [shareItem, setShareItem] = useState<{ id: string; name: string; kind: "file" | "folder" } | null>(null);
  const [batchMoveTarget, setBatchMoveTarget] = useState(false);
  const [page, setPage] = useState(0);
  const [totalItems, setTotalItems] = useState(0);
  const pageSize = 50;
  const inputRef = useRef<HTMLInputElement>(null);
  const dropRef = useRef<HTMLDivElement>(null);
  const [dragOver, setDragOver] = useState(false);
  const { toast } = useToast();

  // Multi-select.
  const [selected, setSelected] = useState<Set<string>>(new Set());

  // Sorting.
  const [sortField, setSortField] = useState<SortField>("name");
  const [sortDir, setSortDir] = useState<SortDir>("asc");

  // Search.
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<{ files: FileEntry[]; folders: FolderEntry[] } | null>(null);
  const searchTimer = useRef<ReturnType<typeof setTimeout>>();

  // View mode.
  const [viewMode, setViewMode] = useState<ViewMode>("list");

  // Trash view.
  const [showTrash, setShowTrash] = useState(false);

  // Folder creation modal.
  const [showNewFolderModal, setShowNewFolderModal] = useState(false);

  // Preview.
  const [previewFile, setPreviewFile] = useState<FileEntry | null>(null);

  // Keyboard shortcuts.
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      // Don't handle if typing in an input.
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;

      if (e.key === "Delete" || e.key === "Backspace") {
        if (selected.size > 0) {
          e.preventDefault();
          batchDelete();
        }
      }
      if (e.key === "f" && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        // Focus search.
        document.querySelector<HTMLInputElement>(".search-input")?.focus();
      }
    }
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected]);

  const loadFiles = useCallback(async (p: string, pageNum: number) => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.listFiles(p, pageSize, pageNum * pageSize);
      setFolders(res.folders ?? []);
      setFiles(res.files ?? []);
      setTotalItems(res.total);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al cargar");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!searchQuery) {
      setSearchResults(null);
      loadFiles(currentPath, page);
    }
  }, [currentPath, page, loadFiles, searchQuery]);

  // Search effect.
  useEffect(() => {
    if (!searchQuery.trim()) {
      setSearchResults(null);
      return;
    }
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(async () => {
      try {
        const res = await api.search(searchQuery, 100, 0);
        setSearchResults(res);
      } catch {}
    }, 250);
    return () => { if (searchTimer.current) clearTimeout(searchTimer.current); };
  }, [searchQuery]);

  function navigate(p: string) {
    setCurrentPath(p);
    setPage(0);
    setRenaming(null);
    setMoving(null);
    setSelected(new Set());
  }

  // ── upload (progress + XHR) ──
  async function onFilesSelected(list: FileList | null) {
    if (!list || list.length === 0) return;
    setUploading(true);
    setError(null);
    const items: UploadItem[] = Array.from(list).map((f) => ({ name: f.name, pct: 0 }));
    setUploadQueue(items);

    try {
      for (let i = 0; i < list.length; i++) {
        const file = list[i];
        await api.uploadFile(file, currentPath, (pct) => {
          setUploadQueue((prev) => {
            const next = [...prev];
            next[i] = { ...next[i], pct };
            return next;
          });
        });
      }
      toast(`${list.length} archivo(s) subido(s) correctamente`, "success");
      await loadFiles(currentPath, page);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Error al subir");
      toast(err instanceof Error ? err.message : "Error al subir", "error");
    } finally {
      setUploading(false);
      setUploadQueue([]);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  // ── drag & drop ──
  useEffect(() => {
    const el = dropRef.current;
    if (!el) return;

    function onDragOver(e: DragEvent) {
      e.preventDefault();
      e.stopPropagation();
      setDragOver(true);
    }
    function onDragLeave() {
      setDragOver(false);
    }
    function onDrop(e: DragEvent) {
      e.preventDefault();
      e.stopPropagation();
      setDragOver(false);
      if (e.dataTransfer?.files) {
        onFilesSelected(e.dataTransfer.files);
      }
    }
    el.addEventListener("dragover", onDragOver);
    el.addEventListener("dragleave", onDragLeave);
    el.addEventListener("drop", onDrop);
    return () => {
      el.removeEventListener("dragover", onDragOver);
      el.removeEventListener("dragleave", onDragLeave);
      el.removeEventListener("drop", onDrop);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentPath, page]);

  // ── paste upload ──
  useEffect(() => {
    function onPaste(e: ClipboardEvent) {
      if (showTrash) return;
      const items = e.clipboardData?.items;
      if (!items) return;
      const files: File[] = [];
      for (let i = 0; i < items.length; i++) {
        const item = items[i];
        if (item.kind === "file") {
          const f = item.getAsFile();
          if (f) files.push(f);
        }
      }
      if (files.length > 0) {
        e.preventDefault();
        const dt = new DataTransfer();
        files.forEach((f) => dt.items.add(f));
        onFilesSelected(dt.files);
      }
    }
    document.addEventListener("paste", onPaste);
    return () => document.removeEventListener("paste", onPaste);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentPath, page, showTrash]);

  // ── new folder ──
  async function onNewFolder(name: string) {
    if (!name.trim()) return;
    setShowNewFolderModal(false);
    try {
      await api.createFolder(name.trim(), currentPath);
      await loadFiles(currentPath, page);
      toast("Carpeta creada", "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Error al crear carpeta";
      setError(msg);
      toast(msg, "error");
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
      await loadFiles(currentPath, page);
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Error al renombrar";
      setError(msg);
      toast(msg, "error");
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
      await loadFiles(currentPath, page);
      toast("Elemento movido", "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Error al mover";
      setError(msg);
      toast(msg, "error");
      setMoving(null);
    }
  }

  // ── delete single ──
  async function onDeleteFile(id: string, name: string) {
    if (!confirm(`¿Eliminar el archivo "${name}"?`)) return;
    try {
      await api.deleteFile(id);
      setFiles((prev) => prev.filter((f) => f.id !== id));
      toast("Archivo movido a la papelera", "info");
    } catch (err) {
      toast(err instanceof Error ? err.message : "Error al eliminar", "error");
    }
  }

  async function onDeleteFolder(id: string, name: string) {
    if (!confirm(`¿Eliminar la carpeta "${name}" y todo su contenido?`)) return;
    try {
      await api.deleteFolder(id);
      setFolders((prev) => prev.filter((f) => f.id !== id));
      toast("Carpeta eliminada", "info");
    } catch (err) {
      toast(err instanceof Error ? err.message : "Error al eliminar carpeta", "error");
    }
  }

  // ── batch delete ──
  async function batchDelete() {
    if (selected.size === 0) return;
    if (!confirm(`¿Eliminar ${selected.size} elemento(s)?`)) return;
    let ok = 0;
    for (const id of selected) {
      // Check if it's a file or folder.
      const file = files.find((f) => f.id === id);
      try {
        if (file) {
          await api.deleteFile(id);
        } else {
          await api.deleteFolder(id);
        }
        ok++;
      } catch {}
    }
    setSelected(new Set());
    toast(`${ok} elemento(s) movido(s) a la papelera`, "info");
    await loadFiles(currentPath, page);
  }

  // ── sorted view ──
  function getSortedItems() {
    const items = [
      ...folders.map((f) => ({ ...f, _kind: "folder" as const, mime_type: "inode/directory", size_bytes: 0 })),
      ...files.map((f) => ({ ...f, _kind: "file" as const })),
    ];

    const dir = sortDir === "asc" ? 1 : -1;
    items.sort((a, b) => {
      let cmp = 0;
      if (sortField === "name") cmp = a.name.localeCompare(b.name);
      else if (sortField === "mime_type") cmp = a.mime_type.localeCompare(b.mime_type);
      else if (sortField === "size_bytes") cmp = a.size_bytes - b.size_bytes;
      else if (sortField === "created_at") cmp = a.created_at.localeCompare(b.created_at);
      // Folders always before files.
      if (cmp === 0 && a._kind !== b._kind) {
        return a._kind === "folder" ? -1 : 1;
      }
      return cmp * dir;
    });

    return items;
  }

  function toggleSelect(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  const segs = breadcrumbSegments(currentPath);
  const isEmpty = !loading && folders.length === 0 && files.length === 0;

  // Trash view.
  if (showTrash) {
    return <TrashView onBack={() => setShowTrash(false)} />;
  }

  // Search results.
  if (searchResults) {
    const allResults = [...searchResults.folders, ...searchResults.files];
    return (
      <div className="page">
        <div className="page-header">
          <h2>Búsqueda: &ldquo;{searchQuery}&rdquo;</h2>
          <button className="btn btn-ghost" onClick={() => { setSearchQuery(""); setSearchResults(null); }}>
            {"✕"} Limpiar
          </button>
        </div>
        {allResults.length === 0 ? (
          <div className="empty"><p>Sin resultados.</p></div>
        ) : (
          <div className="file-table">
            {allResults.map((item: any) => (
              <div className="file-row" key={item.id}>
                <div className="file-name">
                  <span className="file-icon">{item.mime_type === "inode/directory" ? "📁" : "📄"}</span>
                  <span>{item.name}</span>
                </div>
                <div className="muted">{item.parent_path}</div>
                <div>{item.mime_type === "inode/directory" ? "Carpeta" : formatSize(item.size_bytes)}</div>
                <div className="file-actions">
                  <button className="btn btn-ghost btn-sm" onClick={() => navigate(item.parent_path)}>
                    Ir a la carpeta
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="page" ref={dropRef}>
      {/* Drag overlay */}
      {dragOver && <div className="drag-overlay">Suelta los archivos para subirlos</div>}

      {/* Header */}
      <div className="page-header">
        <h2>Archivos</h2>
        <div className="actions">
          {/* Search */}
          <input
            className="search-input"
            type="text"
            placeholder="Buscar archivos..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />

          {/* View toggle */}
          <button
            className="btn btn-ghost btn-sm"
            onClick={() => setViewMode(viewMode === "list" ? "grid" : "list")}
            title={viewMode === "list" ? "Vista cuadrícula" : "Vista lista"}
          >
            {viewMode === "list" ? "☰" : "▦"}
          </button>

          <button className="btn btn-ghost" onClick={() => setShowTrash(true)}>
            Papelera
          </button>
          <button className="btn btn-ghost" onClick={() => setShowNewFolderModal(true)}>
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

      {/* Upload progress */}
      <UploadProgress items={uploadQueue} />

      {/* Breadcrumb */}
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

      {/* Batch actions */}
      {selected.size > 0 && (
        <div className="batch-bar">
          <span className="muted">{selected.size} seleccionado(s)</span>
          <button className="btn btn-danger btn-sm" onClick={batchDelete}>
            Eliminar
          </button>
          <button className="btn btn-ghost btn-sm" onClick={() => setBatchMoveTarget(true)}>
            Mover
          </button>
          <button className="btn btn-ghost btn-sm" onClick={() => setSelected(new Set())}>
            Deseleccionar
          </button>
        </div>
      )}

      {error && <div className="error">{error}</div>}

      {loading ? (
        <div className="empty">Cargando...</div>
      ) : isEmpty ? (
        <div className="empty">
          <p>Esta carpeta está vacía.</p>
          <p className="muted">Crea una carpeta o sube archivos para empezar.</p>
        </div>
      ) : viewMode === "grid" ? (
        /* Grid view */
        <div className="grid-view">
          {folders.map((folder) => {
            const fDragData = JSON.stringify({ kind: "folder", id: folder.id, currentPath: folder.parent_path });
            return (
            <div
              key={folder.id}
              className="grid-item"
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData("text/plain", fDragData);
                e.dataTransfer.effectAllowed = "move";
              }}
              onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = "move"; }}
              onDrop={async (e) => {
                e.preventDefault();
                try {
                  const data = JSON.parse(e.dataTransfer.getData("text/plain"));
                  const dest = folder.parent_path === "/" ? "/" + folder.name : folder.parent_path + "/" + folder.name;
                  if (data.kind === "file") {
                    await api.patchFile(data.id, { parent_path: dest });
                  } else {
                    await api.patchFolder(data.id, { parent_path: dest });
                  }
                  toast("Elemento movido", "success");
                  await loadFiles(currentPath, page);
                } catch { toast("Error al mover", "error"); }
              }}
              onClick={() => navigate(joinPath(folder.parent_path, folder.name))}
            >
              <div className="grid-icon folder-big">📁</div>
              <div className="grid-name">{folder.name}</div>
            </div>
          )})}
          {files.map((f) => {
            const fDragData = JSON.stringify({ kind: "file", id: f.id, currentPath: f.parent_path });
            return (
            <div
              key={f.id}
              className={`grid-item ${selected.has(f.id) ? "grid-selected" : ""}`}
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData("text/plain", fDragData);
                e.dataTransfer.effectAllowed = "move";
              }}
              onClick={() => toggleSelect(f.id)}
              onDoubleClick={() => {
                if (isImageMime(f.mime_type) || isVideoMime(f.mime_type)) {
                  setPreviewFile(f);
                } else {
                  window.open(api.downloadUrl(f.id), "_blank");
                }
              }}
            >
              {isImageMime(f.mime_type) ? (
                <img src={api.thumbUrl(f.id, 200)} alt={f.name} className="grid-thumb" />
              ) : isVideoMime(f.mime_type) ? (
                <div className="grid-icon">🎥</div>
              ) : (
                <div className="grid-icon">📄</div>
              )}
              <div className="grid-name">{f.name}</div>
            </div>
          )})}
        </div>
      ) : (
        /* List view */
        <>
          <div className="file-table">
            <SortControls field={sortField} dir={sortDir} onChange={(f, d) => { setSortField(f); setSortDir(d); }} />

            {getSortedItems().map((item: any) => {
              const dragData = JSON.stringify({ kind: item._kind, id: item.id, currentPath: item.parent_path });
              const isFolder = item._kind === "folder";
              return (
              <div
                className={`file-row ${selected.has(item.id) ? "row-selected" : ""}`}
                key={item.id}
                draggable
                onDragStart={(e) => {
                  e.dataTransfer.setData("text/plain", dragData);
                  e.dataTransfer.effectAllowed = "move";
                }}
                onDragOver={isFolder ? (e) => { e.preventDefault(); e.dataTransfer.dropEffect = "move"; } : undefined}
                onDrop={isFolder ? async (e) => {
                  e.preventDefault();
                  try {
                    const data = JSON.parse(e.dataTransfer.getData("text/plain"));
                    const dest = item.parent_path === "/" ? "/" + item.name : item.parent_path + "/" + item.name;
                    if (data.kind === "file") {
                      await api.patchFile(data.id, { parent_path: dest });
                    } else {
                      await api.patchFolder(data.id, { parent_path: dest });
                    }
                    toast("Elemento movido", "success");
                    await loadFiles(currentPath, page);
                  } catch { toast("Error al mover", "error"); }
                } : undefined}
              >
                <div className="file-name">
                  {selected.size > 0 || true ? (
                    <input
                      type="checkbox"
                      className="file-checkbox"
                      checked={selected.has(item.id)}
                      onChange={() => toggleSelect(item.id)}
                      onClick={(e) => e.stopPropagation()}
                    />
                  ) : null}
                  <span className="file-icon">
                    {item._kind === "folder" ? "📁" : item.mime_type?.startsWith("video/") ? "🎥" : item.is_image ? "🖼️" : "📄"}
                  </span>
                  {item._kind === "folder" ? (
                    renaming?.id === item.id ? (
                      <RenameInline
                        initial={item.name}
                        onConfirm={onRenameConfirm}
                        onCancel={() => setRenaming(null)}
                      />
                    ) : (
                      <button
                        className="folder-link"
                        onClick={() => navigate(joinPath(item.parent_path, item.name))}
                      >
                        {item.name}
                      </button>
                    )
                  ) : (
                    renaming?.id === item.id ? (
                      <RenameInline
                        initial={item.name}
                        onConfirm={onRenameConfirm}
                        onCancel={() => setRenaming(null)}
                      />
                    ) : (
                      <span
                        className="file-link"
                        onClick={() => {
                          if (isImageMime(item.mime_type) || isVideoMime(item.mime_type)) {
                            setPreviewFile(item);
                          }
                        }}
                        style={{ cursor: isImageMime(item.mime_type) || isVideoMime(item.mime_type) ? "pointer" : "default" }}
                      >
                        {item.name}
                      </span>
                    )
                  )}
                </div>
                <div className="muted file-mime">{item.mime_type === "inode/directory" ? "Carpeta" : item.mime_type}</div>
                <div>{item._kind === "folder" ? "—" : formatSize(item.size_bytes)}</div>
                <div className="muted">{formatDate(item.created_at)}</div>
                <div className="file-actions">
                  {item._kind === "file" && (
                    <a
                      className="btn btn-ghost btn-sm"
                      href={api.downloadUrl(item.id)}
                      target="_blank"
                      rel="noreferrer"
                    >
                      Descargar
                    </a>
                  )}
                  <button
                    className="btn btn-ghost btn-sm"
                    onClick={() =>
                      setRenaming(item._kind === "folder"
                        ? { kind: "folder", id: item.id, name: item.name }
                        : { kind: "file", id: item.id, name: item.name }
                      )
                    }
                  >
                    Renombrar
                  </button>
                  <button
                    className="btn btn-ghost btn-sm"
                    onClick={() =>
                      setMoving(item._kind === "folder"
                        ? { kind: "folder", id: item.id, name: item.name, parentPath: item.parent_path }
                        : { kind: "file", id: item.id, name: item.name, parentPath: item.parent_path }
                      )
                    }
                  >
                    Mover
                  </button>
                  <button
                    className="btn btn-ghost btn-sm"
                    onClick={() =>
                      setShareItem({ id: item.id, name: item.name, kind: item._kind === "folder" ? "folder" : "file" })
                    }
                  >
                    Compartir
                  </button>
                  <button
                    className="btn btn-danger btn-sm"
                    onClick={() =>
                      item._kind === "folder"
                        ? onDeleteFolder(item.id, item.name)
                        : onDeleteFile(item.id, item.name)
                    }
                  >
                    Eliminar
                  </button>
                </div>
              </div>
            )})}
          </div>

          {/* Pagination */}
          {totalItems > pageSize && (
            <div className="pagination">
              <button
                className="btn btn-ghost btn-sm"
                disabled={page === 0}
                onClick={() => setPage((p) => Math.max(0, p - 1))}
              >
                {"←"} Anterior
              </button>
              <span className="muted">
                Página {page + 1} de {Math.ceil(totalItems / pageSize)}
              </span>
              <button
                className="btn btn-ghost btn-sm"
                disabled={(page + 1) * pageSize >= totalItems}
                onClick={() => setPage((p) => p + 1)}
              >
                Siguiente {"→"}
              </button>
            </div>
          )}
        </>
      )}

      {/* Move modal */}
      {moving && (
        <MoveModal
          label={moving.name}
          currentPath={moving.parentPath}
          onConfirm={onMoveConfirm}
          onClose={() => setMoving(null)}
        />
      )}

      {/* Batch move modal */}
      {batchMoveTarget && (
        <MoveModal
          label={`${selected.size} elemento(s)`}
          currentPath={currentPath}
          onConfirm={async (dest) => {
            setBatchMoveTarget(false);
            let ok = 0;
            for (const id of selected) {
              const file = files.find((f) => f.id === id);
              try {
                if (file) {
                  await api.patchFile(id, { parent_path: dest });
                } else {
                  await api.patchFolder(id, { parent_path: dest });
                }
                ok++;
              } catch {}
            }
            setSelected(new Set());
            toast(`${ok} elemento(s) movido(s)`, "success");
            await loadFiles(currentPath, page);
          }}
          onClose={() => setBatchMoveTarget(false)}
        />
      )}

      {/* Preview modal */}
      {previewFile && (
        <PreviewModal file={previewFile} onClose={() => setPreviewFile(null)} />
      )}

      {/* Create folder modal */}
      {showNewFolderModal && (
        <CreateFolderModal
          onConfirm={onNewFolder}
          onClose={() => setShowNewFolderModal(false)}
        />
      )}

      {/* Share dialog */}
      {shareItem && (
        <ShareDialog
          itemId={shareItem.id}
          itemName={shareItem.name}
          itemType={shareItem.kind}
          onClose={() => setShareItem(null)}
        />
      )}
    </div>
  );
}
