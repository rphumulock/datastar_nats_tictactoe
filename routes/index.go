package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	datastar "github.com/starfederation/datastar/code/go/sdk"

	"github.com/delaneyj/toolbelt"
	"github.com/delaneyj/toolbelt/embeddednats"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/zangster300/northstar/web/components"
	"github.com/zangster300/northstar/web/pages"
)

type User struct {
	SessionID string `json:"session_id"` // Unique session ID for the user
	GameID    string `json:"game_id"`    // List of games the user is in
}

func setupIndexRoute(router chi.Router, store sessions.Store, ns *embeddednats.Server) error {
	// Create NATS client
	nc, err := ns.Client()
	if err != nil {
		return fmt.Errorf("error creating nats client: %w", err)
	}

	// Access JetStream
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("error creating jetstream client: %w", err)
	}

	// Create or update the "games" KV bucket
	gamesKV, err := js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "games",
		Description: "Datastar Tic Tac Toe Game",
		Compression: true,
		TTL:         time.Hour,
		MaxBytes:    16 * 1024 * 1024,
	})
	if err != nil {
		return fmt.Errorf("error creating key value: %w", err)
	}

	// Create or update the "games" KV bucket
	userKV, err := js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "users",
		Description: "Datastar Tic Tac Toe Game",
		Compression: true,
		TTL:         time.Hour,
		MaxBytes:    16 * 1024 * 1024,
	})
	if err != nil {
		return fmt.Errorf("error creating key value: %w", err)
	}

	// Save MVC state to the "game1" key in the "games" bucket
	saveMVC := func(ctx context.Context, mvc *components.GameState) error {
		b, err := json.Marshal(mvc)
		if err != nil {
			return fmt.Errorf("failed to marshal mvc: %w", err)
		}
		if _, err := gamesKV.Put(ctx, mvc.Id, b); err != nil {
			return fmt.Errorf("failed to put key value: %w", err)
		}
		return nil
	}

	// Save MVC state to the "game1" key in the "games" bucket
	saveUser := func(ctx context.Context, user *User) error {
		b, err := json.Marshal(user)
		if err != nil {
			return fmt.Errorf("failed to marshal mvc: %w", err)
		}
		if _, err := userKV.Put(ctx, user.SessionID, b); err != nil {
			return fmt.Errorf("failed to put key value: %w", err)
		}
		return nil
	}

	// Handle session and save the default MVC state
	mvcSession := func(w http.ResponseWriter, r *http.Request) (*components.GameState, error) {
		ctx := r.Context()

		mvc := &components.GameState{
			Id: toolbelt.NextEncodedID(),
		}
		if err := saveMVC(ctx, mvc); err != nil {
			return nil, fmt.Errorf("failed to save mvc: %w", err)
		}
		return mvc, nil
	}

	// Handle session and save the default MVC state
	userSession := func(w http.ResponseWriter, r *http.Request) (*User, error) {
		ctx := r.Context()

		sessionID, err := createSessionID(store, r, w)
		if err != nil {
			return nil, fmt.Errorf("failed to get session id: %w", err)
		}

		user := &User{
			SessionID: sessionID,
		}
		if err := saveUser(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to save mvc: %w", err)
		}
		return user, nil
	}

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		sessionId, err := getSessionID(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		pages.Index(sessionId).Render(r.Context(), w)
	})

	router.Route("/api", func(apiRouter chi.Router) {

		// Handle session and save the default state
		apiRouter.Route("/login", func(loginRouter chi.Router) {
			// Default game handler
			loginRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				user, err := userSession(w, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				sse := datastar.NewSSE(w, r)
				c := components.InitGame(user.SessionID)
				if err := sse.MergeFragmentTempl(c); err != nil {
					sse.ConsoleError(err)
					return
				}
			})
		})

		apiRouter.Route("/game", func(gameRouter chi.Router) {
			// Default game handler
			gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				_, err := mvcSession(w, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			})

			gameRouter.Delete("/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()

				// Extract the "id" parameter from the URL
				id := chi.URLParam(r, "id")
				if id == "" {
					http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
					return
				}
				log.Printf("id: %s", id)

				// Delete the specified key from the "games" bucket
				if err := gamesKV.Delete(ctx, id); err != nil {
					http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", id, err), http.StatusInternalServerError)
					return
				}
			})
		})

		// Watch for updates to the "games" bucket
		apiRouter.Route("/games", func(gameRouter chi.Router) {
			gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				sse := datastar.NewSSE(w, r)

				log.Printf("Live update: Key=%s, Value=%s", "s", "s")

				ctx := r.Context()

				watcher, err := gamesKV.WatchAll(ctx)
				if err != nil {
					http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
					return
				}
				defer watcher.Stop()

				for {
					select {
					case <-ctx.Done():
						return
					case entry := <-watcher.Updates():
						if entry == nil {
							continue
						}

						// Process the entry
						switch entry.Operation() {
						case jetstream.KeyValuePut:
							log.Printf("[LIVE] Key=%s, Value=%s", entry.Key(), string(entry.Value()))
							mvc := &components.GameState{}
							if err := json.Unmarshal(entry.Value(), mvc); err != nil {
								http.Error(w, err.Error(), http.StatusInternalServerError)
								return
							}
							c := components.AddGame(mvc)
							if err := sse.MergeFragmentTempl(c,
								datastar.WithSelectorID("games-list-container"),
								datastar.WithMergeAppend(),
							); err != nil {
								sse.ConsoleError(err)
								return
							}

						case jetstream.KeyValueDelete:
							// Compare timestamp to identify historical entries
							if entry.Created().Before(time.Now().Add(-1 * time.Second)) {
								log.Printf("[HISTORICAL] Skipping Key=%s", entry.Key())
								continue
							}

							log.Printf("[LIVE DELETE] Key=%s", entry.Key())
							id := fmt.Sprintf("#game-%s", entry.Key())
							if err := sse.RemoveFragments(id, datastar.WithRemoveSettleDuration(1*time.Millisecond), datastar.WithRemoveUseViewTransitions(true)); err != nil {
								log.Printf("Error removing fragment for Key=%s: %v", entry.Key(), err)
								sse.ConsoleError(err)
								return
							}
						}
					}
				}

			})
		})
	})

	return nil
}

// Helper function to marshal data as JSON
func MustJSONMarshal(v any) string {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Check if a user session exists
func getSessionID(store sessions.Store, r *http.Request) (string, error) {
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

// Create a new user session
func createSessionID(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {
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

// Upsert user session: Checks if a session exists; creates one if it doesn't
func upsertSessionID(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {
	id, err := getSessionID(store, r)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil // Session already exists
	}
	return createSessionID(store, r, w) // Create a new session
}
