package routes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/delaneyj/toolbelt"
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

	_, err = js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "games",
		Description: "Datastar Tic Tac Toe Game",
		Compression: true,
		TTL:         time.Hour,
		MaxBytes:    16 * 1024 * 1024,
	})
	if err != nil {
		return cleanup, fmt.Errorf("error creating key value: %w", err)
	}

	_, err = js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "openGames",
		Description: "Datastar Tic Tac Toe Game",
		Compression: true,
		TTL:         time.Hour,
		MaxBytes:    16 * 1024 * 1024,
	})
	if err != nil {
		return cleanup, fmt.Errorf("error creating key value: %w", err)
	}

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

func createSessionId(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {
	sess, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	id := toolbelt.NextEncodedID()
	sess.Values["id"] = id
	if err := sess.Save(r, w); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return id, nil
}

func getSessionId(store sessions.Store, r *http.Request) (string, error) {
	sess, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	id, ok := sess.Values["id"].(string)
	if !ok || id == "" {
		return "", nil // No session ID exists
	}
	return id, nil
}

func deleteSessionId(store sessions.Store, w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, "connections")
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get session: %v", err), http.StatusInternalServerError)
		return
	}
	delete(session.Values, "id")
	if err := session.Save(r, w); err != nil {
		http.Error(w, fmt.Sprintf("failed to save session: %v", err), http.StatusInternalServerError)
		return
	}
}
