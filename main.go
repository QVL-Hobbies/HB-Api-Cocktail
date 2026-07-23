package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"hb-api-cocktail/internal/database"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	config := LoadConfig()

	db, err := database.Open(config.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	mux := http.NewServeMux()
	if config.LocalWriteToken != "" {
		if err := os.MkdirAll(config.ImagesDir, 0o755); err != nil {
			return err
		}
		mux.HandleFunc("POST /cocktails", requireLocalWrite(config, handleCreateCocktail(db, config)))
	}
	resolvedImagesDir := resolveImagesDir(config.ImagesDir)
	mux.HandleFunc("GET /cocktails", handleListCocktails(db))
	mux.HandleFunc("GET /cocktails/search", handleSearchCocktails(db))
	mux.HandleFunc("GET /cocktails/{id}", handleGetCocktail(db))
	mux.HandleFunc("GET /ingredients", handleListIngredients(db))
	mux.HandleFunc("GET /categories", handleListCategories(db))
	mux.HandleFunc("GET /tags", handleListTags(db))
	mux.HandleFunc("GET /images/{name}", handleGetImage(resolvedImagesDir))
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /openapi.yaml", handleOpenAPISpec)
	mux.HandleFunc("GET /docs", handleDocs)

	server := &http.Server{
		Addr:              config.ListenAddr(),
		Handler:           withSecurityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("hb-api-cocktail listening on %s", config.ListenAddr())
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func resolveImagesDir(dir string) string {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	resolved, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return absDir
	}
	return resolved
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
