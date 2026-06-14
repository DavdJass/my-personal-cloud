import { useState, useRef } from "react";
import { getToken } from "../api";

export function BackupPage() {
  const [createPasskey, setCreatePasskey] = useState("");
  const [useCreatePasskey, setUseCreatePasskey] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createMsg, setCreateMsg] = useState("");

  const [restoreFile, setRestoreFile] = useState<File | null>(null);
  const [restorePasskey, setRestorePasskey] = useState("");
  const [useRestorePasskey, setUseRestorePasskey] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [restoreMsg, setRestoreMsg] = useState("");
  const [restoreError, setRestoreError] = useState("");

  const fileRef = useRef<HTMLInputElement>(null);

  const authHeaders = () => {
    const t = getToken();
    return t ? { Authorization: `Bearer ${t}` } : {};
  };

  // ── Create backup ──
  const createBackup = async () => {
    setCreating(true);
    setCreateMsg("");
    try {
      const passkey = useCreatePasskey ? createPasskey : "";
      const res = await fetch("/api/backup/create", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...authHeaders(),
        },
        body: JSON.stringify({ passkey }),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: "Error creating backup" }));
        throw new Error(err.error || "Error creating backup");
      }

      // Download the blob as a file.
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "my-cloud-backup.mpcbackup";
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      setCreateMsg("Backup descargado correctamente.");
    } catch (err) {
      setCreateMsg(err instanceof Error ? err.message : "Error creating backup");
    } finally {
      setCreating(false);
    }
  };

  // ── Restore backup ──
  const restoreBackup = async () => {
    if (!restoreFile) return;
    setRestoring(true);
    setRestoreMsg("");
    setRestoreError("");

    try {
      const form = new FormData();
      form.append("file", restoreFile);
      if (useRestorePasskey && restorePasskey) {
        form.append("passkey", restorePasskey);
      }

      const res = await fetch("/api/backup/restore", {
        method: "POST",
        headers: authHeaders(),
        body: form,
      });

      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || "Error restoring backup");
      }

      setRestoreMsg(data.message || "Backup restaurado correctamente.");
      setRestoreFile(null);
      if (fileRef.current) fileRef.current.value = "";
    } catch (err) {
      setRestoreError(err instanceof Error ? err.message : "Error restoring backup");
    } finally {
      setRestoring(false);
    }
  };

  return (
    <div className="page">
      <div className="page-header">
        <h2>Copia de seguridad</h2>
      </div>

      {/* ── Create ── */}
      <div className="backup-card">
        <h3>Crear backup</h3>
        <p className="muted">
          Genera un archivo cifrado con toda tu nube: base de datos y archivos.
        </p>

        <label className="field checkbox-field">
          <input
            type="checkbox"
            checked={useCreatePasskey}
            onChange={(e) => setUseCreatePasskey(e.target.checked)}
          />
          <span>Proteger con passkey</span>
        </label>

        {useCreatePasskey && (
          <label className="field">
            <span>Passkey (mín. 4 caracteres)</span>
            <input
              type="text"
              value={createPasskey}
              onChange={(e) => setCreatePasskey(e.target.value)}
              placeholder="Mi passkey secreta"
              minLength={4}
            />
          </label>
        )}

        <button
          className="btn btn-primary"
          onClick={createBackup}
          disabled={creating || (useCreatePasskey && createPasskey.length < 4)}
        >
          {creating ? "Generando..." : "Descargar backup"}
        </button>

        {createMsg && (
          <div className={creating ? "" : createMsg.includes("Error") ? "error" : "success-msg"}>
            {createMsg}
          </div>
        )}
      </div>

      {/* ── Restore ── */}
      <div className="backup-card">
        <h3>Restaurar backup</h3>
        <p className="muted">
          Sube un archivo <code>.mpcbackup</code> para restaurar tu nube.
          Los datos actuales serán reemplazados.
        </p>

        <div className="backup-warning">
          ⚠️ Al restaurar se reemplazarán todos los archivos y la base de datos actuales.
        </div>

        <label className="field">
          <span>Archivo de backup (.mpcbackup)</span>
          <input
            ref={fileRef}
            type="file"
            accept=".mpcbackup"
            onChange={(e) => setRestoreFile(e.target.files?.[0] ?? null)}
          />
        </label>

        <label className="field checkbox-field">
          <input
            type="checkbox"
            checked={useRestorePasskey}
            onChange={(e) => setUseRestorePasskey(e.target.checked)}
          />
          <span>El backup está protegido con passkey</span>
        </label>

        {useRestorePasskey && (
          <label className="field">
            <span>Passkey</span>
            <input
              type="text"
              value={restorePasskey}
              onChange={(e) => setRestorePasskey(e.target.value)}
              placeholder="Passkey usada al crear el backup"
            />
          </label>
        )}

        <button
          className="btn btn-danger"
          onClick={restoreBackup}
          disabled={restoring || !restoreFile}
        >
          {restoring ? "Restaurando..." : "Restaurar backup"}
        </button>

        {restoreError && <div className="error">{restoreError}</div>}
        {restoreMsg && <div className="success-msg">{restoreMsg}</div>}
      </div>
    </div>
  );
}
