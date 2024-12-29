package routes

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"net/http"
// 	"time"

// 	datastar "github.com/starfederation/datastar/code/go/sdk"

// 	"github.com/delaneyj/toolbelt"
// 	"github.com/delaneyj/toolbelt/embeddednats"
// 	"github.com/go-chi/chi/v5"
// 	"github.com/gorilla/sessions"
// 	"github.com/nats-io/nats.go/jetstream"
// 	"github.com/zangster300/northstar/web/components"
// 	"github.com/zangster300/northstar/web/pages"
// )

// func setupIndexRoute(router chi.Router, store sessions.Store, ns *embeddednats.Server) error {
// 	nc, err := ns.Client()
// 	if err != nil {
// 		return fmt.Errorf("error creating nats client: %w", err)
// 	}

// 	js, err := jetstream.New(nc)
// 	if err != nil {
// 		return fmt.Errorf("error creating jetstream client: %w", err)
// 	}

// 	kv, err := js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
// 		Bucket:      "games",
// 		Description: "Datastar Tic Tac Toe Game",
// 		Compression: true,
// 		TTL:         time.Hour,
// 		MaxBytes:    16 * 1024 * 1024,
// 	})

// 	if err != nil {
// 		return fmt.Errorf("error creating key value: %w", err)
// 	}

// 	saveMVC := func(ctx context.Context, mvc *components.GameState) error {
// 		b, err := json.Marshal(mvc)
// 		if err != nil {
// 			return fmt.Errorf("failed to marshal mvc: %w", err)
// 		}
// 		if _, err := kv.Put(ctx, "games.game1", b); err != nil {
// 			return fmt.Errorf("failed to put key value: %w", err)
// 		}
// 		return nil
// 	}

// 	// resetMVC := func(mvc *components.GameState, sessionID string) {
// 	// 	mvc.Players = [2]string{"Player-" + sessionID, ""}
// 	// 	mvc.Board = [9]string{}
// 	// 	mvc.XIsNext = true
// 	// 	mvc.Winner = ""
// 	// }

// 	// createNewGame := func(ctx context.Context, sessionID string) error {
// 	// 	// Create a new game instance with initial state
// 	// 	game := &GameState{
// 	// 		PlayerCount: 1,
// 	// 		Status:      "waiting",
// 	// 		Board:       [3][3]string{},
// 	// 		XIsNext:     true,
// 	// 		Winner:      "",
// 	// 	}

// 	// 	// Generate a new game ID
// 	// 	gameId := toolbelt.NextEncodedID()

// 	// 	// Marshal the game struct to JSON
// 	// 	b, err := json.Marshal(game)
// 	// 	if err != nil {
// 	// 		return fmt.Errorf("failed to marshal game: %w", err)
// 	// 	}

// 	// 	// Store the game in the key-value store
// 	// 	if _, err := kv.Put(ctx, gameId, b); err != nil {
// 	// 		return fmt.Errorf("failed to put key-value: %w", err)
// 	// 	}

// 	// 	// Optionally log the successful creation of the game
// 	// 	log.Printf("Game created successfully with ID: %s", gameId)

// 	// 	return nil
// 	// }

// 	// // resetMVC := func(mvc *GameState, sessionID string) {
// 	// // 	mvc.Players = [2]string{"Player-" + sessionID, ""}
// 	// // 	mvc.Board = [9]string{}
// 	// // 	mvc.XIsNext = true
// 	// // 	mvc.Winner = ""
// 	// // }

// 	// getPlayer := func(ctx context.Context, sessionID string, player *Player) (*Player, error) {
// 	// 	if entry, err := kv.Get(ctx, sessionID); err != nil {
// 	// 		if err != jetstream.ErrKeyNotFound {
// 	// 			return nil, fmt.Errorf("failed to get key value: %w", err)
// 	// 		}
// 	// 		return nil, nil
// 	// 	} else {
// 	// 		if err := json.Unmarshal(entry.Value(), player); err != nil {
// 	// 			return nil, fmt.Errorf("failed to unmarshal player: %w", err)
// 	// 		}
// 	// 	}
// 	// 	return player, nil
// 	// }

// 	// getGame := func(ctx context.Context, sessionID string, gameState *GameState) (*GameState, error) {
// 	// 	if entry, err := kv.Get(ctx, sessionID); err != nil {
// 	// 		if err != jetstream.ErrKeyNotFound {
// 	// 			return nil, fmt.Errorf("failed to get key value: %w", err)
// 	// 		}
// 	// 		return nil, nil
// 	// 	} else {
// 	// 		if err := json.Unmarshal(entry.Value(), gameState); err != nil {
// 	// 			return nil, fmt.Errorf("failed to unmarshal player: %w", err)
// 	// 		}
// 	// 	}
// 	// 	return gameState, nil
// 	// }

// 	// mvcSession := func(w http.ResponseWriter, r *http.Request) (*GameState, error) {
// 	// 	ctx := r.Context()
// 	// 	sessionID, err := upsertSessionID(store, r, w)
// 	// 	if err != nil {
// 	// 		return nil, fmt.Errorf("failed to get session id: %w", err)
// 	// 	}

