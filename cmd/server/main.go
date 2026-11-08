package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/example/shorty/internal/config"
	"github.com/example/shorty/internal/httpx"
	"github.com/example/shorty/internal/store/postgres"
	"github.com/example/shorty/web"
)

func main() {
	cfg := config.Load()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(context.Background()); err != nil {
		log.Fatal(err)
	}

	store := postgres.New(db)

	// Prune raw click events on startup and hourly in bounded batches.
	// Lifetime click_count on links is intentionally never decremented.
	retentionCtx, cancelRetention := context.WithCancel(context.Background())
	defer cancelRetention()
	go postgres.RunClickRetention(retentionCtx, store, cfg.ClickRetentionDays, cfg.ClickCleanupBatch)

	r := chi.NewRouter()
	h, err := httpx.NewHandlers(cfg, store, web.FS)
	if err != nil {
		log.Fatal(err)
	}
	h.Mount(r)
	defer h.Close()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Shorty listening on :%s (BASE_URL=%s)\n", cfg.Port, cfg.BaseURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
