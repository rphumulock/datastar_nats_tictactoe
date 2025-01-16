package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

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

	router.Route("/game", func(gameRouter chi.Router) {

		// Handle session and save the default state
		gameRouter.Route("/api", func(gameApiRouter chi.Router) {

			gameApiRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {

				ctx := r.Context()

				// Retrieve the game ID from the URL
				id := chi.URLParam(r, "id")
				if id == "" {
					respondError(w, http.StatusBadRequest, "missing 'id' parameter")
					return
				}

				// Watch updates for the specific key
				watcher, err := gamesKV.Watch(ctx, id)
				if err != nil {
					log.Fatalf("Error starting watcher: %v", err)
				}
				defer watcher.Stop()

				// Process updates
				for update := range watcher.Updates() {
					if update == nil {
						// End of updates (e.g., the watcher was stopped)
						fmt.Println("Watcher stopped or no more updates.")
						return
					}

					// Process the update (you can add your processing logic here)
					fmt.Printf("Received update: %+v\n", update)
				}
			})

			// Default game handler
			gameApiRouter.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {

				ctx := r.Context()

				// Retrieve the session ID
				sessionId, err := getSessionID(store, r)
				if err != nil || sessionId == "" {
					respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
					return
				}

				// Retrieve the game ID from the URL
				id := chi.URLParam(r, "id")
				if id == "" {
					respondError(w, http.StatusBadRequest, "missing 'id' parameter")
					return
				}

				// Fetch the game state from the key-value store
				entry, err := gamesKV.Get(ctx, id)
				if err != nil {
					if err == nats.ErrKeyNotFound {
						respondError(w, http.StatusNotFound, "game with id '%s' not found", id)
						return
					}
					respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
					return
				}

				// Unmarshal the game state
				mvc := &components.GameState{}
				if err := json.Unmarshal(entry.Value(), mvc); err != nil {
					respondError(w, http.StatusInternalServerError, "error unmarshalling game state: %v", err)
					return
				}

				// Ensure the game is not full and the session ID is valid
				if gameIsFull(mvc) && !containsPlayer(mvc.Players[:], sessionId) {
					respondError(w, http.StatusForbidden, "game is full")
					return
				}

				// Add the player if they are not already in the game
				if !containsPlayer(mvc.Players[:], sessionId) {
					addPlayer(mvc, sessionId)
					if err := updateGameState(ctx, gamesKV, mvc); err != nil {
						respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
						return
					}
				}

				// Render the game page
				components.GameMVCView(mvc, sessionId).Render(ctx, w)
			})
		})
	})

	return nil
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
