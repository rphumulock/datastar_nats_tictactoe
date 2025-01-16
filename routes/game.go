package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/zangster300/northstar/web/components"
)

func setupGameRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	gamesKV, err := js.KeyValue(ctx, "games")
	if err != nil {
		return fmt.Errorf("failed to get games key value: %w", err)
	}

	type contextKey string
	const gameStateKey contextKey = "gameState"

	// Middleware to set up a watcher for updates
	gameRouterMiddlewareWatcher := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			// Start watching updates for the specific game ID
			watcher, err := gamesKV.Watch(ctx, id)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error starting watcher: %v", err), http.StatusInternalServerError)
				return
			}
			defer watcher.Stop()

			// Process updates asynchronously
			go func() {
				for update := range watcher.Updates() {
					if update == nil {
						fmt.Println("Watcher stopped or no more updates.")
						return
					}
					fmt.Printf("Received update for game ID '%s': %s\n", id, update.Value())
				}
			}()

			next.ServeHTTP(w, r)
		})
	}

	// Middleware to fetch game state using {id} and store in context
	gameRouterMiddlewareFetchState := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			// Fetch game state from gamesKV
			entry, err := gamesKV.Get(ctx, id)
			if err != nil {
				if err == nats.ErrKeyNotFound {
					respondError(w, http.StatusNotFound, "game with id '%s' not found", id)
					return
				}
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}

			// Unmarshal game state
			mvc := &components.GameState{}
			if err := json.Unmarshal(entry.Value(), mvc); err != nil {
				respondError(w, http.StatusInternalServerError, "error unmarshalling game state: %v", err)
				return
			}

			// Add game state to context
			ctx := context.WithValue(r.Context(), gameStateKey, mvc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	// Define /game/{id} routes
	router.Route("/game/{id}", func(gameRouter chi.Router) {
		gameRouter.Use(gameRouterMiddlewareWatcher)
		gameRouter.Use(gameRouterMiddlewareFetchState)

		// Handle game page
		gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			mvc, ok := r.Context().Value(gameStateKey).(*components.GameState)
			if !ok {
				respondError(w, http.StatusInternalServerError, "game state not found in context")
				return
			}

			// Retrieve session ID
			sessionId, err := getSessionID(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			// Ensure game is not full or session is valid
			if gameIsFull(mvc) && !containsPlayer(mvc.Players[:], sessionId) {
				respondError(w, http.StatusForbidden, "game is full")
				return
			}

			// Add player if not already in game
			if !containsPlayer(mvc.Players[:], sessionId) {
				addPlayer(mvc, sessionId)
				if err := updateGameState(ctx, gamesKV, mvc); err != nil {
					respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
					return
				}
			}

			// Render game page
			components.GameMVCView(mvc, sessionId).Render(ctx, w)
		})

		// Toggle route
		gameRouter.Route("/toggle", func(toggleRouter chi.Router) {
			toggleRouter.Post("/{cell}", func(w http.ResponseWriter, r *http.Request) {
				mvc, ok := r.Context().Value(gameStateKey).(*components.GameState)
				if !ok {
					respondError(w, http.StatusInternalServerError, "game state not found in context")
					return
				}

				// Retrieve session ID
				sessionId, err := getSessionID(store, r)
				if err != nil || sessionId == "" {
					respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
					return
				}

				cell := chi.URLParam(r, "cell")
				i, err := strconv.Atoi(cell)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}

				if sessionId == mvc.Players[0] {
					mvc.Board[i] = "X"
					mvc.XIsNext = false
				} else {
					mvc.Board[i] = "O"
					mvc.XIsNext = true
				}

				// Log the game state
				// logGameState("GameState before update", mvc)

				// Update game state

				// Log updated game state
				// logGameState("GameState after update", mvc)

				// Save updated game state
				if err := updateGameState(ctx, gamesKV, mvc); err != nil {
					respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
					return
				}
			})
		})
	})

	return nil
}

// Helper function to log game state as JSON
func logGameState(prefix string, mvc *components.GameState) {
	mvcJSON, err := json.MarshalIndent(mvc, "", "  ")
	if err != nil {
		log.Printf("%s: error marshalling GameState to JSON: %v", prefix, err)
	} else {
		log.Printf("%s: %s", prefix, mvcJSON)
	}
}

// Helper function to respond with an error
func respondError(w http.ResponseWriter, statusCode int, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	http.Error(w, message, statusCode)
}

// Helper function to check if the game is full
func gameIsFull(mvc *components.GameState) bool {
	return mvc.Players[0] != "" && mvc.Players[1] != ""
}

// Helper function to add a player to the game
func addPlayer(mvc *components.GameState, sessionId string) {
	if mvc.Players[0] == "" {
		mvc.Players[0] = sessionId
	} else if mvc.Players[1] == "" {
		mvc.Players[1] = sessionId
	}
}

// Helper function to update the game state in the KV store
func updateGameState(ctx context.Context, gamesKV jetstream.KeyValue, mvc *components.GameState) error {
	data, err := json.Marshal(mvc)
	if err != nil {
		return fmt.Errorf("error marshalling game state: %v", err)
	}
	if _, err := gamesKV.Put(ctx, mvc.Id, data); err != nil {
		return fmt.Errorf("error storing game state: %v", err)
	}
	return nil
}

// Helper function to check if a player is already in the game
func containsPlayer(players []string, player string) bool {
	for _, p := range players {
		if p == player {
			return true
		}
	}
	return false
}
