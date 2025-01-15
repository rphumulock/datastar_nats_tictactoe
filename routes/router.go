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
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
)

func SetupRoutes(ctx context.Context, logger *slog.Logger, router chi.Router) (cleanup func() error, err error) {
	natsPort, err := 33823, nil
	if err != nil {
		return nil, fmt.Errorf("error getting free port: %w", err)
	}

	log.Printf("Starting on Nats server %d", natsPort)

	ns, err := embeddednats.New(ctx, embeddednats.WithNATSServerOptions(&natsserver.Options{
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
		fmt.Errorf("error creating nats client: %w", err)
	}

	// Access JetStream
	js, err := jetstream.New(nc)
	if err != nil {
		fmt.Errorf("error creating jetstream client: %w", err)
	}

	if err := errors.Join(
		setupIndexRoute(router, sessionStore, js),
		setupGameRoute(router, sessionStore, js),
	); err != nil {
		return cleanup, fmt.Errorf("error setting up routes: %w", err)
	}

	return cleanup, nil
}
