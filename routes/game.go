package routes

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/zangster300/northstar/web/pages"
)

func setupGameRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {

	// Create or get the KeyValue bucket for "games"
	gamesKV, err := js.KeyValue(context.Background(), "games")
	if err != nil {
		return fmt.Errorf("error creating key-value bucket: %w", err)
	}

	router.Get("/game/{id}", func(w http.ResponseWriter, r *http.Request) {

		// Get the "id" parameter from the URL
		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
			return
		}

		// Check if the key exists in the "games" bucket
		entry, err := gamesKV.Get(r.Context(), id)
		if err != nil {
			if err == nats.ErrKeyNotFound {
				http.Error(w, fmt.Sprintf("game with id '%s' not found", id), http.StatusNotFound)
				return
			}
			http.Error(w, fmt.Sprintf("error retrieving game: %v", err), http.StatusInternalServerError)
			return
		}

		// Use the retrieved value (entry.Value()) if necessary
		// Example: Assume `pages.Game` is rendering based on the ID
		fmt.Printf("Game ID: %s, Value: %s\n", entry.Key(), string(entry.Value()))

		// Render the game page
		pages.Game(id).Render(r.Context(), w)
	})

	return nil
}
