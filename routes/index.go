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
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/natastar/web/components"
	"github.com/rphumulock/natastar/web/pages"
)

type User struct {
	SessionID string `json:"session_id"` // Unique session ID for the user
	GameID    string `json:"game_id"`    // List of games the user is in
}

func setupIndexRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	gamesKV, err := js.KeyValue(ctx, "games")
	if err != nil {
		return fmt.Errorf("failed to get games key value: %w", err)
	}
	usersKV, err := js.KeyValue(ctx, "users")
	if err != nil {
		return fmt.Errorf("failed to get games key value: %w", err)
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
		if _, err := usersKV.Put(ctx, user.SessionID, b); err != nil {
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

		if sessionId != "" {
			pages.Dashboard(sessionId).Render(r.Context(), w)
		} else {
			pages.Login().Render(r.Context(), w)
		}
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

			gameRouter.Post("/create", func(w http.ResponseWriter, r *http.Request) {
				sessionId, err := getSessionID(store, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				gameState := components.GameState{
					Id:      sessionId,
					Players: [2]string{sessionId, ""},
					Board:   [9]string{"", "", "", "", "", "", "", "", ""},
					XIsNext: true,
					Winner:  "",
				}
				bytes, err := json.Marshal(gameState)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to marshal mvc: %v", err), http.StatusInternalServerError)
					return
				}
				_, err = gamesKV.Put(r.Context(), sessionId, bytes)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to put key value: %v", err), http.StatusInternalServerError)
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

			gameRouter.Delete("/deleteAllGames", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()

				// List all keys in the bucket
				keys, err := gamesKV.Keys(ctx)
				if err != nil {
					log.Fatalf("Error listing keys: %v", err)
				}

				if len(keys) == 0 {
					fmt.Println("No keys found in the bucket.")
					return
				}

				// Delete all keys
				for _, key := range keys {
					err := gamesKV.Delete(ctx, key)
					if err != nil {
						log.Printf("Error deleting key '%s': %v", key, err)
					} else {
						fmt.Printf("Deleted key: %s\n", key)
					}
				}
			})
		})

		// Watch for updates to the "games" bucket
		apiRouter.Route("/games", func(gameRouter chi.Router) {
			gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				sse := datastar.NewSSE(w, r)

				sessionId, err := getSessionID(store, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				ctx := r.Context()

				watcher, err := gamesKV.WatchAll(ctx)
				if err != nil {
					http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
					return
				}
				defer watcher.Stop()

				// Track historical mode
				historicalMode := true

				// Process updates
				for update := range watcher.Updates() {
					// `nil` signals the end of the historical replay
					if update == nil {
						fmt.Println("End of historical updates. Now receiving live updates...")
						historicalMode = false
						continue
					}

					log.Printf("Received update: %s", update.Value())

					switch update.Operation() {
					case jetstream.KeyValuePut:
						processPutOperation(update, historicalMode, sessionId, *sse)

					case jetstream.KeyValueDelete:
						processDeleteOperation(update, historicalMode, *sse)
					}
				}
			})
		})
	})

	return nil
}

// Helper function to process KeyValuePut operation
func processPutOperation(update jetstream.KeyValueEntry, historicalMode bool, sessionId string, sse datastar.ServerSentEventGenerator) {
	if historicalMode {
		handleHistoricalPut(update, sessionId, sse)
	} else {
		handleLivePut(update, sessionId, sse)
	}
}

// Helper function to process KeyValueDelete operation
func processDeleteOperation(update jetstream.KeyValueEntry, historicalMode bool, sse datastar.ServerSentEventGenerator) {
	if historicalMode {
		fmt.Printf("Ignoring historical delete for key: %s\n", update.Key())
		return
	}

	if err := sse.RemoveFragments("#game-"+update.Key(),
		datastar.WithRemoveSettleDuration(1*time.Millisecond),
		datastar.WithRemoveUseViewTransitions(true),
	); err != nil {
		sse.ConsoleError(err)
	}
}

// Handle historical KeyValuePut updates
func handleHistoricalPut(update jetstream.KeyValueEntry, sessionId string, sse datastar.ServerSentEventGenerator) {
	if update.Key() == sessionId {
		handleSessionUpdate(update, sessionId, sse)
	} else {
		handleNonSessionUpdate(update, sessionId, sse)
	}
}

// Handle live KeyValuePut updates
func handleLivePut(update jetstream.KeyValueEntry, sessionId string, sse datastar.ServerSentEventGenerator) {
	if update.Key() == sessionId {
		sse.ExecuteScript("window.location.assign('/game/" + sessionId + "')")
	} else {
		handleNonSessionUpdate(update, sessionId, sse)
	}
}

// Handle updates for the session ID
func handleSessionUpdate(update jetstream.KeyValueEntry, sessionId string, sse datastar.ServerSentEventGenerator) {
	sse.RemoveFragments("#game-"+update.Key(),
		datastar.WithRemoveSettleDuration(1*time.Millisecond),
	)

	mvc := &components.GameState{}
	if err := json.Unmarshal(update.Value(), mvc); err != nil {
		log.Printf("Error unmarshalling update value: %v", err)
		return
	}

	c := components.HostedGame(mvc, sessionId)
	if err := sse.MergeFragmentTempl(c,
		datastar.WithSelectorID("games-list-container"),
		datastar.WithMergeAppend(),
	); err != nil {
		sse.ConsoleError(err)
	}
}

// Handle updates for keys other than the session ID
func handleNonSessionUpdate(update jetstream.KeyValueEntry, sessionId string, sse datastar.ServerSentEventGenerator) {
	sse.RemoveFragments("#game-"+update.Key(),
		datastar.WithRemoveSettleDuration(1*time.Millisecond),
	)

	mvc := &components.GameState{}
	if err := json.Unmarshal(update.Value(), mvc); err != nil {
		log.Printf("Error unmarshalling update value: %v", err)
		return
	}

	c := components.JoinGame(mvc, sessionId)
	if err := sse.MergeFragmentTempl(c,
		datastar.WithSelectorID("games-list-container"),
		datastar.WithMergeAppend(),
	); err != nil {
		sse.ConsoleError(err)
	}
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
