package routes

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar-nats-tictactoe/web/components"
)

const (
	// playerSessionName is the cookie holding a player's session id.
	playerSessionName = "connections"

	// sessionMaxAge is how long a session survives without activity. It slides:
	// every request re-issues the cookie, so the clock only runs while the tab
	// is idle, and an active player is never logged out mid-game.
	sessionMaxAge = 45 * time.Minute

	// The pages ping /api/session/touch on an interval so a player who is only
	// watching an SSE stream - a single request that stays open for hours - still
	// slides their cookie. That period lives in components.SessionKeepaliveAttrs,
	// which builds the attribute; it must stay comfortably under sessionMaxAge.
)

// slidingSession re-issues the session cookie on every request that carries one.
// It runs as middleware rather than inside the handlers because the Set-Cookie
// header has to be written before the response starts - which for the SSE
// endpoints happens the moment their generator is created.
func slidingSession(store sessions.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, err := store.Get(r, playerSessionName)
			if err == nil {
				if id, ok := session.Values["id"].(string); ok && id != "" {
					session.Options.MaxAge = int(sessionMaxAge / time.Second)
					if err := session.Save(r, w); err != nil {
						log.Printf("session: failed to refresh cookie: %v", err)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// setupSessionRoute serves the keepalive the open pages ping. The cookie itself
// is already slid by the middleware; this handler's job is to keep the user
// record from expiring out from under a player who is still here.
func setupSessionRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	usersKV, err := js.KeyValue(ctx, "users")
	if err != nil {
		return fmt.Errorf("failed to get users key value: %w", err)
	}

	handleTouch := func(w http.ResponseWriter, r *http.Request) {
		sessionId, err := getSessionId(store, r)
		if err != nil || sessionId == "" {
			// Nothing to refresh. The page will find out on its next navigation.
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Re-writing the record resets its bucket TTL. Read first so a keepalive
		// never resurrects a user that has already been deleted.
		user, _, err := GetObject[components.User](r.Context(), usersKV, sessionId)
		if err != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := PutData(r.Context(), usersKV, sessionId, user); err != nil {
			log.Printf("session: failed to refresh user %s: %v", sessionId, err)
		}

		w.WriteHeader(http.StatusNoContent)
	}

	router.Post("/api/session/touch", handleTouch)

	return nil
}
