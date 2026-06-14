package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/backup"
	"github.com/DavdJass/my-personal-cloud/internal/config"
	"github.com/DavdJass/my-personal-cloud/internal/db"
	"github.com/DavdJass/my-personal-cloud/internal/files"
	"github.com/DavdJass/my-personal-cloud/internal/photos"
	"github.com/DavdJass/my-personal-cloud/internal/ratelimit"
	"github.com/DavdJass/my-personal-cloud/internal/shares"
	"github.com/DavdJass/my-personal-cloud/internal/storage"
	"github.com/DavdJass/my-personal-cloud/web"
)

func main() {
	// Structured logging.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	conn, err := db.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("database open failed", "error", err)
		os.Exit(1)
	}

	// Wrap DB in a holder so the backup service can replace it transparently.
	dbh := backup.NewDBHolder(conn)

	authSvc := auth.NewService(dbh.DB(), cfg.JWTSecret, cfg.JWTExpiry)

	// Bootstrap an initial admin account from env vars on first run.
	if u, p := os.Getenv("CLOUD_ADMIN_USER"), os.Getenv("CLOUD_ADMIN_PASS"); u != "" && p != "" {
		if err := authSvc.EnsureUser(context.Background(), u, p); err != nil {
			slog.Error("bootstrap admin failed", "error", err)
			os.Exit(1)
		}
		slog.Info("admin user ensured", "username", u)
	}

	store := storage.New(cfg.StorageRoot)
	fileSvc := files.NewService(dbh.DB(), store, cfg.MaxUploadBytes)
	photoSvc, err := photos.NewService(dbh.DB(), store, cfg.StorageRoot)
	if err != nil {
		slog.Error("photos service failed", "error", err)
		os.Exit(1)
	}
	shareSvc := shares.NewService(dbh.DB(), store)
	backupSvc := backup.NewService(dbh, store, cfg.JWTSecret, cfg.DatabasePath)

	// Restart channel: when backup restore completes, the server restarts
	// so all services pick up the new database connection.
	restartCh := make(chan struct{}, 1)
	backupSvc.RestartCh = restartCh

	// Rate limiter for login endpoint (5 attempts per minute per IP).
	loginLimiter := ratelimit.New(5, 1*time.Minute)
	defer loginLimiter.Stop()

	// Rate limiter for general API endpoints (60 requests per minute per IP).
	apiLimiter := ratelimit.New(60, 1*time.Minute)
	defer apiLimiter.Stop()

	// Rate limiter for file uploads (10 uploads per minute per IP).
	uploadLimiter := ratelimit.New(10, 1*time.Minute)
	defer uploadLimiter.Stop()

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.AllowOrigin},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Route("/api", func(api chi.Router) {
		api.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})

		// Login with rate limiting.
		api.With(loginLimiter.Middleware).Post("/auth/login", authSvc.LoginHandler)

		api.Group(func(protected chi.Router) {
			protected.Use(authSvc.Middleware)

			protected.Get("/auth/me", authSvc.MeHandler)
			protected.Post("/auth/refresh", authSvc.RefreshHandler)

			protected.Get("/files", fileSvc.ListHandler)
			protected.With(apiLimiter.Middleware).Get("/files/search", fileSvc.SearchHandler)
			protected.With(uploadLimiter.Middleware).Post("/files/upload", fileSvc.UploadHandler)
			protected.Get("/files/{id}/download", fileSvc.DownloadHandler)
			protected.With(apiLimiter.Middleware).Patch("/files/{id}", fileSvc.PatchFileHandler)
			protected.With(apiLimiter.Middleware).Delete("/files/{id}", fileSvc.DeleteHandler)
			protected.Post("/files/{id}/restore", fileSvc.RestoreHandler)

			protected.With(apiLimiter.Middleware).Post("/folders", fileSvc.CreateFolderHandler)
			protected.With(apiLimiter.Middleware).Patch("/folders/{id}", fileSvc.PatchFolderHandler)
			protected.With(apiLimiter.Middleware).Delete("/folders/{id}", fileSvc.DeleteFolderHandler)

			protected.Get("/photos", photoSvc.ListHandler)
			protected.Get("/photos/{id}/thumb", photoSvc.ThumbHandler)
			protected.Get("/photos/{id}/full", photoSvc.FullHandler)

			protected.With(apiLimiter.Middleware).Get("/trash", fileSvc.TrashHandler)
			protected.With(apiLimiter.Middleware).Post("/trash/empty", fileSvc.EmptyTrashHandler)

			protected.Post("/shares", shareSvc.CreateHandler)
			protected.Get("/shares", shareSvc.ListHandler)
			protected.Delete("/shares/{id}", shareSvc.RevokeHandler)

			// Backup (authenticated).
			protected.Post("/backup/create", backupSvc.CreateHandler)
			protected.Post("/backup/restore", backupSvc.RestoreHandler)
		})
	})

	// Public share route (no auth required) — under /api/public so the SPA
	// can handle the /share/:token UI route without conflicts.
	r.Route("/api/public", func(public chi.Router) {
		public.Get("/share/{token}", shareSvc.PublicShareRouter)
	})

	// Serve the embedded React build for any non-/api path.
	uiFS, err := web.UI()
	if err != nil {
		slog.Warn("frontend not embedded, API-only mode", "error", err)
	} else {
		r.Handle("/*", spaHandler(uiFS))
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		slog.Info("server starting",
			"addr", cfg.Addr,
			"storage", cfg.StorageRoot,
			"database", cfg.DatabasePath,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-stop:
		slog.Info("shutting down...")
	case <-restartCh:
		slog.Info("restarting server...")
		// Shutdown the HTTP server and DB gracefully, then re-exec.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
		if err := dbh.DB().Close(); err != nil {
			slog.Error("close database", "error", err)
		}
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			slog.Error("restart failed", "error", err)
			os.Exit(1)
		}
		slog.Info("restarted — new PID", "pid", cmd.Process.Pid)
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if err := dbh.DB().Close(); err != nil {
		slog.Error("close database", "error", err)
	}
	slog.Info("server stopped")
}

// spaHandler serves static files from fsys and falls back to index.html for
// any path that does not exist on disk so client-side routing works.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(fsys, path); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
