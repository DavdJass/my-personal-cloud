package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/config"
	"github.com/DavdJass/my-personal-cloud/internal/db"
	"github.com/DavdJass/my-personal-cloud/internal/files"
	"github.com/DavdJass/my-personal-cloud/internal/photos"
	"github.com/DavdJass/my-personal-cloud/internal/ratelimit"
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
	defer conn.Close()

	authSvc := auth.NewService(conn, cfg.JWTSecret, cfg.JWTExpiry)

	// Bootstrap an initial admin account from env vars on first run.
	if u, p := os.Getenv("CLOUD_ADMIN_USER"), os.Getenv("CLOUD_ADMIN_PASS"); u != "" && p != "" {
		if err := authSvc.EnsureUser(context.Background(), u, p); err != nil {
			slog.Error("bootstrap admin failed", "error", err)
			os.Exit(1)
		}
		slog.Info("admin user ensured", "username", u)
	}

	store := storage.New(cfg.StorageRoot)
	fileSvc := files.NewService(conn, store, cfg.MaxUploadBytes)
	photoSvc, err := photos.NewService(conn, store, cfg.StorageRoot)
	if err != nil {
		slog.Error("photos service failed", "error", err)
		os.Exit(1)
	}

	// Rate limiter for login endpoint (5 attempts per minute per IP).
	loginLimiter := ratelimit.New(5, 1*time.Minute)
	defer loginLimiter.Stop()

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
			protected.Get("/files/search", fileSvc.SearchHandler)
			protected.Post("/files/upload", fileSvc.UploadHandler)
			protected.Get("/files/{id}/download", fileSvc.DownloadHandler)
			protected.Patch("/files/{id}", fileSvc.PatchFileHandler)
			protected.Delete("/files/{id}", fileSvc.DeleteHandler)
			protected.Post("/files/{id}/restore", fileSvc.RestoreHandler)

			protected.Post("/folders", fileSvc.CreateFolderHandler)
			protected.Patch("/folders/{id}", fileSvc.PatchFolderHandler)
			protected.Delete("/folders/{id}", fileSvc.DeleteFolderHandler)

			protected.Get("/photos", photoSvc.ListHandler)
			protected.Get("/photos/{id}/thumb", photoSvc.ThumbHandler)
			protected.Get("/photos/{id}/full", photoSvc.FullHandler)

			protected.Get("/trash", fileSvc.TrashHandler)
			protected.Post("/trash/empty", fileSvc.EmptyTrashHandler)
		})
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
	<-stop

	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
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
