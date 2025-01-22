package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"
	datastar "github.com/starfederation/datastar/sdk/go"
)

type User struct {
	SessionId string `json:"session_id"` // Unique session ID for the user
	GameId    string `json:"game_id"`    // List of games the user is in
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

	saveUser := func(ctx context.Context, user *User) error {
		b, err := json.Marshal(user)
		if err != nil {
			return fmt.Errorf("failed to marshal mvc: %w", err)
		}
		if _, err := usersKV.Put(ctx, user.SessionId, b); err != nil {
			return fmt.Errorf("failed to put key value: %w", err)
		}
		return nil
	}

	userSession := func(w http.ResponseWriter, r *http.Request) (*User, error) {
		ctx := r.Context()

		SessionId, err := createSessionID(store, r, w)
		if err != nil {
			return nil, fmt.Errorf("failed to get session id: %w", err)
		}

		user := &User{
			SessionId: SessionId,
		}
		if err := saveUser(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to save mvc: %w", err)
		}
		return user, nil
	}

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		pages.Index().Render(r.Context(), w)
	})

	router.Route("/api/dashboard", func(dashboardRouter chi.Router) {

		dashboardRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			sse := datastar.NewSSE(w, r)
			sessionId, err := getSessionID(store, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if sessionId == "" {
				c := components.Login()
				if err := sse.MergeFragmentTempl(c); err != nil {
					sse.ConsoleError(err)
					return
				}
			} else {
				c := components.Dashboard(sessionId)
				if err := sse.MergeFragmentTempl(c); err != nil {
					sse.ConsoleError(err)
					return
				}
			}
		})

		dashboardRouter.Get("/login", func(w http.ResponseWriter, r *http.Request) {
			user, err := userSession(w, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			sse := datastar.NewSSE(w, r)
			c := components.Dashboard(user.SessionId)
			if err := sse.MergeFragmentTempl(c); err != nil {
				sse.ConsoleError(err)
				return
			}
		})

		dashboardRouter.Route("/lobby", func(lobbyRouter chi.Router) {

			lobbyRouter.Post("/create", func(w http.ResponseWriter, r *http.Request) {
				SessionId, err := getSessionID(store, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				gameState := components.GameState{
					Id:      SessionId,
					Players: [2]string{SessionId, ""},
					Board:   [9]string{"", "", "", "", "", "", "", "", ""},
					XIsNext: true,
					Winner:  "",
				}
				bytes, err := json.Marshal(gameState)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to marshal mvc: %v", err), http.StatusInternalServerError)
					return
				}
				_, err = gamesKV.Put(r.Context(), SessionId, bytes)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to put key value: %v", err), http.StatusInternalServerError)
					return
				}
			})

			lobbyRouter.Delete("/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
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

			lobbyRouter.Delete("/purge", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				sse := datastar.NewSSE(w, r)

				// List all keys in the "games" bucket
				keys, err := gamesKV.Keys(ctx)
				if err != nil {
					http.Error(w, fmt.Sprintf("Error listing keys: %v", err), http.StatusInternalServerError)
					log.Printf("Error listing keys: %v", err)
					return
				}

				if len(keys) == 0 {
					log.Println("No keys found in the bucket.")
					fmt.Fprintln(w, "No games to purge.")
					return
				}

				// Delete all keys
				for _, key := range keys {
					err := gamesKV.Delete(ctx, key)
					if err != nil {
						log.Printf("Error deleting key '%s': %v", key, err)
						continue
					}

					log.Printf("Deleted key: %s", key)

					if err := sse.RemoveFragments("#game-"+key,
						datastar.WithRemoveSettleDuration(1*time.Millisecond),
						datastar.WithRemoveUseViewTransitions(true),
					); err != nil {
						sse.ConsoleError(err)
					}
				}

				// Respond with a success message
				fmt.Fprintln(w, "All games have been purged.")
			})

			lobbyRouter.Get("/watch", func(w http.ResponseWriter, r *http.Request) {
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
		log.Printf("Ignoring historical delete for key: %s", update.Key())
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
	handleSessionUpdate(update, sessionId, sse)
}

// Handle live KeyValuePut updates
func handleLivePut(update jetstream.KeyValueEntry, sessionId string, sse datastar.ServerSentEventGenerator) {
	handleSessionUpdate(update, sessionId, sse)
}

// Handle updates for the session ID
func handleSessionUpdate(update jetstream.KeyValueEntry, sessionId string, sse datastar.ServerSentEventGenerator) {
	// Remove outdated fragments
	if err := sse.RemoveFragments("#game-"+update.Key(),
		datastar.WithRemoveSettleDuration(1*time.Millisecond),
	); err != nil {
		log.Printf("Error removing fragments: %v", err)
		return
	}

	// Parse the update into a GameState object
	var mvc components.GameState
	if err := json.Unmarshal(update.Value(), &mvc); err != nil {
		log.Printf("Error unmarshalling update value: %v", err)
		return
	}

	if mvc.Id != sessionId {
		if mvc.Players[1] == "" {

			// Create a new game list item and merge it into the DOM
			c := components.GameListItem(&mvc, sessionId)
			if err := sse.MergeFragmentTempl(c,
				datastar.WithSelectorID("games-list-container"),
				datastar.WithMergeAppend(),
			); err != nil {
				sse.ConsoleError(err)
			}
		}
	} else {
		// Create a new game list item and merge it into the DOM
		c := components.GameListItem(&mvc, sessionId)
		if err := sse.MergeFragmentTempl(c,
			datastar.WithSelectorID("games-list-container"),
			datastar.WithMergeAppend(),
		); err != nil {
			sse.ConsoleError(err)
		}
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
