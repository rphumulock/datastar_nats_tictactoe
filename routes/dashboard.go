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

func GetObject[T any](ctx context.Context, kv jetstream.KeyValue, key string) (*T, jetstream.KeyValueEntry, error) {
	entry, err := kv.Get(ctx, key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	var obj T
	if err := json.Unmarshal(entry.Value(), &obj); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal value for key %s: %w", key, err)
	}

	return &obj, entry, nil
}

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

				sessionId, err := getSessionId(store, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				user, _, err := GetObject[components.User](ctx, usersKV, sessionId)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
					return
				}

				gameLobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, id)
				if err != nil {
					deleteSessionId(store, w, r)
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}

				sse := datastar.NewSSE(w, r)
				if sessionId != gameLobby.HostId {

					gameLobby.ChallengerId = sessionId
					gameLobby.ChallengerName = user.Name
					gameLobby.Status = "full"
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

			user, _, err := GetObject[components.User](r.Context(), usersKV, sessionId)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get user: %v", err), http.StatusInternalServerError)
				return
			}

			id := toolbelt.NextEncodedID()
			gameLobby := components.GameLobby{
				Id:             id,
				HostId:         sessionId,
				HostName:       user.Name,
				ChallengerId:   "",
				ChallengerName: "",
				Status:         "created",
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

			gameState := components.GameState{
				Id:      id,
				Board:   [9]string{},
				XIsNext: true,
				Winner:  "",
			}
			bytes, err = json.Marshal(gameState)
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

				switch update.Operation() {
				case jetstream.KeyValuePut:

					var gameLobby components.GameLobby
					if err := json.Unmarshal(update.Value(), &gameLobby); err != nil {
						log.Printf("Error unmarshalling update value: %v", err)
						return
					}

					c := components.DashboardItem(&gameLobby, sessionId)

					if historicalMode {

						sse.RemoveFragments("#game-"+update.Key(),
							datastar.WithRemoveSettleDuration(1*time.Millisecond),
							datastar.WithRemoveUseViewTransitions(false),
						)

						if err := sse.MergeFragmentTempl(c,
							datastar.WithSelectorID("list-container"),
							datastar.WithMergeAppend(),
						); err != nil {
							sse.ConsoleError(err)
						}

					} else { // if live mode

						if gameLobby.Status == "created" {
							if err := sse.MergeFragmentTempl(c,
								datastar.WithSelectorID("list-container"),
								datastar.WithMergeAppend(),
							); err != nil {
								sse.ConsoleError(err)
							}
						} else if gameLobby.Status == "open" {

							if gameLobby.HostId == sessionId {
								if err := sse.MergeFragmentTempl(c,
									datastar.WithSelectorID("game-"+update.Key()),
									datastar.WithMergeMorph(),
								); err != nil {
									sse.ConsoleError(err)
								}
							} else if gameLobby.ChallengerId == sessionId {
								if err := sse.MergeFragmentTempl(c,
									datastar.WithSelectorID("game-"+update.Key()),
									datastar.WithMergeMorph(),
								); err != nil {
									sse.ConsoleError(err)
								}
							} else {
								if err := sse.MergeFragmentTempl(c,
									datastar.WithSelectorID("list-container"),
									datastar.WithMergeAppend(),
								); err != nil {
									sse.ConsoleError(err)
								}
							}

						} else if gameLobby.Status == "full" {

							if gameLobby.HostId == sessionId {
								if err := sse.MergeFragmentTempl(c,
									datastar.WithSelectorID("game-"+update.Key()),
									datastar.WithMergeMorph(),
								); err != nil {
									sse.ConsoleError(err)
								}
							} else if gameLobby.ChallengerId == sessionId {
								if err := sse.MergeFragmentTempl(c,
									datastar.WithSelectorID("game-"+update.Key()),
									datastar.WithMergeMorph(),
								); err != nil {
									sse.ConsoleError(err)
								}
							} else {
								sse.RemoveFragments("#game-"+update.Key(),
									datastar.WithRemoveSettleDuration(1*time.Millisecond),
									datastar.WithRemoveUseViewTransitions(true),
								)
							}
						}
					}

				case jetstream.KeyValueDelete:

					if historicalMode {
						log.Printf("Ignoring historical delete for key: %s", update.Key())
						continue
					}

					sse.RemoveFragments("#game-"+update.Key(),
						datastar.WithRemoveSettleDuration(1*time.Millisecond),
						datastar.WithRemoveUseViewTransitions(false),
					)

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
