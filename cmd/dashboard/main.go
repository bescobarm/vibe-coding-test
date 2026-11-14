// Command dashboard serves a web page to send random or specific transactions
// to a running engine and watch its metrics live (D-073, D-074). Start the
// engine first (`make run`), then open http://localhost:8090.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bescobarm/card-auth-engine/internal/dashboard"
	"github.com/bescobarm/card-auth-engine/internal/simulator"
)

const (
	defaultEngineURL   = "http://localhost:8080"
	defaultDatabaseURL = "postgres://auth:auth@localhost:55432/auth?sslmode=disable"
	defaultAddr        = ":8090"
	requestTimeout     = 5 * time.Second
	readHeaderTimeout  = 2 * time.Second
	writeTimeout       = 2 * time.Minute // a scenario run can take a while
	shutdownTimeout    = 5 * time.Second
	exitFailure        = 1
)

func main() {
	engineURL := flag.String("engine", defaultEngineURL, "engine base URL")
	databaseURL := flag.String("database", getEnv("DATABASE_URL", defaultDatabaseURL), "engine database, to create scenario cards")
	addr := flag.String("addr", defaultAddr, "address to serve the dashboard on")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	err := run(log, *engineURL, *databaseURL, *addr)
	if err != nil {
		log.Error("dashboard stopped", "err", err)
		os.Exit(exitFailure)
	}
}

func run(log *slog.Logger, engineURL, databaseURL, addr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}

	defer pool.Close()

	client := &http.Client{Timeout: requestTimeout}

	server, err := dashboard.NewServer(dashboard.Config{
		Engine:    simulator.NewHTTPEngine(engineURL, client),
		Cards:     simulator.NewPostgresCards(pool),
		EngineURL: engineURL,
		Client:    client,
		Log:       log,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: readHeaderTimeout, WriteTimeout: writeTimeout}
	errc := make(chan error, 1)

	go func() {
		errc <- srv.ListenAndServe()
	}()

	log.Info("dashboard listening", "url", "http://localhost"+addr, "engine", engineURL)

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		return srv.Shutdown(shutdownCtx)
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value != "" {
		return value
	}

	return fallback
}
