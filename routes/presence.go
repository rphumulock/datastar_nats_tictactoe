package routes

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar-nats-tictactoe/web/components"
)

// A session is "present" while it holds an open SSE stream - the dashboard list
// or a game board. Each stream heartbeats its key, and the presence bucket's TTL
// expires it shortly after the browser goes away. That makes an absent host
// detectable, which is what lets stale lobbies be hidden and then reaped instead
// of sitting on the dashboard until the hour-long lobby TTL catches them.
const (
	// presenceTTL outlives a couple of missed heartbeats, so a slow tick or a
	// page transition doesn't read as a disconnect.
	presenceTTL       = 90 * time.Second
	presenceHeartbeat = 25 * time.Second

	// reapInterval is how often abandoned lobbies are swept.
	reapInterval = time.Minute
	// reapGrace keeps a freshly written lobby out of the reaper's reach, so a
	// lobby is never purged in the window before its host's first heartbeat.
	reapGrace = 2 * time.Minute
)

// touchPresence marks a session as connected. Failures are logged, not returned:
// a missed heartbeat costs at most one reap cycle and should never take down the
// stream that called it.
func touchPresence(ctx context.Context, presenceKV jetstream.KeyValue, sessionId string) {
	if sessionId == "" {
		return
	}
	if _, err := presenceKV.PutString(ctx, sessionId, time.Now().UTC().Format(time.RFC3339)); err != nil {
		if ctx.Err() == nil {
			log.Printf("presence: failed to touch %s: %v", sessionId, err)
		}
	}
}

// keepPresence marks the session connected now and keeps doing so until ctx is
// done. Call it from a long-lived SSE handler; it returns immediately.
func keepPresence(ctx context.Context, presenceKV jetstream.KeyValue, sessionId string) {
	if sessionId == "" {
		return
	}

	touchPresence(ctx, presenceKV, sessionId)

	go func() {
		ticker := time.NewTicker(presenceHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				touchPresence(ctx, presenceKV, sessionId)
			}
		}
	}()
}

// presentSessions reads the whole presence set in one call, so a render or a
// reap costs one round trip rather than one per lobby.
func presentSessions(ctx context.Context, presenceKV jetstream.KeyValue) (map[string]struct{}, error) {
	keys, err := presenceKV.Keys(ctx)
	if err != nil && !errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, err
	}

	present := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		present[key] = struct{}{}
	}
	return present, nil
}

// reapAbandonedGames purges lobbies whose host is gone and reopens lobbies whose
// challenger is gone, and reports how many of each it touched.
func reapAbandonedGames(ctx context.Context, gameLobbiesKV, gameBoardsKV, presenceKV jetstream.KeyValue) (purged, reopened int) {
	present, err := presentSessions(ctx, presenceKV)
	if err != nil {
		log.Printf("reaper: failed to read presence: %v", err)
		return 0, 0
	}

	keys, err := gameLobbiesKV.Keys(ctx)
	if err != nil {
		if !errors.Is(err, jetstream.ErrNoKeysFound) {
			log.Printf("reaper: failed to list game lobbies: %v", err)
		}
		return 0, 0
	}

	for _, key := range keys {
		lobby, entry, err := GetObject[components.GameLobby](ctx, gameLobbiesKV, key)
		if err != nil {
			continue // raced with a delete; the next pass will see the truth
		}

		// Created() is the timestamp of the current revision, so a lobby someone
		// just joined starts its grace period over. That is the intent: recent
		// activity should buy a lobby more time, not less.
		if time.Since(entry.Created()) < reapGrace {
			continue
		}

		if _, hostHere := present[lobby.HostId]; !hostHere {
			if err := gameLobbiesKV.Purge(ctx, key); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
				log.Printf("reaper: failed to purge lobby %s: %v", key, err)
				continue
			}
			if err := gameBoardsKV.Purge(ctx, key); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
				log.Printf("reaper: failed to purge board %s: %v", key, err)
			}
			purged++
			continue
		}

		if lobby.ChallengerId == "" {
			continue
		}
		if _, challengerHere := present[lobby.ChallengerId]; !challengerHere {
			// The host is still here, so hand them an open lobby rather than
			// tearing down a game they are sitting in.
			if _, err := UpdateObject(ctx, gameLobbiesKV, key, func(lobby *components.GameLobby) error {
				lobby.ChallengerId = ""
				return nil
			}); err != nil {
				log.Printf("reaper: failed to clear challenger on lobby %s: %v", key, err)
				continue
			}
			reopened++
		}
	}

	return purged, reopened
}

// startReaper sweeps abandoned games until ctx is cancelled.
func startReaper(ctx context.Context, gameLobbiesKV, gameBoardsKV, presenceKV jetstream.KeyValue) {
	go func() {
		ticker := time.NewTicker(reapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				purged, reopened := reapAbandonedGames(ctx, gameLobbiesKV, gameBoardsKV, presenceKV)
				if purged > 0 || reopened > 0 {
					log.Printf("reaper: purged %d abandoned game(s), reopened %d lobby(s)", purged, reopened)
				}
			}
		}
	}()
}
