package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"
	datastar "github.com/starfederation/datastar/sdk/go"
)

func setupGameRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	usersKV, err := js.KeyValue(ctx, "users")
	if err != nil {
		return fmt.Errorf("failed to get game boards key value: %w", err)
	}

	gameLobbiesKV, err := js.KeyValue(ctx, "gameLobbies")
	if err != nil {
		return fmt.Errorf("failed to get game boards key value: %w", err)
	}

	gameBoardsKV, err := js.KeyValue(ctx, "gameBoards")
	if err != nil {
		return fmt.Errorf("failed to get game boards key value: %w", err)
	}

	handleGamePage := func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
			return
		}

		sessionId, err := getSessionId(store, r)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		currentUser, _, err := GetObject[components.User](ctx, usersKV, sessionId)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		gameLobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		host, _, err := GetObject[components.User](ctx, usersKV, gameLobby.HostId)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		var challenger *components.User
		if gameLobby.ChallengerId != "" {
			challenger, _, err = GetObject[components.User](ctx, usersKV, gameLobby.ChallengerId)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
				return
			}
		} else {
			challenger = &components.User{}
			challenger.Name = ""
		}

		gameState, _, err := GetObject[components.GameState](ctx, gameBoardsKV, id)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		pages.Game(currentUser, host, challenger, gameLobby, gameState).Render(r.Context(), w)
	}

	router.Get("/game/{id}", handleGamePage)

	// API

	router.Route("/api/game/{id}", func(gameRouter chi.Router) {

		checkWinner := func(board []string) string {
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

		watchGameBoard := func(ctx context.Context, sse *datastar.ServerSentEventGenerator, gameId string) error {
			gameWatcher, err := gameBoardsKV.Watch(ctx, gameId)
			if err != nil {
				return fmt.Errorf("failed to start game watcher: %w", err)
			}
			defer gameWatcher.Stop()

			for {
				select {
				case <-ctx.Done():
					return nil // Exit if context is canceled
				case update, ok := <-gameWatcher.Updates():
					if !ok {
						return nil // Exit if the channel is closed
					}
					if update == nil {
						log.Println("End of historical updates. Now receiving live updates...")
						continue
					}

					switch update.Operation() {
					case jetstream.KeyValuePut:
						var gameState components.GameState
						if err := json.Unmarshal(update.Value(), &gameState); err != nil {
							log.Printf("Error unmarshalling game state: %v", err)
							continue
						}

						log.Printf("Received update for game %v", gameState)

						c := components.GameBoard(&gameState)
						if err := sse.MergeFragmentTempl(c,
							datastar.WithSelectorID("gameboard"),
							datastar.WithMergeMorph(),
						); err != nil {
							sse.ConsoleError(err)
						}

					case jetstream.KeyValuePurge:
						sse.Redirect("/")
					}
				}
			}
		}

		watchGameLobby := func(ctx context.Context, sse *datastar.ServerSentEventGenerator, gameId, sessionId string) error {
			gameLobbyWatcher, err := gameLobbiesKV.Watch(ctx, gameId)
			if err != nil {
				return fmt.Errorf("failed to start game lobby watcher: %w", err)
			}
			defer gameLobbyWatcher.Stop()

			for {
				select {
				case <-ctx.Done():
					return nil // Exit if context is canceled
				case gameLobbyEntry, ok := <-gameLobbyWatcher.Updates():
					if !ok {
						return nil // Exit if the channel is closed
					}
					if gameLobbyEntry == nil {
						log.Println("End of historical updates. Now receiving live updates...")
						continue
					}

					switch gameLobbyEntry.Operation() {
					case jetstream.KeyValuePut:
						var gameLobby components.GameLobby
						if err := json.Unmarshal(gameLobbyEntry.Value(), &gameLobby); err != nil {
							log.Printf("Error unmarshalling game lobby: %v", err)
							continue
						}

						log.Printf("Received update for game lobby %v", gameLobby)

						currentUser, _, err := GetObject[components.User](ctx, usersKV, sessionId)
						if err != nil {
							return fmt.Errorf("failed to get current user: %w", err)
						}

						host, _, err := GetObject[components.User](ctx, usersKV, gameLobby.HostId)
						if err != nil {
							return fmt.Errorf("failed to get host user: %w", err)
						}

						var challenger *components.User
						if gameLobby.ChallengerId != "" {
							challenger, _, err = GetObject[components.User](ctx, usersKV, gameLobby.ChallengerId)
							if err != nil {
								return fmt.Errorf("failed to get challenger user: %w", err)
							}
						} else {
							challenger = &components.User{Name: ""}
						}

						c := components.GameControls(currentUser, host, challenger, &gameLobby)
						if err := sse.MergeFragmentTempl(c,
							datastar.WithSelectorID("gamecontrols"),
							datastar.WithMergeMorph(),
						); err != nil {
							sse.ConsoleError(err)
						}

					case jetstream.KeyValuePurge:
						sse.Redirect("/")
					}
				}
			}
		}

		handleUpdates := func(w http.ResponseWriter, r *http.Request) {
			sse := datastar.NewSSE(w, r)
			id := chi.URLParam(r, "id")
			if id == "" {
				sse.ExecuteScript("alert('Missing game ID')")
				sse.Redirect("/dashboard")
				return
			}

			sessionId, err := getSessionId(store, r)
			if err != nil || sessionId == "" {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}

			// Create a cancellable context for graceful shutdown
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()

			// Use a WaitGroup to wait for all watchers to finish
			var wg sync.WaitGroup
			wg.Add(2) // Two watchers: gameWatcher and gameLobbyWatcher

			// Start gameWatcher
			go func() {
				defer wg.Done()
				if err := watchGameBoard(ctx, sse, id); err != nil {
					log.Printf("Game board watcher error: %v", err)
				}
			}()

			// Start gameLobbyWatcher
			go func() {
				defer wg.Done()
				if err := watchGameLobby(ctx, sse, id, sessionId); err != nil {
					log.Printf("Game lobby watcher error: %v", err)
				}
			}()

			// Wait for all watchers to finish
			wg.Wait()
		}

		handleToggle := func(w http.ResponseWriter, r *http.Request) {
			sse := datastar.NewSSE(w, r)
			id := chi.URLParam(r, "id")
			if id == "" {
				sse.ExecuteScript("alert('Missing game ID')")
				sse.Redirect("/dashboard")
				return
			}

			sessionId, err := getSessionId(store, r)
			if err != nil || sessionId == "" {
				sse.ExecuteScript("alert('Error getting session ID')")
				sse.Redirect("/")
				return
			}

			gameLobby, _, err := GetObject[components.GameLobby](r.Context(), gameLobbiesKV, id)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
				return
			}

			gameState, entry, err := GetObject[components.GameState](r.Context(), gameBoardsKV, id)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
				return
			}

			cell := chi.URLParam(r, "cell")
			i, err := strconv.Atoi(cell)
			if err != nil || i < 0 || i >= len(gameState.Board) {
				sse.ExecuteScript("alert('Invalid cell index')")
				return
			}

			if gameState.Board[i] != "" {
				sse.ExecuteScript("alert('Cell already occupied')")
				return
			}

			if gameState.XIsNext && sessionId != gameLobby.HostId || !gameState.XIsNext && sessionId != gameLobby.ChallengerId {
				sse.ExecuteScript("alert('Not your turn')")
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
			} else if winner != "" {
				gameState.Winner = winner
			}

			if err := UpdateData(ctx, gameBoardsKV, gameState.Id, gameState, entry); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		handleReset := func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			if id == "" {
				http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
				return
			}

			gameState, entry, err := GetObject[components.GameState](ctx, gameBoardsKV, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			gameState.Board = [9]string{"", "", "", "", "", "", "", "", ""}
			gameState.Winner = ""
			gameState.XIsNext = true

			if err := UpdateData(ctx, gameBoardsKV, gameState.Id, gameState, entry); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		handleLeave := func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			sse := datastar.NewSSE(w, r)

			id := chi.URLParam(r, "id")
			if id == "" {
				sse.ExecuteScript("alert('Missing game ID')")
				sse.Redirect("/dashboard")
				return
			}

			gameLobby, entry, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			gameLobby.ChallengerId = ""
			err = UpdateData(ctx, gameLobbiesKV, gameLobby.Id, gameLobby, entry)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			sse.Redirect("/")
		}

		gameRouter.Get("/updates", handleUpdates)

		gameRouter.Post("/toggle/{cell}", handleToggle)

		gameRouter.Post("/reset", handleReset)

		gameRouter.Post("/leave", handleLeave)

	})

	return nil
}