// 	// 	player := &Player{}
// 	// 	if _, err := getPlayer(ctx, sessionID, player); err != nil {
// 	// 		if err != nil {
// 	// 			return nil, fmt.Errorf("failed to get player: %w", err)
// 	// 		}
// 	// 	}

// 	// 	gameState := &GameState{}
// 	// 	if _, err := getGame(ctx, player.GameID, gameState); err != nil {
// 	// 		if err != nil {
// 	// 			return nil, fmt.Errorf("failed to get game: %w", err)
// 	// 		}
// 	// 	}

// 	// 	return gameState, nil
// 	// }

// 	// mvcSession := func(w http.ResponseWriter, r *http.Request) (string, *components.GameState, error) {
// 	// 	ctx := r.Context()
// 	// 	sessionID, err := upsertSessionID(store, r, w)
// 	// 	if err != nil {
// 	// 		return "", nil, fmt.Errorf("failed to get session id: %w", err)
// 	// 	}

// 	// 	mvc := &components.GameState{}
// 	// 	if entry, err := kv.Get(ctx, sessionID); err != nil {
// 	// 		if err != jetstream.ErrKeyNotFound {
// 	// 			return "", nil, fmt.Errorf("failed to get key value: %w", err)
// 	// 		}
// 	// 		resetMVC(mvc, sessionID)

// 	// 		if err := saveMVC(ctx, sessionID, mvc); err != nil {
// 	// 			return "", nil, fmt.Errorf("failed to save mvc: %w", err)
// 	// 		}
// 	// 	} else {
// 	// 		if err := json.Unmarshal(entry.Value(), mvc); err != nil {
// 	// 			return "", nil, fmt.Errorf("failed to unmarshal mvc: %w", err)
// 	// 		}
// 	// 	}
// 	// 	return sessionID, mvc, nil
// 	// }

// 	mvcSession := func(w http.ResponseWriter, r *http.Request) (*components.GameState, error) {
// 		ctx := r.Context()
// 		// sessionID, err := upsertSessionID(store, r, w)
// 		// if err != nil {
// 		// 	return "", nil, fmt.Errorf("failed to get session id: %w", err)
// 		// }

// 		mvc := &components.GameState{
// 			Id: "gamesadasd",
// 		}
// 		// if entry, err := kv.Get(ctx, "games"); err != nil {
// 		// 	if err != jetstream.ErrKeyNotFound {
// 		// 		return nil, fmt.Errorf("failed to get key value: %w", err)
// 		// 	}
// 		// resetMVC(mvc, sessionID)

// 		if err := saveMVC(ctx, mvc); err != nil {
// 			return nil, fmt.Errorf("failed to save mvc: %w", err)
// 		}
// 		// } else {
// 		// 	if err := json.Unmarshal(entry.Value(), mvc); err != nil {
// 		// 		return nil, fmt.Errorf("failed to unmarshal mvc: %w", err)
// 		// 	}
// 		// }
// 		return mvc, nil
// 	}

// 	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
// 		pages.Index("HYPERMEDIA RULES").Render(r.Context(), w)
// 	})

// 	router.Route("/api", func(apiRouter chi.Router) {
// 		apiRouter.Route("/game", func(gameRouter chi.Router) {
// 			gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {

// 				_, err := mvcSession(w, r)
// 				if err != nil {
// 					http.Error(w, err.Error(), http.StatusInternalServerError)
// 					return
// 				}

// 				// sse := datastar.NewSSE(w, r)

// 				// Watch for updates
// 				// ctx := r.Context()
// 				// watcher, err := kv.Watch(ctx, sessionID)
// 				// if err != nil {
// 				// 	http.Error(w, err.Error(), http.StatusInternalServerError)
// 				// 	return
// 				// }
// 				// defer watcher.Stop()

// 				// for {
// 				// 	select {
// 				// 	case <-ctx.Done():
// 				// 		kv.Delete(ctx, sessionID)
// 				// 		return
// 				// 	case entry := <-watcher.Updates():
// 				// 		if entry == nil {
// 				// 			continue
// 				// 		}
// 				// 		if err := json.Unmarshal(entry.Value(), mvc); err != nil {
// 				// 			http.Error(w, err.Error(), http.StatusInternalServerError)
// 				// 			return
// 				// 		}
// 				// 		c := components.GameMVCView(mvc)
// 				// 		if err := sse.MergeFragmentTempl(c); err != nil {
// 				// 			sse.ConsoleError(err)
// 				// 			return
// 				// 		}
// 				// 	}
// 				// }
// 			})

// 			gameRouter.Delete("/delete", func(w http.ResponseWriter, r *http.Request) {
// 				ctx := r.Context()
// 				// mvc := &components.GameState{}
// 				// b, err := json.Marshal(mvc)
// 				if err != nil {
// 					http.Error(w, fmt.Sprintf("failed to marshal mvc: %v", err), http.StatusInternalServerError)
// 					return
// 				}
// 				if err := kv.Delete(ctx, "games.game1"); err != nil {
// 					http.Error(w, fmt.Sprintf("failed to put key value: %v", err), http.StatusInternalServerError)
// 					return
// 				}
// 			})

