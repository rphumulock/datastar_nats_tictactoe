package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/delaneyj/toolbelt"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
)

func createSessionId(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {
	sess, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	id := toolbelt.NextEncodedID()
	sess.Values["id"] = id
	if err := sess.Save(r, w); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return id, nil
}

func getSessionId(store sessions.Store, r *http.Request) (string, error) {
	sess, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	id, ok := sess.Values["id"].(string)
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
