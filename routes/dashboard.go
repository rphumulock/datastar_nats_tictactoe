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

func setupDashboardRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	gamesKV, err := js.KeyValue(ctx, "games")
	if err != nil {
		return fmt.Errorf("failed to get games key value: %w", err)
	}
	openGamesKV, err := js.KeyValue(ctx, "openGames")
	if err != nil {
		return fmt.Errorf("failed to get games key value: %w", err)
	}
	usersKV, err := js.KeyValue(ctx, "users")
	if err != nil {
		return fmt.Errorf("failed to get games key value: %w", err)
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
		entry, err := usersKV.Get(ctx, sessionId)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		user := &components.User{}
		if err := json.Unmarshal(entry.Value(), user); err != nil {
			http.Error(w, fmt.Errorf("error unmarshalling game state: %w", err).Error(), http.StatusInternalServerError)
			return
		}

		pages.Dashboard(user.Name).Render(r.Context(), w)
	})

	router.Post("/logout", func(w http.ResponseWriter, r *http.Request) {
		sessionId, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if sessionId == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if err := usersKV.Delete(ctx, sessionId); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", sessionId, err), http.StatusInternalServerError)
			return
		}
		keys, err := gamesKV.Keys(ctx)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error listing keys: %v", err), http.StatusInternalServerError)
			log.Printf("Error listing keys: %v", err)
			return
		}

		for _, key := range keys {
			entry, err := gamesKV.Get(context.Background(), key)
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
				gamesKV.Delete(ctx, key)
				openGamesKV.Delete(ctx, key)
			}
		}

		deleteSessionId(store, w, r)
		sse := datastar.NewSSE(w, r)
		sse.Redirect("/")
	})

	router.Route("/api/dashboard", func(dashboardRouter chi.Router) {

		dashboardRouter.Route("/lobby", func(lobbyRouter chi.Router) {

			lobbyRouter.Post("/create", func(w http.ResponseWriter, r *http.Request) {
				sessionId, err := getSessionId(store, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				// Set User
				userEntry, err := usersKV.Get(ctx, sessionId)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				user := &components.User{}
				if err := json.Unmarshal(userEntry.Value(), user); err != nil {
					http.Error(w, fmt.Errorf("error unmarshalling game state: %w", err).Error(), http.StatusInternalServerError)
					return
				}

				id := toolbelt.NextEncodedID()
				openGames := components.OpenGames{
					Id:       id,
					HostName: user.Name,
					HostId:   sessionId,
				}
				bytes, err := json.Marshal(openGames)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to marshal mvc: %v", err), http.StatusInternalServerError)
					return
				}
				_, err = openGamesKV.Put(r.Context(), id, bytes)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to put key value: %v", err), http.StatusInternalServerError)
					return
				}

				gameState := components.GameState{
					Id:       id,
					HostId:   sessionId,
					HostName: user.Name,
					Board:    [9]string{"", "", "", "", "", "", "", "", ""},
					XIsNext:  true,
				}
				bytes, err = json.Marshal(gameState)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to marshal mvc: %v", err), http.StatusInternalServerError)
					return
				}
				_, err = gamesKV.Put(r.Context(), id, bytes)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to put key value: %v", err), http.StatusInternalServerError)
					return
				}
			})

			lobbyRouter.Delete("/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()

				// Extract the "id" parameter from the URL
				id := chi.URLParam(r, "id")
				if id == "" {
					http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
					return
				}
				log.Printf("id: %s", id)

				// Delete the specified key from the "games" bucket
				if err := gamesKV.Delete(ctx, id); err != nil {
					http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", id, err), http.StatusInternalServerError)
					return
				}

				// Delete the specified key from the "games" bucket
				if err := openGamesKV.Delete(ctx, id); err != nil {
					http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", id, err), http.StatusInternalServerError)
					return
				}
			})

			lobbyRouter.Delete("/purge", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				sse := datastar.NewSSE(w, r)

				keys, err := gamesKV.Keys(ctx)
				if err != nil {
					http.Error(w, fmt.Sprintf("Error listing keys: %v", err), http.StatusInternalServerError)
					log.Printf("Error listing keys: %v", err)
					return
				}

				for _, key := range keys {
					err = gamesKV.Delete(ctx, key)
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

				keys, err = openGamesKV.Keys(ctx)
				if err != nil {
					http.Error(w, fmt.Sprintf("Error listing keys: %v", err), http.StatusInternalServerError)
					log.Printf("Error listing keys: %v", err)
					return
				}

				for _, key := range keys {
					err = openGamesKV.Delete(ctx, key)
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

			lobbyRouter.Get("/watch", func(w http.ResponseWriter, r *http.Request) {
				sse := datastar.NewSSE(w, r)

				sessionId, err := getSessionId(store, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				ctx := r.Context()

				watcher, err := openGamesKV.WatchAll(ctx)
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

						var mvc components.GameState
						if err := json.Unmarshal(update.Value(), &mvc); err != nil {
							log.Printf("Error unmarshalling update value: %v", err)
							return
						}

						c := components.DashboardItem(&mvc, sessionId)
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

				// historicalMode := true

				// for update := range watcher.Updates() {
				// 	if update == nil {

				// 		continue
				// 	}

				// 	switch update.Operation() {
				// 	case jetstream.KeyValuePut:
				// 		var gameComponents []templ.Component
				// 		var mvc components.GameState
				// 		if err := json.Unmarshal(update.Value(), &mvc); err != nil {
				// 			log.Printf("Error unmarshalling update value: %v", err)
				// 			continue
				// 		}

				// 		c := components.DashboardItem(&mvc, sessionId)
				// 		gameComponents = append(gameComponents, c)

				// 		sse.MergeFragmentTempl(
				// 			components.DashboardList(gameComponents),
				// 			datastar.WithSelectorID("list-container"),
				// 		)

				// 	case jetstream.KeyValueDelete:

				// 	}
				// }

				// for update := range watcher.Updates() {

				// 	var mvc components.GameState
				// 	if err := json.Unmarshal(update.Value(), &mvc); err != nil {
				// 		log.Printf("Error unmarshalling update value: %v", err)
				// 		return
				// 	}
				// 	c := components.DashboardItem(&mvc, sessionId)

				// }

				// 	if update == nil {
				// 		fmt.Println("End of historical updates. Now receiving live updates...")
				// 		historicalMode = false
				// 		continue
				// 	}

				// 	log.Printf("Received update: %s", update.Value())

				// 	switch update.Operation() {
				// 	case jetstream.KeyValuePut:

				// 		var mvc components.GameState
				// 		if err := json.Unmarshal(update.Value(), &mvc); err != nil {
				// 			log.Printf("Error unmarshalling update value: %v", err)
				// 			return
				// 		}

				// 		if historicalMode { // if historical update

				// 			// if err := sse.RemoveFragments("#game-"+update.Key(),
				// 			// 	datastar.WithRemoveSettleDuration(0),
				// 			// ); err != nil {
				// 			// 	log.Printf("Error removing fragments: %v", err)
				// 			// 	return
				// 			// }

				// 			c := components.DashboardItem(&mvc, sessionId)
				// 			if err := sse.MergeFragmentTempl(c,
				// 				datastar.WithSelectorID("list-container"),
				// 				datastar.WithMergeAppend(),
				// 			); err != nil {
				// 				sse.ConsoleError(err)
				// 			}

				// 		} else { // if live update

				// 			if update.Revision() == 1 { // if new game

				// 				c := components.DashboardItem(&mvc, sessionId)
				// 				if err := sse.MergeFragmentTempl(c,
				// 					datastar.WithSelectorID("list-container"),
				// 					datastar.WithMergeAppend(),
				// 				); err != nil {
				// 					sse.ConsoleError(err)
				// 				}

				// 			} else { // if existing game

				// 				// if err := sse.RemoveFragments("#game-"+update.Key(),
				// 				// 	datastar.WithRemoveSettleDuration(0),
				// 				// ); err != nil {
				// 				// 	log.Printf("Error removing fragments: %v", err)
				// 				// 	return
				// 				// }

				// 				c := components.DashboardItem(&mvc, sessionId)
				// 				if err := sse.MergeFragmentTempl(c,
				// 					datastar.WithSelectorID("#game-"+update.Key()),
				// 				); err != nil {
				// 					sse.ConsoleError(err)
				// 				}
				// 			}

				// 		}

				// 		// // Remove outdated fragments
				// 		// if err := sse.RemoveFragments("#game-"+update.Key(),
				// 		// 	datastar.WithRemoveSettleDuration(0),
				// 		// ); err != nil {
				// 		// 	log.Printf("Error removing fragments: %v", err)
				// 		// 	return
				// 		// }

				// 		// // Parse the update into a GameState object
				// 		// var mvc components.GameState
				// 		// if err := json.Unmarshal(update.Value(), &mvc); err != nil {
				// 		// 	log.Printf("Error unmarshalling update value: %v", err)
				// 		// 	return
				// 		// }

				// 		// if update.Revision() == 1 { // Create a new game list item and merge it into the DOM
				// 		// 	log.Println("New key created:", update.Key())
				// 		// 	c := components.DashboardItem(&mvc, sessionId)
				// 		// 	if err := sse.MergeFragmentTempl(c,
				// 		// 		datastar.WithSelectorID("list-container"),
				// 		// 		datastar.WithMergeAppend(),
				// 		// 	); err != nil {
				// 		// 		sse.ConsoleError(err)
				// 		// 	}
				// 		// } else { // Game already existed.
				// 		// 	log.Println("Existing key updated:", update.Key(), "Revision:", update.Revision())
				// 		// }

				// 		// if mvc.HostId != sessionId {
				// 		// 	if mvc.ChallengerId == "" {

				// 		// 		// Create a new game list item and merge it into the DOM
				// 		// 		c := components.DashboardItem(&mvc, sessionId)
				// 		// 		if err := sse.MergeFragmentTempl(c,
				// 		// 			datastar.WithSelectorID("list-container"),
				// 		// 			datastar.WithMergeAppend(),
				// 		// 		); err != nil {
				// 		// 			sse.ConsoleError(err)
				// 		// 		}
				// 		// 	}
				// 		// } else {
				// 		// 	// Create a new game list item and merge it into the DOM
				// 		// 	c := components.DashboardItem(&mvc, sessionId)
				// 		// 	if err := sse.MergeFragmentTempl(c,
				// 		// 		datastar.WithSelectorID("list-container"),
				// 		// 		datastar.WithMergeAppend(),
				// 		// 	); err != nil {
				// 		// 		sse.ConsoleError(err)
				// 		// 	}
				// 		// }

				// 	case jetstream.KeyValueDelete:

				// 		// if historicalMode {
				// 		// 	log.Printf("Ignoring historical delete for key: %s", update.Key())
				// 		// 	continue
				// 		// }

				// 		if err := sse.RemoveFragments("#game-"+update.Key(),
				// 			datastar.WithRemoveSettleDuration(1*time.Millisecond),
				// 			datastar.WithRemoveUseViewTransitions(true),
				// 		); err != nil {
				// 			sse.ConsoleError(err)
				// 		}

				// 	}
				// }
			})

		})

	})

	return nil
}
