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

	datastar "github.com/starfederation/datastar/sdk/go"
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

	router.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		sessionId, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if sessionId == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		user, _, err := GetObject[components.User](ctx, usersKV, sessionId)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		pages.Dashboard(user.Name).Render(r.Context(), w)
	})

	router.Route("/api/dashboard", func(dashboardRouter chi.Router) {

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

		dashboardRouter.Post("/create", func(w http.ResponseWriter, r *http.Request) {
			sessionId, err := getSessionId(store, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			id, name := generateGameDetails()
			gameLobby := createGameLobby(id, name, sessionId)

			log.Printf("Created game: %s (ID: %s)", name, id)

			if err := storeData(r.Context(), gameLobbiesKV, id, gameLobby); err != nil {
				http.Error(w, fmt.Sprintf("failed to store game lobby: %v", err), http.StatusInternalServerError)
				return
			}

			gameState := createGameState(id)
			if err := storeData(r.Context(), gameBoardsKV, id, gameState); err != nil {
				http.Error(w, fmt.Sprintf("failed to store game state: %v", err), http.StatusInternalServerError)
				return
			}
		})

		dashboardRouter.Post("/logout", func(w http.ResponseWriter, r *http.Request) {
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
				entry, err := gameLobbiesKV.Get(context.Background(), key)
				if err != nil {
					log.Printf("Failed to get value for key %s: %v", key, err)
					continue
				}

				var gameLobby components.GameLobby
				if err := json.Unmarshal(entry.Value(), &gameLobby); err != nil {
					log.Printf("Error unmarshalling update value: %v", err)
					return
				}

				if gameLobby.HostId == sessionId {
					gameLobbiesKV.Delete(ctx, key)
					gameBoardsKV.Delete(ctx, key)
				}
			}

			if err := usersKV.Delete(ctx, sessionId); err != nil {
				http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", sessionId, err), http.StatusInternalServerError)
				return
			}
			deleteSessionId(store, w, r)
			sse := datastar.NewSSE(w, r)
			sse.Redirect("/")
		})

		dashboardRouter.Get("/updates", func(w http.ResponseWriter, r *http.Request) {
			sessionId, err := getSessionId(store, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			ctx := r.Context()

			watcher, err := gameLobbiesKV.WatchAll(ctx)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
				return
			}
			defer watcher.Stop()

			historicalMode := true
			var dashboardItems []components.GameLobby

			sse := datastar.NewSSE(w, r)

			handleHistoricalUpdates := func() {
				if len(dashboardItems) == 0 {
					return // No updates needed
				}

				c := components.DashboardList(dashboardItems, sessionId)
				if err := sse.MergeFragmentTempl(c); err != nil {
					sse.ConsoleError(err)
				}
				dashboardItems = nil // Reset instead of reallocation
			}

			handleKeyValuePut := func(update jetstream.KeyValueEntry) {
				var gameLobby components.GameLobby
				if err := json.Unmarshal(update.Value(), &gameLobby); err != nil {
					log.Printf("Error unmarshalling update value for key %s: %v", update.Key(), err)
					return
				}

				if historicalMode {
					dashboardItems = append(dashboardItems, gameLobby)
					return
				}

				history, err := gameLobbiesKV.History(r.Context(), update.Key())
				if err != nil {
					log.Printf("Error getting history for key %s: %v", update.Key(), err)
					return
				}

				if len(history) == 1 {
					c := components.DashboardListItem(&gameLobby, sessionId)
					if err := sse.MergeFragmentTempl(c,
						datastar.WithSelectorID("list-container"),
						datastar.WithMergeAppend()); err != nil {
						sse.ConsoleError(err)
					}
				} else {
					c := components.DashboardListItem(&gameLobby, sessionId)
					if err := sse.MergeFragmentTempl(c,
						datastar.WithSelectorID("list-container"),
						datastar.WithMergeMorph()); err != nil {
						sse.ConsoleError(err)
					}
				}

			}

			handleKeyValueDelete := func(update jetstream.KeyValueEntry) {
				if historicalMode {
					log.Printf("Ignoring historical delete for key: %s", update.Key())
					return
				}

				if err := sse.RemoveFragments("#game-"+update.Key(),
					datastar.WithRemoveSettleDuration(1*time.Millisecond),
					datastar.WithRemoveUseViewTransitions(false)); err != nil {
					sse.ConsoleError(err)
				}

			}

			for update := range watcher.Updates() {
				if update == nil {
					handleHistoricalUpdates()
					historicalMode = false
					continue
				}

				switch update.Operation() {
				case jetstream.KeyValuePut:
					handleKeyValuePut(update)
				case jetstream.KeyValuePurge:
					handleKeyValueDelete(update)
				}
			}
		})

		dashboardRouter.Delete("/purge", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			keys, err := gameLobbiesKV.Keys(ctx)
			if err != nil {
				log.Printf("Error listing keys: %v", err)
				return
			}

			for _, key := range keys {
				err = gameLobbiesKV.Purge(ctx, key)
				if err != nil {
					log.Printf("Error deleting key '%s': %v", key, err)
					continue
				}

				err = gameBoardsKV.Purge(ctx, key)
				if err != nil {
					log.Printf("Error deleting key '%s': %v", key, err)
					continue
				}

				log.Printf("Deleted key: %s", key)
			}

			fmt.Fprintln(w, "All games have been purged.")
		})

		dashboardRouter.Route("/{id}", func(gameIdRouter chi.Router) {

			gameIdRouter.Post("/join", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()

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

				sse := datastar.NewSSE(w, r)

				if sessionID != gameLobby.HostId {

					if gameLobby.ChallengerId != "" && gameLobby.ChallengerId != sessionID {
						sse.Redirect("/dashboard")
						sse.ExecuteScript("alert('Another player has already joined. Game is full.');")
						return
					}

					updatedLobby := *gameLobby
					updatedLobby.ChallengerId = sessionID

					updatedBytes, err := json.Marshal(updatedLobby)
					if err != nil {
						http.Error(w, fmt.Sprintf("failed to marshal updated lobby: %v", err), http.StatusInternalServerError)
						return
					}

					_, err = gameLobbiesKV.Update(ctx, id, updatedBytes, entry.Revision())
					if err != nil {
						sse.ExecuteScript("alert('Someone else joined first. This lobby is now full.');")
						sse.Redirect("/dashboard")
						return
					}
				}

				sse.Redirect("/game/" + id)
			})

			gameIdRouter.Delete("/delete", func(w http.ResponseWriter, r *http.Request) {
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
			})

		})

	})

	return nil
}
