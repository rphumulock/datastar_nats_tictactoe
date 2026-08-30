package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/delaneyj/toolbelt"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
)

func createSessionId(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {
	session, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	id := toolbelt.NextEncodedID()
	session.Values["id"] = id
	session.Options.MaxAge = 45 * 60
	if err := session.Save(r, w); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return id, nil
}

func getSessionId(store sessions.Store, r *http.Request) (string, error) {
	session, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	id, ok := session.Values["id"].(string)
	if !ok || id == "" {
		return "", nil
	}
	return id, nil
}

func deleteSessionId(store sessions.Store, w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, "connections")
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get session: %v", err), http.StatusInternalServerError)
		return
	}
	delete(session.Values, "id")
	if err := session.Save(r, w); err != nil {
		http.Error(w, fmt.Sprintf("failed to save session: %v", err), http.StatusInternalServerError)
		return
	}
}

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

func PutData(ctx context.Context, kv jetstream.KeyValue, id string, data interface{}) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = kv.Put(ctx, id, bytes)
	if err != nil {
		return fmt.Errorf("failed to put key-value: %w", err)
	}

	return nil
}

func UpdateData(ctx context.Context, kv jetstream.KeyValue, id string, data interface{}, entry jetstream.KeyValueEntry) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = kv.Update(ctx, id, bytes, entry.Revision())
	if err != nil {
		return fmt.Errorf("failed to update key-value: %w", err)
	}

	return nil
}

// UpdateObject re-reads key, applies mutate, and writes the result back with an
// optimistic revision check. When another writer wins the race it retries against
// their result, so mutate must be safe to run more than once and should do its
// validation inside the callback — the state it sees can change between attempts.
func UpdateObject[T any](ctx context.Context, kv jetstream.KeyValue, key string, mutate func(*T) error) (*T, error) {
	const maxAttempts = 5

	for range maxAttempts {
		obj, entry, err := GetObject[T](ctx, kv, key)
		if err != nil {
			return nil, err
		}

		if err := mutate(obj); err != nil {
			return nil, err
		}

		bytes, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal JSON: %w", err)
		}

		if _, err := kv.Update(ctx, key, bytes, entry.Revision()); err != nil {
			if errors.Is(err, jetstream.ErrKeyRevisionMismatch) {
				continue // someone else wrote first; rebuild on their result
			}
			return nil, fmt.Errorf("failed to update key %s: %w", key, err)
		}

		return obj, nil
	}

	return nil, fmt.Errorf("failed to update key %s after %d attempts: too much contention", key, maxAttempts)
}