// 			// gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {

// 			// 	sessionID, mvc, err := mvcSession(w, r)
// 			// 	if err != nil {
// 			// 		http.Error(w, err.Error(), http.StatusInternalServerError)
// 			// 		return
// 			// 	}

// 			// 	sse := datastar.NewSSE(w, r)

// 			// 	// Watch for updates
// 			// 	ctx := r.Context()
// 			// 	watcher, err := kv.Watch(ctx, sessionID)
// 			// 	if err != nil {
// 			// 		http.Error(w, err.Error(), http.StatusInternalServerError)
// 			// 		return
// 			// 	}
// 			// 	defer watcher.Stop()

// 			// 	for {
// 			// 		select {
// 			// 		case <-ctx.Done():
// 			// 			kv.Delete(ctx, sessionID)
// 			// 			return
// 			// 		case entry := <-watcher.Updates():
// 			// 			if entry == nil {
// 			// 				continue
// 			// 			}
// 			// 			if err := json.Unmarshal(entry.Value(), mvc); err != nil {
// 			// 				http.Error(w, err.Error(), http.StatusInternalServerError)
// 			// 				return
// 			// 			}
// 			// 			c := components.GameMVCView(mvc)
// 			// 			if err := sse.MergeFragmentTempl(c); err != nil {
// 			// 				sse.ConsoleError(err)
// 			// 				return
// 			// 			}
// 			// 		}
// 			// 	}
// 			// })

// 			// gameRouter.Put("/reset/{idx}", func(w http.ResponseWriter, r *http.Request) {
// 			// 	sessionID, mvc, err := mvcSession(w, r)
// 			// 	if err != nil {
// 			// 		http.Error(w, err.Error(), http.StatusInternalServerError)
// 			// 		return
// 			// 	}

// 			// 	resetMVC(mvc, sessionID)
// 			// 	if err := saveMVC(r.Context(), sessionID, mvc); err != nil {
// 			// 		http.Error(w, err.Error(), http.StatusInternalServerError)
// 			// 		return
// 			// 	}
// 			// })

// 			// gameRouter.Route("/{idx}", func(gameRouter chi.Router) {

// 			// routeIndex := func(w http.ResponseWriter, r *http.Request) (int, error) {
// 			// 	idx := chi.URLParam(r, "idx")
// 			// 	i, err := strconv.Atoi(idx)
// 			// 	if err != nil {
// 			// 		http.Error(w, err.Error(), http.StatusBadRequest)
// 			// 		return 0, err
// 			// 	}
// 			// 	return i, nil
// 			// }

// 			// gameRouter.Post("/toggle", func(w http.ResponseWriter, r *http.Request) {
// 			// 	sessionID, mvc, err := mvcSession(w, r)

// 			// 	sse := datastar.NewSSE(w, r)
// 			// 	if err != nil {
// 			// 		sse.ConsoleError(err)
// 			// 		return
// 			// 	}

// 			// 	i, err := routeIndex(w, r)
// 			// 	if err != nil {
// 			// 		sse.ConsoleError(err)
// 			// 		return
// 			// 	}

// 			// 	mvc.Board[i] = "X"

// 			// 	saveMVC(r.Context(), sessionID, mvc)
// 			// })
// 			// })
// 		})

// 		apiRouter.Route("/games", func(gameRouter chi.Router) {
// 			gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {

// 				// Initialize SSE
// 				sse := datastar.NewSSE(w, r)

// 				// Watch for updates
// 				ctx := r.Context()
// 				watcher, err := kv.Watch(ctx, "games.*")
// 				if err != nil {
// 					http.Error(w, err.Error(), http.StatusInternalServerError)
// 					return
// 				}
// 				defer watcher.Stop()

// 				mvc := &components.GameState{}
// 				for {
// 					select {
// 					case <-ctx.Done():
// 						return
// 					case entry := <-watcher.Updates():
// 						if entry == nil {
// 							continue
// 						}
// 						if err := json.Unmarshal(entry.Value(), mvc); err != nil {
// 							http.Error(w, err.Error(), http.StatusInternalServerError)
// 							return
// 						}
// 						c := components.CurrentGamesMVCView(mvc)
// 						if err := sse.MergeFragmentTempl(c); err != nil {
// 							sse.ConsoleError(err)
// 							return
// 						}
// 					}
// 				}
// 			})
// 		})

// 	})

// 	return nil
// }

// func MustJSONMarshal(v any) string {
// 	b, err := json.MarshalIndent(v, "", " ")
// 	if err != nil {
// 		panic(err)
// 	}
// 	return string(b)
// }

// func upsertSessionID(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {

// 	sess, err := store.Get(r, "connections")
// 	if err != nil {
// 		return "", fmt.Errorf("failed to get session: %w", err)
// 	}
// 	id, ok := sess.Values["id"].(string)
// 	if !ok {
// 		id = toolbelt.NextEncodedID()
// 		sess.Values["id"] = id
// 		if err := sess.Save(r, w); err != nil {
// 			return "", fmt.Errorf("failed to save session: %w", err)
// 		}
// 	}
// 	return id, nil
// }
