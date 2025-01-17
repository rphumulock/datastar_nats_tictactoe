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

	// Define /game/{id} routes
	router.Route("/game/{id}", func(gameRouter chi.Router) {

		gameRouter.Get("/watch", func(w http.ResponseWriter, r *http.Request) {
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

			sse := datastar.NewSSE(w, r)
			watcher, err := gamesKV.WatchAll(r.Context())
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
				return
			}
			defer watcher.Stop()

			// Process updates
			for update := range watcher.Updates() {
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

					log.Printf("Received update for game %v", mvc)

					if mvc.Id != id {
						continue
					}

					if mvc.Winner != "" {
						c := components.GameWinner(mvc, sessionId)
						if err := sse.MergeFragmentTempl(c,
							datastar.WithSelectorID("game-container"),
							datastar.WithMergeMorph(),
						); err != nil {
							sse.ConsoleError(err)
						}
					} else {
						c := components.GameBoard(mvc, sessionId)
						if err := sse.MergeFragmentTempl(c,
							datastar.WithSelectorID("game-container"),
							datastar.WithMergeMorph(),
						); err != nil {
							sse.ConsoleError(err)
						}
					}
				case jetstream.KeyValueDelete:
				}
			}
		})

		gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			mvc, err := fetchGameState(ctx, gamesKV, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
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

		gameRouter.Post("/toggle/{cell}", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			mvc, err := fetchGameState(ctx, gamesKV, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
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

			if mvc.Board[i] != "" {
				respondError(w, http.StatusBadRequest, "cell already occupied")
				return
			}

			if mvc.XIsNext && sessionId != mvc.Players[0] {
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}
			if !mvc.XIsNext && sessionId != mvc.Players[1] {
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}

			if mvc.XIsNext {
				mvc.Board[i] = "X"
			} else {
				mvc.Board[i] = "O"
			}
			mvc.XIsNext = !mvc.XIsNext

			winner := checkWinner(mvc.Board[:])
			if winner != "" {
				mvc.Winner = winner
				log.Printf("Game over! Winner: %s", winner)
			}

			if err := updateGameState(ctx, gamesKV, mvc); err != nil {
				respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
				return
			}

			w.WriteHeader(http.StatusOK)
		})

		gameRouter.Post("/reset", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			mvc, err := fetchGameState(ctx, gamesKV, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}

			sessionId, err := getSessionID(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			if sessionId != mvc.Players[0] {
				respondError(w, http.StatusForbidden, "not your game")
				return
			}

			mvc.Board = [9]string{"", "", "", "", "", "", "", "", ""}
			mvc.Winner = ""
			mvc.XIsNext = true

			if err := updateGameState(ctx, gamesKV, mvc); err != nil {
				respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
				return
			}

			w.WriteHeader(http.StatusOK)
		})
	})

	return nil
}

func fetchGameState(ctx context.Context, gamesKV jetstream.KeyValue, id string) (*components.GameState, error) {
	entry, err := gamesKV.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	mvc := &components.GameState{}
	if err := json.Unmarshal(entry.Value(), mvc); err != nil {
		return nil, fmt.Errorf("error unmarshalling game state: %w", err)
	}

	return mvc, nil
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
