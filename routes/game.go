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
	datastar "github.com/starfederation/datastar/code/go/sdk"
	"github.com/zangster300/northstar/web/components"
)

func setupGameRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	gamesKV, err := js.KeyValue(ctx, "games")
	if err != nil {
		return fmt.Errorf("failed to get games key-value store: %w", err)
	}

	type contextKey string
	const gameStateKey contextKey = "gameState"

	// Middleware to fetch game state using {id} and store it in context
	gameRouterMiddlewareFetchState := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			entry, err := gamesKV.Get(ctx, id)
			if err != nil {
				if err == nats.ErrKeyNotFound {
					respondError(w, http.StatusNotFound, "game with id '%s' not found", id)
					return
				}
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}

			mvc := &components.GameState{}
			if err := json.Unmarshal(entry.Value(), mvc); err != nil {
				respondError(w, http.StatusInternalServerError, "error unmarshalling game state: %v", err)
				return
			}

			ctx := context.WithValue(r.Context(), gameStateKey, mvc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	// Define /game/{id} routes
	router.Route("/game/{id}", func(gameRouter chi.Router) {
		gameRouter.Use(gameRouterMiddlewareFetchState)

		gameRouter.Get("/watch", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			sse := datastar.NewSSE(w, r)

			id := chi.URLParam(r, "id")

			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			sessionId, err := getSessionID(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			watcher, err := gamesKV.WatchAll(ctx)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
				return
			}
			defer watcher.Stop()

			// Process updates
			for update := range watcher.Updates() {
				// `nil` signals the end of the historical replay
				if update == nil {
					fmt.Println("End of historical updates. Now receiving live updates...")
					continue
				}

				switch update.Operation() {
				case jetstream.KeyValuePut:
					mvc := &components.GameState{}
					if err := json.Unmarshal(update.Value(), mvc); err != nil {
						log.Printf("Error unmarshalling update: %v", err)
						continue
					}

					log.Printf("X is next update: %t", mvc.XIsNext)

					c := components.GameBoard(mvc, sessionId)
					if err := sse.MergeFragmentTempl(c,
						datastar.WithSelectorID("game-container"),
						datastar.WithMergeMorph(),
					); err != nil {
						sse.ConsoleError(err)
					}

				case jetstream.KeyValueDelete:
				}

			}
		})

		// Game state route
		gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			mvc, ok := r.Context().Value(gameStateKey).(*components.GameState)
			if !ok {
				respondError(w, http.StatusInternalServerError, "game state not found in context")
				return
			}

			sessionId, err := getSessionID(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			if gameIsFull(mvc) && !containsPlayer(mvc.Players[:], sessionId) {
				respondError(w, http.StatusForbidden, "game is full")
				return
			}

			if !containsPlayer(mvc.Players[:], sessionId) {
				addPlayer(mvc, sessionId)
				if err := updateGameState(ctx, gamesKV, mvc); err != nil {
					respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
					return
				}
			}

			components.GameMVCView(mvc, sessionId).Render(ctx, w)
		})

		// Toggle cell route
		gameRouter.Post("/toggle/{cell}", func(w http.ResponseWriter, r *http.Request) {
			mvc, ok := r.Context().Value(gameStateKey).(*components.GameState)
			if !ok {
				respondError(w, http.StatusInternalServerError, "game state not found in context")
				return
			}

			sessionId, err := getSessionID(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			cell := chi.URLParam(r, "cell")
			i, err := strconv.Atoi(cell)
			if err != nil || i < 0 || i >= len(mvc.Board) {
				respondError(w, http.StatusBadRequest, "invalid cell index")
				return
			}

			// Ensure the cell is not already occupied
			if mvc.Board[i] != "" {
				respondError(w, http.StatusBadRequest, "cell already occupied")
				return
			}

			// Ensure the current player is valid
			if mvc.XIsNext && sessionId != mvc.Players[0] {
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}
			if !mvc.XIsNext && sessionId != mvc.Players[1] {
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}

			// Update the board and toggle the turn
			if mvc.XIsNext {
				mvc.Board[i] = "X"
			} else {
				mvc.Board[i] = "O"
			}
			mvc.XIsNext = !mvc.XIsNext

			// Check for a winner
			winner := checkWinner(mvc.Board[:])
			if winner != "" {
				mvc.Winner = winner
				log.Printf("Game over! Winner: %s", winner)
			}

			// Save updated game state
			if err := updateGameState(ctx, gamesKV, mvc); err != nil {
				respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
				return
			}

			w.WriteHeader(http.StatusOK)
		})

		gameRouter.Post("/reset", func(w http.ResponseWriter, r *http.Request) {
			mvc, ok := r.Context().Value(gameStateKey).(*components.GameState)
			if !ok {
				respondError(w, http.StatusInternalServerError, "game state not found in context")
				return
			}

			sessionId, err := getSessionID(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			cell := chi.URLParam(r, "cell")
			i, err := strconv.Atoi(cell)
			if err != nil || i < 0 || i >= len(mvc.Board) {
				respondError(w, http.StatusBadRequest, "invalid cell index")
				return
			}

			// Ensure the cell is not already occupied
			if mvc.Board[i] != "" {
				respondError(w, http.StatusBadRequest, "cell already occupied")
				return
			}

			// Ensure the current player is valid
			if mvc.XIsNext && sessionId != mvc.Players[0] {
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}
			if !mvc.XIsNext && sessionId != mvc.Players[1] {
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}

			// Update the board and toggle the turn
			if mvc.XIsNext {
				mvc.Board[i] = "X"
			} else {
				mvc.Board[i] = "O"
			}
			mvc.XIsNext = !mvc.XIsNext

			// Check for a winner
			winner := checkWinner(mvc.Board[:])
			if winner != "" {
				mvc.Winner = winner
				log.Printf("Game over! Winner: %s", winner)
			}

			// Save updated game state
			if err := updateGameState(ctx, gamesKV, mvc); err != nil {
				respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
				return
			}

			w.WriteHeader(http.StatusOK)
		})

	})

	return nil
}

