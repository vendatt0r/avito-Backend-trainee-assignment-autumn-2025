package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"Backend-trainee-assignment-autumn-2025/internal/handlers"
	"Backend-trainee-assignment-autumn-2025/internal/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("parse dsn: %v", err)
	}

	cfg.MaxConns = 10
	cfg.MaxConnLifetime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		log.Fatalf("open pool: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for {
		err = pool.Ping(ctx)
		if err == nil {
			break
		}
		log.Printf("waiting for db: %v", err)
		time.Sleep(time.Second)
	}

	store := storage.NewStore(pool)

	mux := http.NewServeMux()
	handlers.RegisterHandlers(mux, store)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	log.Printf("listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
