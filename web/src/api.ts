// Thin API client. Uses the JWT stored in localStorage and surfaces a
// uniform error type so the UI can render messages without parsing fetch
// results in every component.

const TOKEN_KEY = "cloud.token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string | null) {
  if (token) localStorage.setItem(TOKEN_KEY, token);
  else localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

async function request<T>(
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const headers = new Headers(init.headers);
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (!(init.body instanceof FormData) && init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const res = await fetch(path, { ...init, headers });

  if (res.status === 204) {
    return undefined as T;
  }

  const contentType = res.headers.get("Content-Type") ?? "";
  const isJSON = contentType.includes("application/json");
  const payload = isJSON ? await res.json() : await res.text();

  if (!res.ok) {
    const msg =
      (isJSON && (payload as { error?: string }).error) ||
      `Request failed (${res.status})`;
    throw new ApiError(msg, res.status);
  }
  return payload as T;
}

export interface User {
  id: number;
  username: string;
}

export interface LoginResponse {
  token: string;
  user: User;
  expires_in_seconds: number;
}

export interface FileEntry {
  id: string;
  name: string;
  parent_path: string;
  mime_type: string;
  size_bytes: number;
  is_image: boolean;
  created_at: string;
  deleted_at?: string;
}

export interface FolderEntry {
  id: string;
  name: string;
  parent_path: string;
  created_at: string;
}

export interface PhotoEntry {
  id: string;
  name: string;
  mime_type: string;
  size_bytes: number;
  created_at: string;
}

export interface ListResponse {
  path?: string;
  folders: FolderEntry[];
  files: FileEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface SearchResponse {
  files: FileEntry[];
  folders: FolderEntry[];
  limit: number;
  offset: number;
}

export interface PhotosResponse {
  photos: PhotoEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface TrashResponse {
  files: FileEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface ShareLink {
  id: string;
  user_id: number;
  file_id?: string;
  folder_id?: string;
  url: string;
  expires_at: string;
  max_views: number;
  current_views: number;
  is_active: boolean;
  created_at: string;
  name: string;
}

export const api = {
  login: (username: string, password: string) =>
    request<LoginResponse>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),

  me: () => request<User>("/api/auth/me"),

  refresh: () =>
    request<LoginResponse>("/api/auth/refresh", { method: "POST" }),

  listFiles: (path = "/", limit = 50, offset = 0) =>
    request<ListResponse>(
      `/api/files?path=${encodeURIComponent(path)}&limit=${limit}&offset=${offset}`
    ),

  search: (q: string, limit = 50, offset = 0) =>
    request<SearchResponse>(
      `/api/files/search?q=${encodeURIComponent(q)}&limit=${limit}&offset=${offset}`
    ),

  uploadFile: (file: File, parentPath = "/", onProgress?: (pct: number) => void) => {
    return new Promise<FileEntry>((resolve, reject) => {
      const fd = new FormData();
      fd.append("file", file);
      fd.append("path", parentPath);

      const token = getToken();
      const xhr = new XMLHttpRequest();
      xhr.open("POST", "/api/files/upload");

      if (token) xhr.setRequestHeader("Authorization", `Bearer ${token}`);

      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && onProgress) {
          onProgress(Math.round((e.loaded / e.total) * 100));
        }
      };

      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(JSON.parse(xhr.responseText));
        } else {
          let msg = `Upload failed (${xhr.status})`;
          try {
            const body = JSON.parse(xhr.responseText);
            msg = body.error || msg;
          } catch {}
          reject(new ApiError(msg, xhr.status));
        }
      };

      xhr.onerror = () => reject(new ApiError("Network error", 0));
      xhr.send(fd);
    });
  },

  patchFile: (id: string, patch: { name?: string; parent_path?: string }) =>
    request<FileEntry>(`/api/files/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),

  deleteFile: (id: string) =>
    request<void>(`/api/files/${id}`, { method: "DELETE" }),

  restoreFile: (id: string) =>
    request<void>(`/api/files/${id}/restore`, { method: "POST" }),

  downloadUrl: (id: string) => {
    const token = getToken() ?? "";
    return `/api/files/${id}/download?token=${encodeURIComponent(token)}`;
  },

  streamUrl: (id: string) => {
    const token = getToken() ?? "";
    return `/api/files/${id}/download?inline=1&token=${encodeURIComponent(token)}`;
  },

  createFolder: (name: string, parentPath = "/") =>
    request<FolderEntry>("/api/folders", {
      method: "POST",
      body: JSON.stringify({ name, parent_path: parentPath }),
    }),

  patchFolder: (id: string, patch: { name?: string; parent_path?: string }) =>
    request<FolderEntry>(`/api/folders/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),

  deleteFolder: (id: string) =>
    request<void>(`/api/folders/${id}`, { method: "DELETE" }),

  listPhotos: (limit = 50, offset = 0) =>
    request<PhotosResponse>(
      `/api/photos?limit=${limit}&offset=${offset}`
    ),

  thumbUrl: (id: string, size = 256) => {
    const token = getToken() ?? "";
    return `/api/photos/${id}/thumb?size=${size}&token=${encodeURIComponent(token)}`;
  },

  fullUrl: (id: string) => {
    const token = getToken() ?? "";
    return `/api/photos/${id}/full?token=${encodeURIComponent(token)}`;
  },

  listTrash: (limit = 50, offset = 0) =>
    request<TrashResponse>(`/api/trash?limit=${limit}&offset=${offset}`),

  emptyTrash: () =>
    request<void>("/api/trash/empty", { method: "POST" }),

  // ── Shares ──────────────────────────────────────────────────────────

  createShare: (opts: {
    file_id?: string;
    folder_id?: string;
    expires_in_hours?: number;
    max_views?: number;
  }) => request<ShareLink>("/api/shares", {
    method: "POST",
    body: JSON.stringify({
      file_id: opts.file_id,
      folder_id: opts.folder_id,
      expires_in_hours: opts.expires_in_hours ?? 1,
      max_views: opts.max_views ?? 0,
    }),
  }),

  listShares: () =>
    request<{ shares: ShareLink[] }>("/api/shares"),

  revokeShare: (id: string) =>
    request<void>(`/api/shares/${id}`, { method: "DELETE" }),
};

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let n = bytes / 1024;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(1)} ${units[i]}`;
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString();
}