func checkWinner(board []string) string {
	winningCombinations := [][]int{
		{0, 1, 2}, // Top row
		{3, 4, 5}, // Middle row
		{6, 7, 8}, // Bottom row
		{0, 3, 6}, // Left column
		{1, 4, 7}, // Middle column
		{2, 5, 8}, // Right column
		{0, 4, 8}, // Top-left to bottom-right diagonal
		{2, 4, 6}, // Top-right to bottom-left diagonal
	}

	for _, combination := range winningCombinations {
		if board[combination[0]] != "" &&
			board[combination[0]] == board[combination[1]] &&
			board[combination[0]] == board[combination[2]] {
			return board[combination[0]] // Return the winner ("X" or "O")
		}
	}

	return "" // No winner
}

// Helper functions

func respondError(w http.ResponseWriter, statusCode int, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	http.Error(w, message, statusCode)
}

func gameIsFull(mvc *components.GameState) bool {
	return mvc.Players[0] != "" && mvc.Players[1] != ""
}

func addPlayer(mvc *components.GameState, sessionId string) {
	if mvc.Players[0] == "" {
		mvc.Players[0] = sessionId
	} else if mvc.Players[1] == "" {
		mvc.Players[1] = sessionId
	}
}

func updateGameState(ctx context.Context, gamesKV jetstream.KeyValue, mvc *components.GameState) error {
	data, err := json.Marshal(mvc)
	if err != nil {
		return fmt.Errorf("error marshalling game state: %v", err)
	}

	entry, err := gamesKV.Get(ctx, mvc.Id)
	if err != nil {
		return fmt.Errorf("error retrieving current revision: %v", err)
	}

	if _, err := gamesKV.Update(ctx, mvc.Id, data, entry.Revision()); err != nil {
		return fmt.Errorf("error updating game state: %v", err)
	}
	return nil
}

func containsPlayer(players []string, player string) bool {
	for _, p := range players {
		if p == player {
			return true
		}
	}
	return false
}
