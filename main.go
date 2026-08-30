package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rphumulock/datastar_nats_tictactoe/routes"
	"golang.org/x/sync/errgroup"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	getPort := func() string {
		if p, ok := os.LookupEnv("PORT"); ok {
			return p
		}
		return "8080"
	}
	logger.Info(fmt.Sprintf("Starting Server 0.0.0.0:%s", getPort()))
	defer logger.Info("Stopping Server")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, getPort()); err != nil {
		logger.Error("Error running server", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, port string) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(startServer(ctx, logger, port))

	if err := g.Wait(); err != nil {
		return fmt.Errorf("error running server: %w", err)
	}

	return nil
}

func startServer(ctx context.Context, logger *slog.Logger, port string) func() error {
	return func() error {
		router := chi.NewMux()

		router.Use(
			middleware.Logger,
			middleware.Recoverer,
		)

		router.Handle("/static/*", http.StripPrefix("/static/", static(logger)))

		// SetupRoutes returns a nil cleanup when it fails before the NATS server
		// exists, so guard the defer rather than calling a nil func on return.
		cleanup, err := routes.SetupRoutes(ctx, logger, router)
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			return fmt.Errorf("error setting up routes: %w", err)
		}

		srv := &http.Server{
			Addr:    "0.0.0.0:" + port,
			Handler: router,
			// Bound how long a slow or idle client can hold a connection.
			// No WriteTimeout: it would cut off the long-lived SSE streams.
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()

		// ListenAndServe always returns a non-nil error. ErrServerClosed is the
		// expected result of a graceful shutdown, not a failure.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
