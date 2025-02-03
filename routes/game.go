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

	gameLobbiesKV, err := js.KeyValue(ctx, "gameLobbies")
	if err != nil {
		return fmt.Errorf("failed to get game lobbies key value: %w", err)
	}

	gameBoardsKV, err := js.KeyValue(ctx, "gameBoards")
	if err != nil {
		return fmt.Errorf("failed to get game boards key value: %w", err)
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

			entry, err := gameBoardsKV.Get(ctx, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}
			gameState := &components.GameState{}
			if err := json.Unmarshal(entry.Value(), gameState); err != nil {
				respondError(w, http.StatusInternalServerError, "error unmarshalling game state: %v", err)
				return
			}

			sessionId, err := getSessionId(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			if gameState.ChallengerId != "" && !containsPlayer(gameState, sessionId) {
				respondError(w, http.StatusForbidden, "game is full")
				return
			}

			if !containsPlayer(gameState, sessionId) {
				gameState.ChallengerId = sessionId
				data, err := json.Marshal(gameState)
				if err != nil {
					respondError(w, http.StatusInternalServerError, "error marshalling game state: %v", err)
					return
				}
				if _, err := gameLobbiesKV.Update(ctx, gameState.Id, data, entry.Revision()); err != nil {
					respondError(w, http.StatusInternalServerError, "error updating game state: %v", err)
					return
				}
			}
			c := components.GameContainer(sessionId, gameState)
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

			watcher, err := gameBoardsKV.Watch(ctx, id)
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
						datastar.WithSelectorID("gameboard"),
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

			entry, err := gameLobbiesKV.Get(ctx, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}
			gameLobby := &components.GameLobby{}
			if err := json.Unmarshal(entry.Value(), gameLobby); err != nil {
				respondError(w, http.StatusInternalServerError, "error unmarshalling game state: %v", err)
				return
			}

			entry, err = gameBoardsKV.Get(ctx, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}
			gameState := &components.GameState{}
			if err := json.Unmarshal(entry.Value(), gameState); err != nil {
				respondError(w, http.StatusInternalServerError, "error unmarshalling game state: %v", err)
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
			if err != nil || i < 0 || i >= len(gameState.Board) {
				sse.ExecuteScript("alert('Invalid cell index')")
				respondError(w, http.StatusBadRequest, "invalid cell index")
				return
			}

			if gameState.Board[i] != "" {
				sse.ExecuteScript("alert('Cell already occupied')")
				respondError(w, http.StatusBadRequest, "cell already occupied")
				return
			}

			if gameState.XIsNext && sessionId != gameState.HostId {
				sse.ExecuteScript("alert('Not your turn')")
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}
			if !gameState.XIsNext && sessionId != gameState.ChallengerId {
				sse.ExecuteScript("alert('Not your turn')")
				respondError(w, http.StatusForbidden, "not your turn")
				return
			}

			if gameState.XIsNext {
				gameState.Board[i] = "X"
			} else {
				gameState.Board[i] = "O"
			}
			gameState.XIsNext = !gameState.XIsNext

			winner := checkWinner(gameState.Board[:])
			if winner == "TIE" {
				gameState.Winner = "TIE"
				log.Printf("Game over! Result: Tie")
			} else if winner != "" {
				gameState.Winner = winner
				log.Printf("Game over! Winner: %s", winner)
			}

			data, err := json.Marshal(gameState)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error marshalling game state: %v", err)
				return
			}
			entry, err = gameBoardsKV.Get(ctx, gameState.Id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving current revision: %v", err)
				return
			}
			if _, err := gameBoardsKV.Update(ctx, gameState.Id, data, entry.Revision()); err != nil {
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

			entry, err := gameBoardsKV.Get(ctx, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}
			gameState := &components.GameState{}
			if err := json.Unmarshal(entry.Value(), gameState); err != nil {
				respondError(w, http.StatusInternalServerError, "error unmarshalling game state: %v", err)
				return
			}

			gameState.ChallengerId = ""
			data, err := json.Marshal(gameState)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error marshalling game state: %v", err)
				return
			}
			entry, err = gameLobbiesKV.Get(ctx, gameState.Id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving current revision: %v", err)
				return
			}
			if _, err := gameLobbiesKV.Update(ctx, gameState.Id, data, entry.Revision()); err != nil {
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

			entry, err := gameLobbiesKV.Get(ctx, id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving game: %v", err)
				return
			}
			gameState := &components.GameState{}
			if err := json.Unmarshal(entry.Value(), gameState); err != nil {
				respondError(w, http.StatusInternalServerError, "error unmarshalling game state: %v", err)
				return
			}

			sessionId, err := getSessionId(store, r)
			if err != nil || sessionId == "" {
				respondError(w, http.StatusInternalServerError, "error getting session ID: %v", err)
				return
			}

			if sessionId != gameState.HostId {
				respondError(w, http.StatusForbidden, "not your game")
				return
			}

			gameState.Board = [9]string{"", "", "", "", "", "", "", "", ""}
			gameState.Winner = ""
			gameState.XIsNext = true

			data, err := json.Marshal(gameState)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error marshalling game state: %v", err)
				return
			}
			entry, err = gameBoardsKV.Get(ctx, gameState.Id)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "error retrieving current revision: %v", err)
				return
			}
			if _, err := gameBoardsKV.Update(ctx, gameState.Id, data, entry.Revision()); err != nil {
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

func containsPlayer(mvc *components.GameState, sessionId string) bool {
	return mvc.HostId == sessionId || mvc.ChallengerId == sessionId
}
