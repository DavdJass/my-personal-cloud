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
}

export interface PhotoEntry {
  id: string;
  name: string;
  mime_type: string;
  size_bytes: number;
  created_at: string;
}

export const api = {
  login: (username: string, password: string) =>
    request<LoginResponse>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),

  me: () => request<User>("/api/auth/me"),

  listFiles: (path = "/") =>
    request<{ path: string; files: FileEntry[] }>(
      `/api/files?path=${encodeURIComponent(path)}`
    ),

  uploadFile: (file: File, parentPath = "/") => {
    const fd = new FormData();
    fd.append("file", file);
    fd.append("path", parentPath);
    return request<FileEntry>("/api/files/upload", {
      method: "POST",
      body: fd,
    });
  },

  deleteFile: (id: string) =>
    request<void>(`/api/files/${id}`, { method: "DELETE" }),

  downloadUrl: (id: string) => {
    const token = getToken() ?? "";
    return `/api/files/${id}/download?token=${encodeURIComponent(token)}`;
  },

  listPhotos: () =>
    request<{ photos: PhotoEntry[] }>("/api/photos"),

  thumbUrl: (id: string, size = 256) => {
    const token = getToken() ?? "";
    return `/api/photos/${id}/thumb?size=${size}&token=${encodeURIComponent(token)}`;
  },

  fullUrl: (id: string) => {
    const token = getToken() ?? "";
    return `/api/photos/${id}/full?token=${encodeURIComponent(token)}`;
  },
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
