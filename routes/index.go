package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

func setupIndexRoute(router chi.Router, store sessions.Store, ns *embeddednats.Server) error {
	// Create NATS client
	nc, err := ns.Client()
	if err != nil {
		return fmt.Errorf("error creating nats client: %w", err)
	}

	// Access JetStream
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("error creating jetstream client: %w", err)
	}

	// Create or update the "games" KV bucket
	kv, err := js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "games",
		Description: "Datastar Tic Tac Toe Game",
		Compression: true,
		TTL:         time.Hour,
		MaxBytes:    16 * 1024 * 1024,
	})
	if err != nil {
		return fmt.Errorf("error creating key value: %w", err)
	}

	// Save MVC state to the "game1" key in the "games" bucket
	saveMVC := func(ctx context.Context, mvc *components.GameState) error {
		b, err := json.Marshal(mvc)
		if err != nil {
			return fmt.Errorf("failed to marshal mvc: %w", err)
		}
		if _, err := kv.Put(ctx, mvc.Id, b); err != nil {
			return fmt.Errorf("failed to put key value: %w", err)
		}
		return nil
	}

	// Handle session and save the default MVC state
	mvcSession := func(w http.ResponseWriter, r *http.Request) (*components.GameState, error) {
		ctx := r.Context()

		mvc := &components.GameState{
			Id: toolbelt.NextEncodedID(),
		}
		if err := saveMVC(ctx, mvc); err != nil {
			return nil, fmt.Errorf("failed to save mvc: %w", err)
		}
		return mvc, nil
	}

	// Setup routes
	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		pages.Index("HYPERMEDIA RULES").Render(r.Context(), w)
	})

	router.Route("/api", func(apiRouter chi.Router) {
		apiRouter.Route("/game", func(gameRouter chi.Router) {
			// Default game handler
			gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				_, err := mvcSession(w, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			})

			gameRouter.Delete("/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()

				// Extract the "id" parameter from the URL
				id := chi.URLParam(r, "id")
				if id == "" {
					http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
					return
				}
				log.Printf("id: %s", id)

				// Delete the specified key from the "games" bucket
				if err := kv.Delete(ctx, id); err != nil {
					http.Error(w, fmt.Sprintf("failed to delete key '%s': %v", id, err), http.StatusInternalServerError)
					return
				}
			})
		})

		// Watch for updates to the "games" bucket
		apiRouter.Route("/games", func(gameRouter chi.Router) {
			gameRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				sse := datastar.NewSSE(w, r)

				log.Printf("Live update: Key=%s, Value=%s", "s", "s")

				ctx := r.Context()

				watcher, err := kv.WatchAll(ctx, jetstream.UpdatesOnly())
				if err != nil {
					http.Error(w, fmt.Sprintf("Failed to start watcher: %v", err), http.StatusInternalServerError)
					return
				}
				defer watcher.Stop()

				for {
					select {
					case <-ctx.Done():
						return
					case entry := <-watcher.Updates():
						if entry == nil {
							continue
						}

						// Handle live updates
						switch entry.Operation() {
						case jetstream.KeyValuePut:
							log.Printf("Live update: Key=%s, Value=%s", entry.Key(), string(entry.Value()))
							mvc := &components.GameState{}
							if err := json.Unmarshal(entry.Value(), mvc); err != nil {
								http.Error(w, err.Error(), http.StatusInternalServerError)
								return
							}
							c := components.AddGame(mvc)
							if err := sse.MergeFragmentTempl(c,
								datastar.WithSelectorID("games-list-container"),
								datastar.WithMergeAppend(),
							); err != nil {
								sse.ConsoleError(err)
								return
							}

						case jetstream.KeyValueDelete:
							log.Printf("Live deletion: Key=%s", entry.Key())
							id := fmt.Sprintf("#game-%s", entry.Key())
							if err := sse.RemoveFragments(id); err != nil {
								log.Printf("error")
								sse.ConsoleError(err)
								return
							}
							continue
						}
					}
				}

			})
		})
	})

	return nil
}

// Helper function to marshal data as JSON
func MustJSONMarshal(v any) string {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Handle session IDs
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
