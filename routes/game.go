package routes

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"
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

	resolveName := func(user *components.User, SessionId string) string {
		if user.SessionId == SessionId {
			return "You"
		}
		return user.Name
	}

	router.Get("/game/{id}", func(w http.ResponseWriter, r *http.Request) {
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

		var gameName string
		gameLobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		gameName = gameLobby.Name

		var hostName string
		host, _, err := GetObject[components.User](ctx, usersKV, gameLobby.HostId)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		hostName = resolveName(host, sessionId)
		isHost := sessionId == gameLobby.HostId

		var challengerName string
		if gameLobby.ChallengerId != "" {
			challenger, _, err := GetObject[components.User](ctx, usersKV, gameLobby.ChallengerId)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
				return
			}
			challengerName = challenger.Name
		}

		gameState, _, err := GetObject[components.GameState](ctx, gameBoardsKV, id)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		pages.Game(gameState, gameName, hostName, challengerName, isHost).Render(r.Context(), w)
	})

	router.Route("/api/game", func(gameRouter chi.Router) {

		gameRouter.Route("/{id}", func(gameIdRouter chi.Router) {

			// gameIdRouter.Get("/watch", func(w http.ResponseWriter, r *http.Request) {
			// 	sse := datastar.NewSSE(w, r)
			// 	id := chi.URLParam(r, "id")
			// 	if id == "" {
			// 		sse.ExecuteScript("alert('Missing game ID')")
			// 		sse.Redirect("/dashboard")
			// 		return
			// 	}

			// 	sessionId, err := getSessionId(store, r)
			// 	if err != nil || sessionId == "" {
			// 		http.Redirect(w, r, "/", http.StatusSeeOther)
			// 		return
			// 	}

			// 	gameWatcher, err := gameBoardsKV.Watch(ctx, id)
			// 	if err != nil {
			// 		http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}
			// 	defer gameWatcher.Stop()

			// 	gameLobbyWatcher, err := gameLobbiesKV.Watch(ctx, id)
			// 	if err != nil {
			// 		http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}
			// 	defer gameLobbyWatcher.Stop()

			// 	// Use a single loop with select to handle multiple watchers
			// 	for {
			// 		select {
			// 		case update, ok := <-gameWatcher.Updates():
			// 			if !ok {
			// 				log.Println("Main watcher closed.")
			// 				return
			// 			}
			// 			handleGameUpdate(update, sse, id, gameLobby, sessionId)

			// 		case lobbyUpdate, ok := <-gameLobbyWatcher.Updates():
			// 			if !ok {
			// 				log.Println("Chat watcher closed.")
			// 				return
			// 			}
			// 			handleLobbyUpdate(lobbyUpdate, sse, id)
			// 		}
			// 	}
			// })

			// gameIdRouter.Post("/toggle/{cell}", func(w http.ResponseWriter, r *http.Request) {
			// 	sse := datastar.NewSSE(w, r)
			// 	id := chi.URLParam(r, "id")
			// 	if id == "" {
			// 		sse.ExecuteScript("alert('Missing game ID')")
			// 		sse.Redirect("/dashboard")
			// 		return
			// 	}

			// 	sessionId, err := getSessionId(store, r)
			// 	if err != nil || sessionId == "" {
			// 		sse.ExecuteScript("alert('Error getting session ID')")
			// 		sse.Redirect("/")
			// 		return
			// 	}

			// 	gameLobby, _, err := GetObject[components.GameLobby](r.Context(), gameLobbiesKV, id)
			// 	if err != nil {
			// 		http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}

			// 	gameState, entry, err := GetObject[components.GameState](r.Context(), gameBoardsKV, id)
			// 	if err != nil {
			// 		http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}

			// 	cell := chi.URLParam(r, "cell")
			// 	i, err := strconv.Atoi(cell)
			// 	if err != nil || i < 0 || i >= len(gameState.Board) {
			// 		sse.ExecuteScript("alert('Invalid cell index')")
			// 		return
			// 	}

			// 	if gameState.Board[i] != "" {
			// 		sse.ExecuteScript("alert('Cell already occupied')")
			// 		return
			// 	}

			// 	if gameState.XIsNext && sessionId != gameLobby.HostId || !gameState.XIsNext && sessionId != gameLobby.ChallengerId {
			// 		sse.ExecuteScript("alert('Not your turn')")
			// 		return
			// 	}

			// 	if gameState.XIsNext {
			// 		gameState.Board[i] = "X"
			// 	} else {
			// 		gameState.Board[i] = "O"
			// 	}
			// 	gameState.XIsNext = !gameState.XIsNext

			// 	winner := checkWinner(gameState.Board[:])
			// 	if winner == "TIE" {
			// 		gameState.Winner = "TIE"
			// 	} else if winner != "" {
			// 		gameState.Winner = winner
			// 	}

			// 	b, err := json.Marshal(gameState)
			// 	if err != nil {
			// 		http.Error(w, fmt.Sprintf("failed to marshal game state: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}

			// 	if _, err := gameBoardsKV.Update(ctx, gameState.Id, b, entry.Revision()); err != nil {
			// 		http.Error(w, fmt.Sprintf("failed to update game state: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}
			// })

			// gameIdRouter.Post("/reset", func(w http.ResponseWriter, r *http.Request) {
			// 	id := chi.URLParam(r, "id")
			// 	if id == "" {
			// 		http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
			// 		return
			// 	}

			// 	gameState, entry, err := GetObject[components.GameState](ctx, gameBoardsKV, id)
			// 	if err != nil {
			// 		http.Error(w, err.Error(), http.StatusInternalServerError)
			// 		return
			// 	}

			// 	gameState.Board = [9]string{"", "", "", "", "", "", "", "", ""}
			// 	gameState.Winner = ""
			// 	gameState.XIsNext = true

			// 	data, err := json.Marshal(gameState)
			// 	if err != nil {
			// 		http.Error(w, fmt.Sprintf("failed to marshal game state: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}
			// 	if _, err := gameBoardsKV.Update(ctx, gameState.Id, data, entry.Revision()); err != nil {
			// 		http.Error(w, fmt.Sprintf("failed to update game state: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}
			// })

			// gameIdRouter.Post("/leave", func(w http.ResponseWriter, r *http.Request) {
			// 	ctx := r.Context()

			// 	sse := datastar.NewSSE(w, r)
			// 	id := chi.URLParam(r, "id")
			// 	if id == "" {
			// 		sse.ExecuteScript("alert('Missing game ID')")
			// 		sse.Redirect("/dashboard")
			// 		return
			// 	}

			// 	gameLobby, entry, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
			// 	if err != nil {
			// 		http.Error(w, err.Error(), http.StatusInternalServerError)
			// 		return
			// 	}

			// 	gameLobby.ChallengerId = ""
			// 	data, err := json.Marshal(gameLobby)
			// 	if err != nil {
			// 		http.Error(w, fmt.Sprintf("failed to marshal game lobby: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}

			// 	if _, err := gameLobbiesKV.Update(ctx, gameLobby.Id, data, entry.Revision()); err != nil {
			// 		http.Error(w, fmt.Sprintf("failed to update game lobby: %v", err), http.StatusInternalServerError)
			// 		return
			// 	}

			// 	sse.Redirect("/")
			// })

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

// // Helper functions
// func handleGameUpdate(update jetstream.KeyValueEntry, sse *datastar.ServerSentEventGenerator, id string, gameLobby components.GameLobby, sessionId string) {
// 	if update == nil {
// 		fmt.Println("End of historical updates. Now receiving live updates...")
// 		return
// 	}

// 	switch update.Operation() { // No need for *update
// 	case jetstream.KeyValuePut:
// 		GameState := &components.GameState{}
// 		if err := json.Unmarshal(update.Value(), GameState); err != nil {
// 			log.Printf("Error unmarshalling update: %v", err)
// 			return
// 		}

// 		if GameState.Id != id {
// 			return
// 		}

// 		log.Printf("Received update for game %v", GameState)
// 		c := components.GameBoard(GameState, gameLobby, sessionId)
// 		if err := sse.MergeFragmentTempl(c,
// 			datastar.WithSelectorID("gameboard"),
// 			datastar.WithMergeMorph(),
// 		); err != nil {
// 			sse.ConsoleError(err)
// 		}

// 	case jetstream.KeyValueDelete:
// 		sse.Redirect("/")
// 	}
// }
