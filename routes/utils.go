package routes

import (
	"fmt"
	"net/http"

	"github.com/delaneyj/toolbelt"
	"github.com/gorilla/sessions"
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
