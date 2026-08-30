package routes

import (
	"context"
	"errors"
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
	datastar "github.com/starfederation/datastar-go/datastar"
)

// Sentinel errors from the board mutation, translated into player-facing
// messages by the handler. They are raised inside the CAS retry, so each one is
// re-evaluated against fresh state on every attempt.
var (
	errInvalidCell = errors.New("invalid cell index")
	errGameOver    = errors.New("game already decided")
	errCellTaken   = errors.New("cell already occupied")
	errNotYourTurn = errors.New("not your turn")
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

		currentUser, _, err := GetObject[components.User](r.Context(), usersKV, sessionId)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		gameLobby, _, err := GetObject[components.GameLobby](r.Context(), gameLobbiesKV, id)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		host, _, err := GetObject[components.User](r.Context(), usersKV, gameLobby.HostId)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		var challenger *components.User
		if gameLobby.ChallengerId != "" {
			challenger, _, err = GetObject[components.User](r.Context(), usersKV, gameLobby.ChallengerId)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
				return
			}
		} else {
			challenger = &components.User{}
			challenger.Name = ""
		}

		gameState, _, err := GetObject[components.GameState](r.Context(), gameBoardsKV, id)
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

		// renderGameContent reloads everything the view needs and morphs the whole
		// #game-container, so a win swaps the entire page for the victory screen.
		renderGameContent := func(ctx context.Context, sse *datastar.ServerSentEventGenerator, gameId, sessionId string) error {
			gameLobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, gameId)
			if err != nil {
				return fmt.Errorf("failed to get game lobby: %w", err)
			}

			gameState, _, err := GetObject[components.GameState](ctx, gameBoardsKV, gameId)
			if err != nil {
				return fmt.Errorf("failed to get game state: %w", err)
			}

			currentUser, _, err := GetObject[components.User](ctx, usersKV, sessionId)
			if err != nil {
				return fmt.Errorf("failed to get current user: %w", err)
			}

			host, _, err := GetObject[components.User](ctx, usersKV, gameLobby.HostId)
			if err != nil {
				return fmt.Errorf("failed to get host user: %w", err)
			}

			challenger := &components.User{}
			if gameLobby.ChallengerId != "" {
				challenger, _, err = GetObject[components.User](ctx, usersKV, gameLobby.ChallengerId)
				if err != nil {
					return fmt.Errorf("failed to get challenger user: %w", err)
				}
			}

			c := components.GameContent(currentUser, host, challenger, gameLobby, gameState)
			return sse.PatchElementTempl(c,
				datastar.WithSelectorID("game-container"),
			)
		}

		// watchGame drives one KV watcher, re-rendering the view on every change.
		// The board and lobby buckets both feed the same render.
		watchGame := func(ctx context.Context, sse *datastar.ServerSentEventGenerator, kv jetstream.KeyValue, gameId, sessionId string) error {
			watcher, err := kv.Watch(ctx, gameId)
			if err != nil {
				return fmt.Errorf("failed to start watcher: %w", err)
			}
			defer watcher.Stop()

			for {
				select {
				case <-ctx.Done():
					return nil
				case update, ok := <-watcher.Updates():
					if !ok {
						return nil
					}
					if update == nil {
						continue // end of the historical replay
					}

					switch update.Operation() {
					case jetstream.KeyValuePut:
						// A transient read failure shouldn't tear down the player's
						// live connection, so log it and keep watching.
						if err := renderGameContent(ctx, sse, gameId, sessionId); err != nil {
							log.Printf("game %s: render failed: %v", gameId, err)
						}
					case jetstream.KeyValuePurge, jetstream.KeyValueDelete:
						sse.Redirect("/")
						return nil
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

			// Start the game board watcher
			go func() {
				defer wg.Done()
				if err := watchGame(ctx, sse, gameBoardsKV, id, sessionId); err != nil {
					log.Printf("Game board watcher error: %v", err)
				}
			}()

			// Start the game lobby watcher
			go func() {
				defer wg.Done()
				if err := watchGame(ctx, sse, gameLobbiesKV, id, sessionId); err != nil {
					log.Printf("Game lobby watcher error: %v", err)
				}
			}()

			// Wait for all watchers to finish
			wg.Wait()
		}

		handleToggle := func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
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

			gameLobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get game lobby: %v", err), http.StatusInternalServerError)
				return
			}

			i, err := strconv.Atoi(chi.URLParam(r, "cell"))
			if err != nil {
				sse.ExecuteScript("alert('Invalid cell index')")
				return
			}

			// Every rule lives inside the mutation so it is re-checked against
			// the winning state when the other player's move lands first.
			_, err = UpdateObject(ctx, gameBoardsKV, id, func(gameState *components.GameState) error {
				if i < 0 || i >= len(gameState.Board) {
					return errInvalidCell
				}
				if gameState.Winner != "" {
					return errGameOver
				}
				if gameState.Board[i] != "" {
					return errCellTaken
				}
				if gameState.XIsNext && sessionId != gameLobby.HostId ||
					!gameState.XIsNext && sessionId != gameLobby.ChallengerId {
					return errNotYourTurn
				}

				if gameState.XIsNext {
					gameState.Board[i] = "X"
				} else {
					gameState.Board[i] = "O"
				}
				gameState.XIsNext = !gameState.XIsNext
				gameState.Winner = checkWinner(gameState.Board[:])
				return nil
			})

			switch {
			case errors.Is(err, errInvalidCell):
				sse.ExecuteScript("alert('Invalid cell index')")
			case errors.Is(err, errGameOver):
				sse.ExecuteScript("alert('This game is already over')")
			case errors.Is(err, errCellTaken):
				sse.ExecuteScript("alert('Cell already occupied')")
			case errors.Is(err, errNotYourTurn):
				sse.ExecuteScript("alert('Not your turn')")
			case err != nil:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		handleReset := func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			if id == "" {
				http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
				return
			}

			if _, err := UpdateObject(r.Context(), gameBoardsKV, id, func(gameState *components.GameState) error {
				gameState.Board = [9]string{}
				gameState.Winner = ""
				gameState.XIsNext = true
				return nil
			}); err != nil {
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

			if _, err := UpdateObject(ctx, gameLobbiesKV, id, func(gameLobby *components.GameLobby) error {
				gameLobby.ChallengerId = ""
				return nil
			}); err != nil {
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
