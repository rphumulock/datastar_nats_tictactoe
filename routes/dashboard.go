package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"github.com/goombaio/namegenerator"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"

	datastar "github.com/starfederation/datastar-go/datastar"
)

func setupDashboardRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	gameLobbiesKV, err := js.KeyValue(ctx, "gameLobbies")
	if err != nil {
		return fmt.Errorf("failed to get game lobbies key value: %w", err)
	}

	gameBoardsKV, err := js.KeyValue(ctx, "gameBoards")
	if err != nil {
		return fmt.Errorf("failed to get game lobbies key value: %w", err)
	}

	usersKV, err := js.KeyValue(ctx, "users")
	if err != nil {
		return fmt.Errorf("failed to get users key value: %w", err)
	}

	handleGetDashboard := func(w http.ResponseWriter, r *http.Request) {
		sessionId, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if sessionId == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		user, _, err := GetObject[components.User](r.Context(), usersKV, sessionId)
		if err != nil {
			deleteSessionId(store, w, r)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		pages.Dashboard(user.Name).Render(r.Context(), w)
	}

	router.Get("/dashboard", handleGetDashboard)

	// API

	generateGameDetails := func() (string, string) {
		id := toolbelt.NextEncodedID()
		seed := time.Now().UTC().UnixNano()
		nameGenerator := namegenerator.NewNameGenerator(seed)
		name := strings.ToUpper(nameGenerator.Generate())
		return id, name
	}

	createGameLobby := func(id, name, sessionId string) components.GameLobby {
		return components.GameLobby{
			Id:           id,
			Name:         name,
			HostId:       sessionId,
			ChallengerId: "",
		}
	}

	createGameState := func(id string) components.GameState {
		return components.GameState{
			Id:      id,
			Board:   [9]string{},
			XIsNext: true,
			Winner:  "",
		}
	}

	handleCreate := func(w http.ResponseWriter, r *http.Request) {
		sessionId, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id, name := generateGameDetails()
		gameLobby := createGameLobby(id, name, sessionId)
		if err := PutData(r.Context(), gameLobbiesKV, id, gameLobby); err != nil {
			http.Error(w, fmt.Sprintf("failed to store game lobby: %v", err), http.StatusInternalServerError)
			return
		}
		gameState := createGameState(id)
		if err := PutData(r.Context(), gameBoardsKV, id, gameState); err != nil {
			http.Error(w, fmt.Sprintf("failed to store game state: %v", err), http.StatusInternalServerError)
			return
		}
	}

	handleLogout := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sessionId, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if sessionId == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		keys, err := gameLobbiesKV.Keys(ctx)
		if err != nil {
			log.Printf("%v", err)
		}

		for _, key := range keys {
			gameLobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, key)
			if err != nil {
				log.Printf("Failed to get value for key %s: %v", key, err)
				continue
			}

			switch sessionId {
			case gameLobby.HostId:
				// The host owns the game, so leaving tears it down.
				if err := gameLobbiesKV.Delete(ctx, key); err != nil {
					log.Printf("Failed to delete lobby %s: %v", key, err)
				}
				if err := gameBoardsKV.Delete(ctx, key); err != nil {
					log.Printf("Failed to delete board %s: %v", key, err)
				}
			case gameLobby.ChallengerId:
				// Leaving as the challenger just reopens the lobby.
				if _, err := UpdateObject(ctx, gameLobbiesKV, key, func(lobby *components.GameLobby) error {
					lobby.ChallengerId = ""
					return nil
				}); err != nil {
					log.Printf("Failed to clear challenger on lobby %s: %v", key, err)
				}
			}
		}

		if err := usersKV.Delete(ctx, sessionId); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", sessionId, err), http.StatusInternalServerError)
			return
		}
		deleteSessionId(store, w, r)
		sse := datastar.NewSSE(w, r)
		sse.Redirect("/")
	}

	handleHistoricalUpdates := func(dashboardItems []components.GameLobby, sessionId string, sse *datastar.ServerSentEventGenerator) {
		if len(dashboardItems) == 0 {
			return
		}

		c := components.DashboardList(dashboardItems, sessionId)
		if err := sse.PatchElementTempl(c); err != nil {
			sse.ConsoleError(err)
		}
		dashboardItems = nil
	}

	handleKeyValueDelete := func(historicalMode bool, update jetstream.KeyValueEntry, sse *datastar.ServerSentEventGenerator) {
		if historicalMode {
			log.Printf("Ignoring historical delete for key: %s", update.Key())
			return
		}

		if err := sse.RemoveElement("#game-"+update.Key(),
			datastar.WithoutViewTransitions()); err != nil {
			sse.ConsoleError(err)
		}

	}

	handleKeyValuePut := func(
		ctx context.Context,
		historicalMode bool,
		dashboardItems *[]components.GameLobby,
		entry jetstream.KeyValueEntry,
		sessionId string,
		sse *datastar.ServerSentEventGenerator,
	) {
		var gameLobby components.GameLobby
		if err := json.Unmarshal(entry.Value(), &gameLobby); err != nil {
			log.Printf("Error unmarshalling update value for key %s: %v", entry.Key(), err)
			return
		}

		if historicalMode {
			*dashboardItems = append(*dashboardItems, gameLobby)
			return
		}

		history, err := gameLobbiesKV.History(ctx, entry.Key())
		if err != nil {
			log.Printf("Error getting history for key %s: %v", entry.Key(), err)
			return
		}

		c := components.DashboardListItem(&gameLobby, sessionId)
		if len(history) == 1 {
			if err := sse.PatchElementTempl(c,
				datastar.WithSelectorID("list-container"),
				datastar.WithModeAppend()); err != nil {
				sse.ConsoleError(err)
			}
		} else {
			if err := sse.PatchElementTempl(c,
				datastar.WithSelectorID("game-"+entry.Key())); err != nil {
				sse.ConsoleError(err)
			}
		}
	}

	handleUpdates := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sse := datastar.NewSSE(w, r)

		sessionId, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		watcher, err := gameLobbiesKV.WatchAll(ctx)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
			return
		}
		defer watcher.Stop()

		historicalMode := true
		dashboardItems := &[]components.GameLobby{}

		for {
			select {
			case <-ctx.Done():
				log.Println("Context canceled, stopping watcher updates")
				return
			case entry, ok := <-watcher.Updates():
				if !ok {
					log.Println("Watcher updates channel closed")
					return
				}

				if entry == nil {
					handleHistoricalUpdates(*dashboardItems, sessionId, sse)
					historicalMode = false
					continue
				}

				switch entry.Operation() {
				case jetstream.KeyValuePut:
					handleKeyValuePut(ctx, historicalMode, dashboardItems, entry, sessionId, sse)
				case jetstream.KeyValuePurge, jetstream.KeyValueDelete:
					handleKeyValueDelete(historicalMode, entry, sse)
				}
			}
		}
	}

	handleJoin := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sse := datastar.NewSSE(w, r)

		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
			return
		}

		sessionID, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to get session: %v", err), http.StatusInternalServerError)
			return
		}
		if sessionID == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		gameLobby, entry, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if sessionID != gameLobby.HostId {

			if gameLobby.ChallengerId != "" && gameLobby.ChallengerId != sessionID {
				sse.ExecuteScript("alert('Another player has already joined. Game is full.');")
				return
			}

			gameLobby.ChallengerId = sessionID

			if err := UpdateData(ctx, gameLobbiesKV, id, gameLobby, entry); err != nil {
				sse.ExecuteScript("alert('Someone else joined first. This lobby is now full.');")
				return
			}
		}

		sse.Redirect("/game/" + id)
	}

	handleDelete := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
			return
		}

		if err := gameLobbiesKV.Purge(ctx, id); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", id, err), http.StatusInternalServerError)
			return
		}
		if err := gameBoardsKV.Purge(ctx, id); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", id, err), http.StatusInternalServerError)
			return
		}
	}

	router.Route("/api/dashboard", func(dashboardRouter chi.Router) {

		dashboardRouter.Post("/create", handleCreate)

		dashboardRouter.Post("/logout", handleLogout)

		dashboardRouter.Get("/updates", handleUpdates)

		dashboardRouter.Route("/{id}", func(gameIdRouter chi.Router) {

			gameIdRouter.Post("/join", handleJoin)

			gameIdRouter.Delete("/delete", handleDelete)

		})

	})

	return nil
}
