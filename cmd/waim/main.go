// Command waim connects to a Jellyfin server, compares its libraries against
// TMDB and reports missing seasons, episodes and movie-collection entries
// through a small web dashboard.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // embed the timezone database so TZ works on any base image

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/server"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
	"github.com/daknoblo/waim/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-healthcheck" || os.Args[1] == "healthcheck") {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// healthcheck performs a local request to /healthz and returns a process exit
// code. It is used as the container HEALTHCHECK command (the distroless image
// has no shell or curl).
func healthcheck() int {
	addr := envDefault("WAIM_ADDR", ":8080")
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run() error {
	// Logging: structured stdout output plus an in-memory ring buffer for the UI.
	// The level is held in an slog.LevelVar so it can be changed at runtime from
	// the settings page.
	logBuf := logbuf.New(300)
	levelVar := new(slog.LevelVar)
	levelVar.Set(slog.LevelInfo)
	if envBool("WAIM_DEBUG") {
		levelVar.Set(slog.LevelDebug)
	}
	base := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: levelVar})
	logger := slog.New(logbuf.NewHandler(base, logBuf))
	slog.SetDefault(logger)

	logger.Info("starting waim", "version", version.Get().String())

	dataDir := config.DataDir()
	masterKey := os.Getenv("WAIM_MASTER_KEY")
	if strings.TrimSpace(masterKey) == "" {
		logger.Warn("WAIM_MASTER_KEY is not set; API keys cannot be stored until it is configured")
	}

	cfg, err := config.Load(dataDir, masterKey)
	if err != nil {
		return err
	}
	// Apply the configured log level (overrides the startup default).
	levelVar.Set(config.ParseLogLevel(cfg.Get().LogLevel))
	logger.Info("configuration loaded", "dataDir", dataDir, "encryption", cfg.CipherEnabled(), "logLevel", cfg.Get().LogLevel)

	st, err := store.Open(filepath.Join(dataDir, "waim.db"))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	catalog, err := i18n.Load()
	if err != nil {
		return err
	}

	sched := scheduler.New(cfg, st, logger)
	suggestSvc := suggest.New(cfg, st, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go sched.Run(ctx)

	srv := server.New(cfg, st, sched, suggestSvc, logBuf, catalog, logger, levelVar)
	httpServer := &http.Server{
		Addr:              envDefault("WAIM_ADDR", ":8080"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
