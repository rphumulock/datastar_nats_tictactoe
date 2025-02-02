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
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"
	datastar "github.com/starfederation/datastar/sdk/go"
)

func setupGameRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	gamesKV, err := js.KeyValue(ctx, "games")
	if err != nil {
		return fmt.Errorf("failed to get games key-value store: %w", err)
	}

	router.Get("/game/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			respondError(w, http.StatusBadRequest, "missing 'id' parameter")
			return
		}

		pages.Game(id).Render(r.Context(), w)
	})

	router.Route("/api/game/{id}", func(gameRouter chi.Router) {

		gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			sse := datastar.NewSSE(w, r)
			id := chi.URLParam(r, "id")
			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			gameState, err := fetchGameState(ctx, gamesKV, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}

			sessionId, err := getSessionId(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			if gameIsFull(gameState) && !containsPlayer(gameState, sessionId) {
				respondError(w, http.StatusForbidden, "game is full")
				return
			}

			if !containsPlayer(gameState, sessionId) {
				gameState.ChallengerId = sessionId
				if err := updateGameState(ctx, gamesKV, gameState); err != nil {
					respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
					return
				}
			}
			c := components.Game(gameState, sessionId)
			if err := sse.MergeFragmentTempl(c); err != nil {
				sse.ConsoleError(err)
				return
			}

		})

		gameRouter.Get("/watch", func(w http.ResponseWriter, r *http.Request) {
			sse := datastar.NewSSE(w, r)
			id := chi.URLParam(r, "id")
			if id == "" {
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			sessionId, err := getSessionId(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			watcher, err := gamesKV.Watch(ctx, id)
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

					c := components.GameBoard(mvc, sessionId)
					if err := sse.MergeFragmentTempl(c,
						datastar.WithSelectorID("game-container"),
						datastar.WithMergeMorph(),
					); err != nil {
						sse.ConsoleError(err)
					}

				case jetstream.KeyValueDelete:
					sse.Redirect("/")
				}
			}
		})

		gameRouter.Post("/toggle/{cell}", func(w http.ResponseWriter, r *http.Request) {
			sse := datastar.NewSSE(w, r)

			id := chi.URLParam(r, "id")
			if id == "" {
				sse.ExecuteScript("alert('Missing game ID')")
				respondError(w, http.StatusBadRequest, "missing 'id' parameter")
				return
			}

			mvc, err := fetchGameState(ctx, gamesKV, id)
			if err != nil {
				sse.ExecuteScript("alert('Error retrieving game state')")
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}

			sessionId, err := getSessionId(store, r)
			if err != nil || sessionId == "" {
				sse.ExecuteScript("alert('Error getting session ID')")
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			cell := chi.URLParam(r, "cell")
			i, err := strconv.Atoi(cell)
			if err != nil || i < 0 || i >= len(mvc.Board) {
				sse.ExecuteScript("alert('Invalid cell index')")
				respondError(w, http.StatusBadRequest, "invalid cell index")
				return
			}

			if mvc.Board[i] != "" {
				sse.ExecuteScript("alert('Cell already occupied')")
				respondError(w, http.StatusBadRequest, "cell already occupied")
				return
			}

			if mvc.XIsNext && sessionId != mvc.HostId {
				sse.ExecuteScript("alert('Not your turn')")
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}
			if !mvc.XIsNext && sessionId != mvc.ChallengerId {
				sse.ExecuteScript("alert('Not your turn')")
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
			if winner == "TIE" {
				mvc.Winner = "TIE"
				log.Printf("Game over! Result: Tie")
			} else if winner != "" {
				mvc.Winner = winner
				log.Printf("Game over! Winner: %s", winner)
			}

			if err := updateGameState(ctx, gamesKV, mvc); err != nil {
				respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
				return
			}

			w.WriteHeader(http.StatusOK)
		})

		gameRouter.Post("/leave", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			sse := datastar.NewSSE(w, r)
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

			mvc.ChallengerId = ""
			if err := updateGameState(ctx, gamesKV, mvc); err != nil {
				respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
				return
			}

			sse.Redirect("/")
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

			sessionId, err := getSessionId(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			if sessionId != mvc.HostId {
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

	boardFull := true // Assume the board is full initially

	// Check for a winner and simultaneously check if the board is full
	for _, combination := range winningCombinations {
		if board[combination[0]] != "" &&
			board[combination[0]] == board[combination[1]] &&
			board[combination[0]] == board[combination[2]] {
			return board[combination[0]] // Return the winner ("X" or "O")
		}
	}

	// Check if the board is full
	for _, cell := range board {
		if cell == "" {
			boardFull = false
			break
		}
	}

	if boardFull {
		return "TIE" // Board is full and no winner
	}

	return "" // No winner yet and moves still possible
}

// Helper functions

func respondError(w http.ResponseWriter, statusCode int, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	http.Error(w, message, statusCode)
}

func gameIsFull(mvc *components.GameState) bool {
	return mvc.ChallengerId != ""
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

func containsPlayer(mvc *components.GameState, sessionId string) bool {
	return mvc.HostId == sessionId || mvc.ChallengerId == sessionId
}
