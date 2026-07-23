package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
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
	mux.HandleFunc("GET /cocktails", handleListCocktails(db))
	mux.HandleFunc("GET /cocktails/search", handleSearchCocktails(db))
	mux.HandleFunc("GET /cocktails/{id}", handleGetCocktail(db))
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /openapi.yaml", handleOpenAPISpec)
	mux.HandleFunc("GET /docs", handleDocs)

	server := &http.Server{
		Addr:              config.ListenAddr(),
		Handler:           withSecurityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
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

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
