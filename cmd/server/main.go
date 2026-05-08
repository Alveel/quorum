package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alveel/vacation-coverage/internal/config"
	"github.com/alveel/vacation-coverage/internal/locale"
	"github.com/alveel/vacation-coverage/internal/server"
	"github.com/alveel/vacation-coverage/internal/store"
	"github.com/alveel/vacation-coverage/web"
)

func main() {
	locale.Init()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := config.Load()

	if cfg.DatabaseURL == "" {
		log.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	if cfg.DevAuthBypass && cfg.DevUser == "" {
		log.Error("DEV_AUTH_BYPASS=true requires DEV_USER to be set")
		os.Exit(1)
	}

	if err := waitForDB(cfg.DatabaseURL, 15*time.Second, log); err != nil {
		log.Error("database not reachable", "err", err)
		os.Exit(1)
	}

	log.Info("applying migrations")
	if err := store.RunMigrations(cfg.DatabaseURL); err != nil {
		log.Error("migrations failed", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	// Sub-root the embedded FS at "static/" so the FileServer sees plain filenames.
	staticSub, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		log.Error("static fs sub", "err", err)
		os.Exit(1)
	}

	h := server.New(cfg, st, staticSub)
	addr := ":" + cfg.Port
	log.Info("listening", "addr", addr, "dev_bypass", cfg.DevAuthBypass)
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}

func waitForDB(url string, timeout time.Duration, log *slog.Logger) error {
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			err = pool.Ping(ctx)
			pool.Close()
		}
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		log.Info("waiting for database", "err", err)
		time.Sleep(time.Second)
	}
}
