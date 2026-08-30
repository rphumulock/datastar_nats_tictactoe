package routes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/delaneyj/toolbelt/embeddednats"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
)

func SetupRoutes(
	ctx context.Context,
	logger *slog.Logger,
	router chi.Router,
) (cleanup func() error, err error) {
	natsPort := 1234

	log.Printf("Starting on Nats server %d", natsPort)
	ns, err := embeddednats.New(ctx, embeddednats.WithNATSServerOptions(&server.Options{
		JetStream: true,
		Port:      natsPort,
		// Only this process talks to the embedded server, so don't expose it
		// on every interface.
		Host: "127.0.0.1",
		// The embedded server installs its own SIGINT/SIGTERM handlers by
		// default, which race with the signal.NotifyContext in main.
		NoSigs: true,
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
	// Keep the session cookie out of reach of JavaScript, and stop it riding
	// along on cross-site requests (gorilla defaults to SameSite=None).
	sessionStore.Options.HttpOnly = true
	sessionStore.Options.SameSite = http.SameSiteLaxMode

	nc, err := ns.Client()
	if err != nil {
		err = fmt.Errorf("error creating nats client: %w", err)
		return nil, err
	}

	js, err := jetstream.New(nc)
	if err != nil {
		err = fmt.Errorf("error creating nats client: %w", err)
		return nil, err
	}

	createKeyValueBuckets := func(ctx context.Context, js jetstream.JetStream) error {
		createBucket := func(bucket, desc string, ttl time.Duration) error {
			_, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
				Bucket:      bucket,
				Description: desc,
				Compression: true,
				TTL:         ttl,
				MaxBytes:    16 * 1024 * 1024,
				History:     2,
			})
			if err != nil {
				return fmt.Errorf("error creating bucket %q: %w", bucket, err)
			}
			return nil
		}

		if err := createBucket("gameLobbies", "Datastar Tic Tac Toe Game", time.Hour); err != nil {
			return err
		}
		if err := createBucket("gameBoards", "Datastar Tic Tac Toe Game", time.Hour); err != nil {
			return err
		}
		if err := createBucket("users", "Datastar Tic Tac Toe Game", time.Hour); err != nil {
			return err
		}
		// Presence is a liveness signal, not stored state: the short TTL is the
		// whole mechanism, expiring a session's key once its browser stops
		// heartbeating so abandoned lobbies can be found.
		if err := createBucket("presence", "Connected sessions", presenceTTL); err != nil {
			return err
		}
		return nil
	}

	if err := createKeyValueBuckets(ctx, js); err != nil {
		return cleanup, err
	}

	gameLobbiesKV, err := js.KeyValue(ctx, "gameLobbies")
	if err != nil {
		return cleanup, fmt.Errorf("error getting game lobbies bucket: %w", err)
	}
	gameBoardsKV, err := js.KeyValue(ctx, "gameBoards")
	if err != nil {
		return cleanup, fmt.Errorf("error getting game boards bucket: %w", err)
	}
	presenceKV, err := js.KeyValue(ctx, "presence")
	if err != nil {
		return cleanup, fmt.Errorf("error getting presence bucket: %w", err)
	}
	startReaper(ctx, gameLobbiesKV, gameBoardsKV, presenceKV)

	// A Group rather than router.Use: chi refuses middleware added after a route
	// is mounted, and main registers the static handler before calling this.
	var setupErr error
	router.Group(func(appRouter chi.Router) {
		// Every app route re-issues the session cookie, so the idle clock only
		// runs while the player is actually idle.
		appRouter.Use(slidingSession(sessionStore))

		setupErr = errors.Join(
			setupIndexRoute(appRouter, sessionStore, js),
			setupDashboardRoute(appRouter, sessionStore, js),
			setupGameRoute(appRouter, sessionStore, js),
			setupAdminRoute(appRouter, sessionStore, js),
			setupSessionRoute(appRouter, sessionStore, js),
		)
	})
	if setupErr != nil {
		return cleanup, fmt.Errorf("error setting up routes: %w", setupErr)
	}

	return cleanup, nil
}
