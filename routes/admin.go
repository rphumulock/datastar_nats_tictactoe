// Package routes
package routes

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"

	datastar "github.com/starfederation/datastar-go/datastar"
)

// adminSessionName is deliberately a different cookie from the player session,
// so unlocking the panel never doubles as being logged in as a player.
const adminSessionName = "admin"

// adminSessionMaxAge keeps an unlocked panel from staying unlocked all day.
const adminSessionMaxAge = int(time.Hour / time.Second)

func setupAdminRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	gameLobbiesKV, err := js.KeyValue(ctx, "gameLobbies")
	if err != nil {
		return fmt.Errorf("failed to get game lobbies key value: %w", err)
	}

	gameBoardsKV, err := js.KeyValue(ctx, "gameBoards")
	if err != nil {
		return fmt.Errorf("failed to get game boards key value: %w", err)
	}

	usersKV, err := js.KeyValue(ctx, "users")
	if err != nil {
		return fmt.Errorf("failed to get users key value: %w", err)
	}

	presenceKV, err := js.KeyValue(ctx, "presence")
	if err != nil {
		return fmt.Errorf("failed to get presence key value: %w", err)
	}

	// ADMIN_TOKEN gates the panel. Without one we mint a throwaway token per run
	// and log it, so a local dev server is usable while a deployed one is not
	// left open to whoever finds /admin.
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return fmt.Errorf("failed to generate admin token: %w", err)
		}
		adminToken = hex.EncodeToString(buf)
		log.Printf("ADMIN_TOKEN not set - generated admin token for this run: %s", adminToken)
	}

	isAdmin := func(r *http.Request) bool {
		session, err := store.Get(r, adminSessionName)
		if err != nil {
			return false
		}
		authed, _ := session.Values["authed"].(bool)
		return authed
	}

	// keys returns an empty slice rather than an error when a bucket is empty,
	// which is the normal state here and not worth special-casing at each call.
	keys := func(ctx context.Context, kv jetstream.KeyValue) ([]string, error) {
		ks, err := kv.Keys(ctx)
		if err != nil {
			if errors.Is(err, jetstream.ErrNoKeysFound) {
				return nil, nil
			}
			return nil, err
		}
		return ks, nil
	}

	// deleteGame removes a lobby and its board. Purge (not Delete) so the keys
	// leave no tombstone behind, matching the dashboard's own delete.
	deleteGame := func(ctx context.Context, id string) {
		if err := gameLobbiesKV.Purge(
			ctx,
			id,
		); err != nil &&
			!errors.Is(err, jetstream.ErrKeyNotFound) {
			log.Printf("admin: failed to purge lobby %s: %v", id, err)
		}
		if err := gameBoardsKV.Purge(
			ctx,
			id,
		); err != nil &&
			!errors.Is(err, jetstream.ErrKeyNotFound) {
			log.Printf("admin: failed to purge board %s: %v", id, err)
		}
	}

	buildSnapshot := func(ctx context.Context) (components.AdminSnapshot, error) {
		snapshot := components.AdminSnapshot{}

		// Presence is the same liveness signal the dashboard filters on, so this
		// table shows exactly which rows a player would and would not see.
		present, err := presentSessions(ctx, presenceKV)
		if err != nil {
			return snapshot, fmt.Errorf("failed to read presence: %w", err)
		}

		userKeys, err := keys(ctx, usersKV)
		if err != nil {
			return snapshot, fmt.Errorf("failed to list users: %w", err)
		}

		users := make(map[string]*components.User, len(userKeys))
		for _, key := range userKeys {
			user, entry, err := GetObject[components.User](ctx, usersKV, key)
			if err != nil {
				continue // raced with an expiry; it just won't be in this snapshot
			}
			users[key] = user
			_, online := present[key]
			snapshot.Users = append(snapshot.Users, components.AdminUser{
				SessionId: key,
				Name:      user.Name,
				Online:    online,
				Created:   entry.Created(),
				Age:       components.HumanAge(entry.Created()),
			})
		}

		lobbyKeys, err := keys(ctx, gameLobbiesKV)
		if err != nil {
			return snapshot, fmt.Errorf("failed to list game lobbies: %w", err)
		}

		hosted := make(map[string]int, len(users))
		for _, key := range lobbyKeys {
			lobby, entry, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, key)
			if err != nil {
				continue
			}

			hostName := lobby.HostName
			if host, ok := users[lobby.HostId]; ok {
				hostName = host.Name
			}
			_, hostOnline := present[lobby.HostId]

			challengerName := ""
			if challenger, ok := users[lobby.ChallengerId]; ok {
				challengerName = challenger.Name
			}
			_, challengerOnline := present[lobby.ChallengerId]

			game := components.AdminGame{
				Id:               lobby.Id,
				Name:             lobby.Name,
				HostId:           lobby.HostId,
				HostName:         hostName,
				HostOnline:       hostOnline,
				ChallengerId:     lobby.ChallengerId,
				ChallengerName:   challengerName,
				ChallengerOnline: challengerOnline,
				Created:          entry.Created(),
				Age:              components.HumanAge(entry.Created()),
				// The host is not connected, so this lobby is already hidden from
				// the dashboard and the reaper will purge it shortly.
				Orphaned: !hostOnline,
			}

			if board, _, err := GetObject[components.GameState](
				ctx,
				gameBoardsKV,
				key,
			); err == nil {
				game.Winner = board.Winner
				for _, cell := range board.Board {
					if cell != "" {
						game.Moves++
					}
				}
			}

			hosted[lobby.HostId]++
			if game.Orphaned {
				snapshot.Orphaned++
			}
			if game.Winner != "" {
				snapshot.Finished++
			}
			snapshot.Games = append(snapshot.Games, game)
		}

		for i := range snapshot.Users {
			snapshot.Users[i].Games = hosted[snapshot.Users[i].SessionId]
		}

		// Keys() has no defined order, so sort oldest first: the rows most likely
		// to need clearing sit at the top and stay put across re-renders.
		sort.Slice(snapshot.Games, func(i, j int) bool {
			return snapshot.Games[i].Created.Before(snapshot.Games[j].Created)
		})
		sort.Slice(snapshot.Users, func(i, j int) bool {
			return snapshot.Users[i].Created.Before(snapshot.Users[j].Created)
		})

		return snapshot, nil
	}

	// purge deletes every game the predicate accepts and reports the count.
	purge := func(ctx context.Context, match func(components.AdminGame) bool) (int, error) {
		snapshot, err := buildSnapshot(ctx)
		if err != nil {
			return 0, err
		}
		removed := 0
		for _, game := range snapshot.Games {
			if match(game) {
				deleteGame(ctx, game.Id)
				removed++
			}
		}
		return removed, nil
	}

	handleAdminPage := func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			if err := pages.AdminLogin().Render(r.Context(), w); err != nil {
				log.Printf("admin: login render failed: %v", err)
			}
			return
		}
		if err := pages.Admin().Render(r.Context(), w); err != nil {
			log.Printf("admin: render failed: %v", err)
		}
	}

	router.Get("/admin", handleAdminPage)

	// API

	handleLogin := func(w http.ResponseWriter, r *http.Request) {
		signals := struct {
			Token string `json:"token"`
		}{}
		if err := datastar.ReadSignals(r, &signals); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if subtle.ConstantTimeCompare([]byte(signals.Token), []byte(adminToken)) != 1 {
			if err := datastar.NewSSE(w, r).PatchElementTempl(components.AdminLogin(true)); err != nil {
				log.Printf("admin: login patch failed: %v", err)
			}
			return
		}

		// The session cookie has to be written before the SSE generator sends
		// its headers, so save first and only then start the stream.
		session, err := store.Get(r, adminSessionName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		session.Values["authed"] = true
		session.Options.MaxAge = adminSessionMaxAge
		if err := session.Save(r, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		sseRedirect(datastar.NewSSE(w, r), "/admin")
	}

	handleLogout := func(w http.ResponseWriter, r *http.Request) {
		session, err := store.Get(r, adminSessionName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		delete(session.Values, "authed")
		session.Options.MaxAge = -1
		if err := session.Save(r, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sseRedirect(datastar.NewSSE(w, r), "/")
	}

	renderAdmin := func(ctx context.Context, sse *datastar.ServerSentEventGenerator) {
		snapshot, err := buildSnapshot(ctx)
		if err != nil {
			log.Printf("admin: failed to build snapshot: %v", err)
			return
		}
		// Reporting a broken stream back down that same stream would fail too.
		if err := sse.PatchElementTempl(components.AdminContent(snapshot)); err != nil {
			log.Printf("admin: patch failed: %v", err)
		}
	}

	handleUpdates := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sse := datastar.NewSSE(w, r)

		changed := make(chan struct{}, 1)
		notify := func() {
			select {
			case changed <- struct{}{}:
			default: // a render is already pending; it will pick this up too
			}
		}

		// All three buckets feed the same table, so fan them into one signal
		// rather than rendering per bucket.
		for _, kv := range []jetstream.KeyValue{gameLobbiesKV, gameBoardsKV, usersKV} {
			watcher, err := kv.WatchAll(ctx)
			if err != nil {
				http.Error(
					w,
					fmt.Sprintf("failed to start watcher: %v", err),
					http.StatusInternalServerError,
				)
				return
			}
			go func() {
				defer func() {
					if err := watcher.Stop(); err != nil {
						log.Printf("admin: failed to stop watcher: %v", err)
					}
				}()
				live := false
				for {
					select {
					case <-ctx.Done():
						return
					case entry, ok := <-watcher.Updates():
						if !ok {
							return
						}
						// A nil entry marks the end of the historical replay; the
						// initial render below already covers that state.
						if entry == nil {
							live = true
							continue
						}
						if live {
							notify()
						}
					}
				}
			}()
		}

		// The buckets expire entries on their own TTL, and an expiry produces no
		// watcher event, so poll as well to keep ages and counts honest.
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		renderAdmin(ctx, sse)

		for {
			select {
			case <-ctx.Done():
				return
			case <-changed:
				renderAdmin(ctx, sse)
			case <-ticker.C:
				renderAdmin(ctx, sse)
			}
		}
	}

	handleDeleteGame := func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
			return
		}
		deleteGame(r.Context(), id)
	}

	// handleDeleteUser removes a session and everything anchored to it: the games
	// it hosts go away, and lobbies it had joined reopen for someone else.
	handleDeleteUser := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, "missing 'id' parameter", http.StatusBadRequest)
			return
		}

		lobbyKeys, err := keys(ctx, gameLobbiesKV)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("failed to list game lobbies: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		for _, key := range lobbyKeys {
			lobby, _, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, key)
			if err != nil {
				continue
			}
			switch id {
			case lobby.HostId:
				deleteGame(ctx, key)
			case lobby.ChallengerId:
				if _, err := UpdateObject(
					ctx,
					gameLobbiesKV,
					key,
					func(lobby *components.GameLobby) error {
						lobby.ChallengerId = ""
						return nil
					},
				); err != nil {
					log.Printf("admin: failed to clear challenger on lobby %s: %v", key, err)
				}
			}
		}

		if err := usersKV.Purge(ctx, id); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
			http.Error(
				w,
				fmt.Sprintf("failed to delete user '%s': %v", id, err),
				http.StatusInternalServerError,
			)
			return
		}
	}

	handlePurgeOrphaned := func(w http.ResponseWriter, r *http.Request) {
		removed, err := purge(r.Context(), func(game components.AdminGame) bool {
			return game.Orphaned
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("admin: purged %d orphaned game(s)", removed)
	}

	handlePurgeFinished := func(w http.ResponseWriter, r *http.Request) {
		removed, err := purge(r.Context(), func(game components.AdminGame) bool {
			return game.Winner != ""
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("admin: purged %d finished game(s)", removed)
	}

	handlePurgeOlder := func(w http.ResponseWriter, r *http.Request) {
		signals := struct {
			OlderThanMinutes float64 `json:"olderThanMinutes"`
		}{}
		if err := datastar.ReadSignals(r, &signals); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if signals.OlderThanMinutes < 0 {
			http.Error(w, "olderThanMinutes must not be negative", http.StatusBadRequest)
			return
		}

		cutoff := time.Now().Add(-time.Duration(signals.OlderThanMinutes * float64(time.Minute)))
		removed, err := purge(r.Context(), func(game components.AdminGame) bool {
			return game.Created.Before(cutoff)
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf(
			"admin: purged %d game(s) older than %v minutes",
			removed,
			signals.OlderThanMinutes,
		)
	}

	handlePurgeGames := func(w http.ResponseWriter, r *http.Request) {
		removed, err := purge(r.Context(), func(components.AdminGame) bool { return true })
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("admin: purged all %d game(s)", removed)
	}

	// handlePurgeUsers drops every session record. Games go with them, since a
	// lobby whose host no longer exists is exactly what this panel is for.
	handlePurgeUsers := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if _, err := purge(ctx, func(components.AdminGame) bool { return true }); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		userKeys, err := keys(ctx, usersKV)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("failed to list users: %v", err),
				http.StatusInternalServerError,
			)
			return
		}
		for _, key := range userKeys {
			if err := usersKV.Purge(
				ctx,
				key,
			); err != nil &&
				!errors.Is(err, jetstream.ErrKeyNotFound) {
				log.Printf("admin: failed to purge user %s: %v", key, err)
			}
		}
		log.Printf("admin: purged %d user(s)", len(userKeys))
	}

	requireAdmin := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAdmin(r) {
				http.Error(w, "admin session required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	router.Route("/api/admin", func(adminRouter chi.Router) {
		adminRouter.Post("/login", handleLogin)

		adminRouter.Group(func(authed chi.Router) {
			authed.Use(requireAdmin)

			authed.Post("/logout", handleLogout)

			authed.Get("/updates", handleUpdates)

			authed.Delete("/games/{id}", handleDeleteGame)

			authed.Delete("/users/{id}", handleDeleteUser)

			authed.Route("/purge", func(purgeRouter chi.Router) {
				purgeRouter.Post("/orphaned", handlePurgeOrphaned)
				purgeRouter.Post("/finished", handlePurgeFinished)
				purgeRouter.Post("/older", handlePurgeOlder)
				purgeRouter.Post("/games", handlePurgeGames)
				purgeRouter.Post("/users", handlePurgeUsers)
			})
		})
	})

	return nil
}
