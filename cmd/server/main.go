package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/DavdJass/my-personal-cloud/internal/ai"
	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/config"
	"github.com/DavdJass/my-personal-cloud/internal/db"
	"github.com/DavdJass/my-personal-cloud/internal/files"
	"github.com/DavdJass/my-personal-cloud/internal/photos"
	"github.com/DavdJass/my-personal-cloud/internal/storage"
	"github.com/DavdJass/my-personal-cloud/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	conn, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()

	authSvc := auth.NewService(conn, cfg.JWTSecret, cfg.JWTExpiry)

	// Bootstrap an initial admin account from env vars on first run. Both
	// CLOUD_ADMIN_USER and CLOUD_ADMIN_PASS must be set; otherwise the user
	// is expected to create accounts manually.
	if u, p := os.Getenv("CLOUD_ADMIN_USER"), os.Getenv("CLOUD_ADMIN_PASS"); u != "" && p != "" {
		if err := authSvc.EnsureUser(context.Background(), u, p); err != nil {
			log.Fatalf("bootstrap admin: %v", err)
		}
		log.Printf("ensured user %q exists", u)
	}

	store := storage.New(cfg.StorageRoot)
	fileSvc := files.NewService(conn, store, cfg.MaxUploadBytes)
	photoSvc, err := photos.NewService(conn, store, cfg.StorageRoot)
	if err != nil {
		log.Fatalf("photos: %v", err)
	}

	// AI module is fully opt-in. Without DEEPSEEK_API_KEY set, the cloud runs
	// exactly as before — the AI routes simply aren't registered and the
	// frontend's /api/ai/status reports enabled=false.
	var aiSvc *ai.Service
	aiKey := os.Getenv("DEEPSEEK_API_KEY")
	if aiKey != "" {
		client := ai.NewClient(aiKey, os.Getenv("DEEPSEEK_BASE_URL"), os.Getenv("DEEPSEEK_EMBED_MODEL"))
		aiSvc = ai.NewService(conn, store, client)
		log.Printf("AI search enabled (model: %s)", aiSvc.Model())
	} else {
		log.Printf("AI search disabled (set DEEPSEEK_API_KEY to enable)")
	}

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

	r.Route("/api", func(apiR chi.Router) {
		apiR.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})

		apiR.Post("/auth/login", authSvc.LoginHandler)

		// Public AI status: tells the frontend whether to render search UI.
		aiModel := ""
		if aiSvc != nil {
			aiModel = aiSvc.Model()
		}
		apiR.Get("/ai/status", ai.StatusHandler(aiSvc != nil, aiModel))

		apiR.Group(func(protected chi.Router) {
			protected.Use(authSvc.Middleware)

			protected.Get("/auth/me", authSvc.MeHandler)

			protected.Get("/files", fileSvc.ListHandler)
			protected.Post("/files/upload", fileSvc.UploadHandler)
			protected.Get("/files/{id}/download", fileSvc.DownloadHandler)
			protected.Patch("/files/{id}", fileSvc.PatchFileHandler)
			protected.Delete("/files/{id}", fileSvc.DeleteHandler)

			protected.Post("/folders", fileSvc.CreateFolderHandler)
			protected.Patch("/folders/{id}", fileSvc.PatchFolderHandler)
			protected.Delete("/folders/{id}", fileSvc.DeleteFolderHandler)

			protected.Get("/photos", photoSvc.ListHandler)
			protected.Get("/photos/{id}/thumb", photoSvc.ThumbHandler)
			protected.Get("/photos/{id}/full", photoSvc.FullHandler)

			// Only registered when AI is configured. Frontend hides these
			// actions behind /api/ai/status, but the routes simply 404 if
			// the user hits them directly without a key configured.
			if aiSvc != nil {
				protected.Post("/ai/index/{id}", aiSvc.IndexHandler)
				protected.Delete("/ai/index/{id}", aiSvc.RemoveIndexHandler)
				protected.Get("/ai/search", aiSvc.SearchHandler)
			}
		})
	})

	// Serve the embedded React build for any non-/api path. The frontend is
	// a single-page app, so unknown routes fall back to index.html.
	uiFS, err := web.UI()
	if err != nil {
		log.Printf("frontend not embedded yet: %v", err)
	} else {
		r.Handle("/*", spaHandler(uiFS))
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadHeaderTimeout: 15 * time.Second,
		// No WriteTimeout: large downloads/uploads must be allowed to take
		// arbitrary time on a home network.
	}

	go func() {
		log.Printf("personal cloud listening on %s (storage=%s, db=%s)", cfg.Addr, cfg.StorageRoot, cfg.DatabasePath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Printf("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
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
