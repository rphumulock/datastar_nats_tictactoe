package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"
	datastar "github.com/starfederation/datastar/sdk/go"
)

// set game lobby all in here with join and everything

// do board in game.

func GetObject[T any](ctx context.Context, kv jetstream.KeyValue, key string) (*T, error) {
	entry, err := kv.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	var obj T
	if err := json.Unmarshal(entry.Value(), &obj); err != nil {
		return nil, fmt.Errorf("error unmarshalling object: %w", err)
	}

	return &obj, nil
}

func setupDashboardRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	gameLobbiesKV, err := js.KeyValue(ctx, "gameLobbies")
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

		user, err := GetObject[components.User](ctx, usersKV, sessionId)
		if err != nil {
			deleteSessionId(store, w, r)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		pages.Dashboard(user.Name).Render(r.Context(), w)
	})

	router.Route("/api/dashboard", func(dashboardRouter chi.Router) {

		dashboardRouter.Route("/{id}", func(gameIdRouter chi.Router) {

			gameIdRouter.Post("/join", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				id := chi.URLParam(r, "id")
				if id == "" {
					http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
					return
				}

				gameLobby, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
				if err != nil {
					deleteSessionId(store, w, r)
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}
				gameLobby.ChallengerId = id
				bytes, err = json.Marshal(gameboard)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to marshal mvc: %v", err), http.StatusInternalServerError)
					return
				}
				_, err = gameBoardsKV.Put(r.Context(), id, bytes)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to put key value: %v", err), http.StatusInternalServerError)
					return
				}

			})

			gameIdRouter.Delete("/delete", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()

				// Extract the "id" parameter from the URL
				id := chi.URLParam(r, "id")
				if id == "" {
					http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
					return
				}
				log.Printf("id: %s", id)

				if err := gameLobbiesKV.Delete(ctx, id); err != nil {
					http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", id, err), http.StatusInternalServerError)
					return
				}

				if err := gameBoardsKV.Delete(ctx, id); err != nil {
					http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", id, err), http.StatusInternalServerError)
					return
				}
			})

		})

		dashboardRouter.Post("/create", func(w http.ResponseWriter, r *http.Request) {
			sessionId, err := getSessionId(store, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Set User

			id := toolbelt.NextEncodedID()
			gameLobby := components.GameLobby{
				Id: id,
			}
			bytes, err := json.Marshal(gameLobby)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to marshal mvc: %v", err), http.StatusInternalServerError)
				return
			}
			_, err = gameLobbiesKV.Put(r.Context(), id, bytes)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to put key value: %v", err), http.StatusInternalServerError)
				return
			}

			gameboard := components.GameState{
				Id:             id,
				HostId:         sessionId,
				HostName:       user.Name,
				ChallengerId:   "",
				ChallengerName: "",
			}
			bytes, err = json.Marshal(gameboard)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to marshal mvc: %v", err), http.StatusInternalServerError)
				return
			}
			_, err = gameBoardsKV.Put(r.Context(), id, bytes)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to put key value: %v", err), http.StatusInternalServerError)
				return
			}
		})

		dashboardRouter.Get("/watch", func(w http.ResponseWriter, r *http.Request) {
			sse := datastar.NewSSE(w, r)

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

			for update := range watcher.Updates() {

				if update == nil {
					fmt.Println("End of historical updates. Now receiving live updates...")
					historicalMode = false
					continue
				}

				log.Printf("Received update: %s", update.Value())

				switch update.Operation() {
				case jetstream.KeyValuePut:

					var gameLobby components.GameLobby
					if err := json.Unmarshal(update.Value(), &gameLobby); err != nil {
						log.Printf("Error unmarshalling update value: %v", err)
						return
					}

					entry, err := gameBoardsKV.Get(ctx, gameLobby.Id)
					if err != nil {
						log.Printf("Failed to get game state for key %s: %v", gameLobby.Id, err)
						continue
					}
					gameState := &components.GameState{}
					if err := json.Unmarshal(entry.Value(), gameState); err != nil {
						http.Error(w, fmt.Errorf("error unmarshalling user: %w", err).Error(), http.StatusInternalServerError)
						return
					}

					c := components.DashboardItem(gameState, sessionId)
					if err := sse.MergeFragmentTempl(c,
						datastar.WithSelectorID("list-container"),
						datastar.WithMergeAppend(),
					); err != nil {
						sse.ConsoleError(err)
					}

				case jetstream.KeyValueDelete:

					if historicalMode {
						log.Printf("Ignoring historical delete for key: %s", update.Key())
						continue
					}

					if err := sse.RemoveFragments("#game-"+update.Key(),
						datastar.WithRemoveSettleDuration(1*time.Millisecond),
						datastar.WithRemoveUseViewTransitions(true),
					); err != nil {
						sse.ConsoleError(err)
					}

				}
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
				entry, err := gameBoardsKV.Get(context.Background(), key)
				if err != nil {
					log.Printf("Failed to get value for key %s: %v", key, err)
					continue
				}

				var gameState components.GameState
				if err := json.Unmarshal(entry.Value(), &gameState); err != nil {
					log.Printf("Error unmarshalling update value: %v", err)
					return
				}

				if gameState.HostId == sessionId {
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

		dashboardRouter.Delete("/purge", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			sse := datastar.NewSSE(w, r)

			keys, err := gameLobbiesKV.Keys(ctx)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error listing keys: %v", err), http.StatusInternalServerError)
				log.Printf("Error listing keys: %v", err)
				return
			}

			for _, key := range keys {
				err = gameLobbiesKV.Delete(ctx, key)
				if err != nil {
					log.Printf("Error deleting key '%s': %v", key, err)
					continue
				}

				err = gameBoardsKV.Delete(ctx, key)
				if err != nil {
					log.Printf("Error deleting key '%s': %v", key, err)
					continue
				}

				log.Printf("Deleted key: %s", key)

				if err := sse.RemoveFragments("game-"+key,
					datastar.WithRemoveSettleDuration(1*time.Millisecond),
					datastar.WithRemoveUseViewTransitions(true),
				); err != nil {
					sse.ConsoleError(err)
				}
			}

			fmt.Fprintln(w, "All games have been purged.")
		})

	})

	return nil
}
