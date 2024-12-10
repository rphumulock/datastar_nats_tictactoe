package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	datastar "github.com/starfederation/datastar/code/go/sdk"

	"github.com/delaneyj/toolbelt"
	"github.com/delaneyj/toolbelt/embeddednats"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/zangster300/northstar/web/components"
	"github.com/zangster300/northstar/web/pages"
)

// Global connection counter
var (
	connectionCounter = make(map[string]int)
	mu                sync.Mutex // To ensure thread-safe access
)

func setupIndexRoute(router chi.Router, store sessions.Store, ns *embeddednats.Server) error {
	nc, err := ns.Client()
	if err != nil {
		return fmt.Errorf("error creating nats client: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("error creating jetstream client: %w", err)
	}

	kv, err := js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "todos",
		Description: "Datastar Todos",
		Compression: true,
		TTL:         time.Hour,
		MaxBytes:    16 * 1024 * 1024,
	})

	if err != nil {
		return fmt.Errorf("error creating key value: %w", err)
	}

	saveMVC := func(ctx context.Context, sessionID string, mvc *components.GameState) error {
		b, err := json.Marshal(mvc)
		if err != nil {
			return fmt.Errorf("failed to marshal mvc: %w", err)
		}
		if _, err := kv.Put(ctx, sessionID, b); err != nil {
			return fmt.Errorf("failed to put key value: %w", err)
		}
		return nil
	}

	resetMVC := func(mvc *components.GameState) {
		mvc.Board = [9]string{}
		mvc.XIsNext = true
		mvc.Winner = ""
	}

	mvcSession := func(w http.ResponseWriter, r *http.Request) (string, *components.GameState, error) {
		ctx := r.Context()
		sessionID, err := upsertSessionID(store, r, w)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get session id: %w", err)
		}

		mvc := &components.GameState{}
		if entry, err := kv.Get(ctx, sessionID); err != nil {
			if err != jetstream.ErrKeyNotFound {
				return "", nil, fmt.Errorf("failed to get key value: %w", err)
			}
			resetMVC(mvc)

			if err := saveMVC(ctx, sessionID, mvc); err != nil {
				return "", nil, fmt.Errorf("failed to save mvc: %w", err)
			}
		} else {
			if err := json.Unmarshal(entry.Value(), mvc); err != nil {
				return "", nil, fmt.Errorf("failed to unmarshal mvc: %w", err)
			}
		}
		return sessionID, mvc, nil
	}

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		pages.Index("HYPERMEDIA RULES").Render(r.Context(), w)
	})

	router.Route("/api", func(apiRouter chi.Router) {
		apiRouter.Route("/game", func(gameRouter chi.Router) {
			gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {

				sessionID, mvc, err := mvcSession(w, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				// Increment connection count for the session
				mu.Lock()
				connectionCounter[sessionID]++
				currentConnections := connectionCounter[sessionID]
				mu.Unlock()

				// Block if more than 2 connections for this session
				if currentConnections > 2 {
					mu.Lock()
					connectionCounter[sessionID]--
					mu.Unlock()
					http.Error(w, "Only 2 tabs are allowed for this session", http.StatusForbidden)
					return
				}

				// Setup SSE
				sse := datastar.NewSSE(w, r)

				// Watch for updates
				ctx := r.Context()
				watcher, err := kv.Watch(ctx, sessionID)
				if err != nil {
					mu.Lock()
					connectionCounter[sessionID]--
					mu.Unlock()
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				defer func() {
					// Stop watcher and decrement connection count
					watcher.Stop()
					mu.Lock()
					connectionCounter[sessionID]--
					mu.Unlock()
				}()

				// Start watching for updates
				for {
					select {
					case <-ctx.Done():
						return
					case entry := <-watcher.Updates():
						if entry == nil {
							continue
						}
						if err := json.Unmarshal(entry.Value(), mvc); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						c := components.GameMVCView(mvc)
						if err := sse.MergeFragmentTempl(c); err != nil {
							sse.ConsoleError(err)
							return
						}
					}
				}
			})

			gameRouter.Post("/reset", func(w http.ResponseWriter, r *http.Request) {
				sessionID, mvc, err := mvcSession(w, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				resetMVC(mvc)
				if err := saveMVC(r.Context(), sessionID, mvc); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			})

			// todosRouter.Put("/cancel", func(w http.ResponseWriter, r *http.Request) {

			// 	sessionID, mvc, err := mvcSession(w, r)
			// 	sse := datastar.NewSSE(w, r)
			// 	if err != nil {
			// 		sse.ConsoleError(err)
			// 		return
			// 	}

			// 	mvc.EditingIdx = -1
			// 	if err := saveMVC(r.Context(), sessionID, mvc); err != nil {
			// 		sse.ConsoleError(err)
			// 		return
			// 	}
			// })

			// todosRouter.Put("/mode/{mode}", func(w http.ResponseWriter, r *http.Request) {

			// 	sessionID, mvc, err := mvcSession(w, r)
			// 	if err != nil {
			// 		http.Error(w, err.Error(), http.StatusInternalServerError)
			// 		return
			// 	}

			// 	modeStr := chi.URLParam(r, "mode")
			// 	modeRaw, err := strconv.Atoi(modeStr)
			// 	if err != nil {
			// 		http.Error(w, err.Error(), http.StatusBadRequest)
			// 		return
			// 	}

			// 	mode := components.TodoViewMode(modeRaw)
			// 	if mode < components.TodoViewModeAll || mode > components.TodoViewModeCompleted {
			// 		http.Error(w, "invalid mode", http.StatusBadRequest)
			// 		return
			// 	}

			// 	mvc.Mode = mode
			// 	if err := saveMVC(r.Context(), sessionID, mvc); err != nil {
			// 		http.Error(w, err.Error(), http.StatusInternalServerError)
			// 		return
			// 	}
			// })

			gameRouter.Route("/{idx}", func(gameRouter chi.Router) {

				routeIndex := func(w http.ResponseWriter, r *http.Request) (int, error) {
					idx := chi.URLParam(r, "idx")
					i, err := strconv.Atoi(idx)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return 0, err
					}
					return i, nil
				}

				gameRouter.Post("/toggle", func(w http.ResponseWriter, r *http.Request) {
					sessionID, mvc, err := mvcSession(w, r)

					sse := datastar.NewSSE(w, r)
					if err != nil {
						sse.ConsoleError(err)
						return
					}

					i, err := routeIndex(w, r)
					if err != nil {
						sse.ConsoleError(err)
						return
					}

					if mvc.XIsNext {
						mvc.Board[i] = "X"
						mvc.XIsNext = false
					} else {
						mvc.Board[i] = "O"
						mvc.XIsNext = true
					}

					saveMVC(r.Context(), sessionID, mvc)
				})

				// 	todoRouter.Route("/edit", func(editRouter chi.Router) {
				// 		editRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				// 			sessionID, mvc, err := mvcSession(w, r)
				// 			if err != nil {
				// 				http.Error(w, err.Error(), http.StatusInternalServerError)
				// 				return
				// 			}

				// 			i, err := routeIndex(w, r)
				// 			if err != nil {
				// 				return
				// 			}

				// 			mvc.EditingIdx = i
				// 			saveMVC(r.Context(), sessionID, mvc)
				// 		})

				// 		editRouter.Put("/", func(w http.ResponseWriter, r *http.Request) {
				// 			type Store struct {
				// 				Input string `json:"input"`
				// 			}
				// 			store := &Store{}

				// 			if err := datastar.ReadSignals(r, store); err != nil {
				// 				http.Error(w, err.Error(), http.StatusBadRequest)
				// 				return
				// 			}

				// 			if store.Input == "" {
				// 				return
				// 			}

				// 			sessionID, mvc, err := mvcSession(w, r)
				// 			if err != nil {
				// 				http.Error(w, err.Error(), http.StatusInternalServerError)
				// 				return
				// 			}

				// 			i, err := routeIndex(w, r)
				// 			if err != nil {
				// 				return
				// 			}

				// 			if i >= 0 {
				// 				mvc.Todos[i].Text = store.Input
				// 			} else {
				// 				mvc.Todos = append(mvc.Todos, &components.Todo{
				// 					Text:      store.Input,
				// 					Completed: false,
				// 				})
				// 			}
				// 			mvc.EditingIdx = -1

				// 			saveMVC(r.Context(), sessionID, mvc)

				// 		})
				// 	})

				// 	todoRouter.Delete("/", func(w http.ResponseWriter, r *http.Request) {
				// 		i, err := routeIndex(w, r)
				// 		if err != nil {
				// 			return
				// 		}

				// 		sessionID, mvc, err := mvcSession(w, r)
				// 		if err != nil {
				// 			http.Error(w, err.Error(), http.StatusInternalServerError)
				// 			return
				// 		}

				// 		if i >= 0 {
				// 			mvc.Todos = append(mvc.Todos[:i], mvc.Todos[i+1:]...)
				// 		} else {
				// 			mvc.Todos = lo.Filter(mvc.Todos, func(todo *components.Todo, i int) bool {
				// 				return !todo.Completed
				// 			})
				// 		}
				// 		saveMVC(r.Context(), sessionID, mvc)
				// 	})
			})
		})
	})

	return nil
}

func MustJSONMarshal(v any) string {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func upsertSessionID(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {

	sess, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	id, ok := sess.Values["id"].(string)
	if !ok {
		id = toolbelt.NextEncodedID()
		sess.Values["id"] = id
		if err := sess.Save(r, w); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
	}
	return id, nil
}
