package routes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"github.com/goombaio/namegenerator"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar-nats-tictactoe/web/components"
	"github.com/rphumulock/datastar-nats-tictactoe/web/pages"

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

	presenceKV, err := js.KeyValue(ctx, "presence")
	if err != nil {
		return fmt.Errorf("failed to get presence key value: %w", err)
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

		if err := pages.Dashboard(user.Name).Render(r.Context(), w); err != nil {
			log.Printf("dashboard: render failed: %v", err)
		}
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

	createGameLobby := func(id, name, sessionId, hostName string) components.GameLobby {
		return components.GameLobby{
			Id:           id,
			Name:         name,
			HostId:       sessionId,
			HostName:     hostName,
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
		// Best effort: a missing user record just leaves the name blank rather
		// than blocking creation, which would change this endpoint's semantics.
		hostName := ""
		if host, _, err := GetObject[components.User](r.Context(), usersKV, sessionId); err == nil {
			hostName = host.Name
		}

		id, name := generateGameDetails()
		gameLobby := createGameLobby(id, name, sessionId, hostName)
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
		sseRedirect(datastar.NewSSE(w, r), "/")
	}

	// renderDashboard rebuilds the entire lobby list and morphs #list-container.
	// One render path covers creates, joins and deletes: morphing patches only
	// what actually changed, which is simpler and cheaper than tracking per-card
	// selectors plus a History() lookup on every event.
	renderDashboard := func(ctx context.Context, sse *datastar.ServerSentEventGenerator, sessionId string) {
		keys, err := gameLobbiesKV.Keys(ctx)
		if err != nil && !errors.Is(err, jetstream.ErrNoKeysFound) {
			log.Printf("Error listing game lobbies: %v", err)
			return
		}

		// A lobby whose host has no live SSE stream is unplayable: nobody can
		// join them and only they could delete it. Hide those immediately; the
		// reaper deletes them a grace period later.
		present, err := presentSessions(ctx, presenceKV)
		if err != nil {
			log.Printf("Error reading presence: %v", err)
			return
		}

		lobbies := make([]components.GameLobby, 0, len(keys))
		for _, key := range keys {
			lobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, key)
			if err != nil {
				continue // raced with a delete; it just won't be in this render
			}
			if _, hostHere := present[lobby.HostId]; !hostHere {
				continue
			}
			lobbies = append(lobbies, *lobby)
		}
		// Keys() has no defined order, so sort to keep the cards from reshuffling.
		sort.Slice(lobbies, func(i, j int) bool { return lobbies[i].Id < lobbies[j].Id })

		// The fragment root carries id="list-container" and the default patch mode
		// is outer, which matches on that id, so no selector is needed.
		// A failed patch means the stream is broken, so reporting it back down
		// that same stream would fail too - log it here instead.
		if err := sse.PatchElementTempl(components.DashboardList(lobbies, sessionId)); err != nil {
			log.Printf("dashboard: patch failed: %v", err)
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

		// Heartbeat before the first render, so this session's own lobbies are
		// never filtered out of the list it is about to receive.
		keepPresence(ctx, presenceKV, sessionId)

		watcher, err := gameLobbiesKV.WatchAll(ctx)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
			return
		}
		defer func() {
			if err := watcher.Stop(); err != nil {
				log.Printf("dashboard: failed to stop watcher: %v", err)
			}
		}()

		live := false
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
				// A nil entry marks the end of the historical replay. Render once
				// there rather than per replayed key, then on every live change.
				if entry == nil {
					live = true
				}
				if live {
					renderDashboard(ctx, sse, sessionId)
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
				toastWarning(sse, "Another player has already joined. Game is full.")
				return
			}

			gameLobby.ChallengerId = sessionID

			if err := UpdateData(ctx, gameLobbiesKV, id, gameLobby, entry); err != nil {
				toastWarning(sse, "Someone else joined first. This lobby is now full.")
				return
			}
		}

		sseRedirect(sse, "/game/"+id)
	}

	handleDelete := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
			return
		}

		sessionId, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if sessionId == "" {
			http.Error(w, "not logged in", http.StatusUnauthorized)
			return
		}

		gameLobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
		if err != nil {
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}

		// The card's disabled delete button is presentation only; the lobby
		// belongs to its host and this is where that is actually enforced.
		if sessionId != gameLobby.HostId {
			http.Error(w, "only the host can delete this game", http.StatusForbidden)
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
