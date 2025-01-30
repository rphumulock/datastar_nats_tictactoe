package routes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/delaneyj/toolbelt/embeddednats"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
)

func SetupRoutes(ctx context.Context, logger *slog.Logger, router chi.Router) (cleanup func() error, err error) {
	natsPort := 1234

	log.Printf("Starting on Nats server %d", natsPort)
	ns, err := embeddednats.New(ctx, embeddednats.WithNATSServerOptions(&server.Options{
		JetStream: true,
		Port:      natsPort,
	}))

	if err != nil {
		return nil, fmt.Errorf("error creating embedded nats server: %w", err)
	}

	ns.WaitForServer()

	cleanup = func() error {
		return errors.Join(
			ns.Close(),
		)
	}

	sessionStore := sessions.NewCookieStore([]byte("session-secret"))
	sessionStore.MaxAge(int(24 * time.Hour / time.Second))

	// Create NATS client
	nc, err := ns.Client()
	if err != nil {
		err = fmt.Errorf("error creating nats client: %w", err)
		return nil, err
	}

	// Access JetStream
	js, err := jetstream.New(nc)
	if err != nil {
		err = fmt.Errorf("error creating nats client: %w", err)
		return nil, err
	}

	// Create or update the "games" KV bucket
	_, err = js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "games",
		Description: "Datastar Tic Tac Toe Game",
		Compression: true,
		TTL:         time.Minute * 20,
		MaxBytes:    16 * 1024 * 1024,
	})
	if err != nil {
		return cleanup, fmt.Errorf("error creating key value: %w", err)
	}

	// Create or update the "users" KV bucket
	_, err = js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "users",
		Description: "Datastar Tic Tac Toe Game",
		Compression: true,
		TTL:         time.Hour,
		MaxBytes:    16 * 1024 * 1024,
	})
	if err != nil {
		return cleanup, fmt.Errorf("error creating key value: %w", err)
	}

	if err := errors.Join(
		setupIndexRoute(router, sessionStore, js),
		setupDashboardRoute(router, sessionStore, js),
		setupGameRoute(router, sessionStore, js),
	); err != nil {
		return cleanup, fmt.Errorf("error setting up routes: %w", err)
	}

	return cleanup, nil
}
