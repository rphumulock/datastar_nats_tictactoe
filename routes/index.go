package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"github.com/delaneyj/toolbelt"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"

	datastar "github.com/starfederation/datastar/sdk/go"
)

type User struct {
	SessionId string `json:"session_id"` // Unique session ID for the user
	GameId    string `json:"game_id"`    // List of games the user is in
}

func setupIndexRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	usersKV, err := js.KeyValue(ctx, "users")
	if err != nil {
		return fmt.Errorf("failed to get games key value: %w", err)
	}

	saveUser := func(ctx context.Context, user *User) error {
		b, err := json.Marshal(user)
		if err != nil {
			return fmt.Errorf("failed to marshal mvc: %w", err)
		}
		if _, err := usersKV.Put(ctx, user.SessionId, b); err != nil {
			return fmt.Errorf("failed to put key value: %w", err)
		}
		return nil
	}

	userSession := func(w http.ResponseWriter, r *http.Request) (*User, error) {
		ctx := r.Context()

		SessionId, err := createSessionID(store, r, w)
		if err != nil {
			return nil, fmt.Errorf("failed to get session id: %w", err)
		}

		user := &User{
			SessionId: SessionId,
		}
		if err := saveUser(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to save mvc: %w", err)
		}
		return user, nil
	}

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		user, err := getSessionID(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if user != "" {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		pages.Index().Render(r.Context(), w)
	})

	router.Route("/api/index", func(indexRouter chi.Router) {

		indexRouter.Route("/inline_validation/data", func(dataRouter chi.Router) {
			userValidation := func(u *components.InlineValidationUser) (isEmailValid bool, isFirstNameValid bool, isLastNameValid bool, isValid bool) {
				isEmailValid = u.Email == "test@test.com"
				isFirstNameValid = len(u.FirstName) >= 2
				isLastNameValid = len(u.LastName) >= 2
				isValid = isFirstNameValid && isLastNameValid && isEmailValid
				return isEmailValid, isFirstNameValid, isLastNameValid, isValid
			}

			dataRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				u := &components.InlineValidationUser{}
				if err := datastar.ReadSignals(r, u); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				sse := datastar.NewSSE(w, r)
				isEmailValid, isFirstNameValid, isLastNameValid, isValid := userValidation(u)
				sse.MergeFragmentTempl(components.InlineValidationUserComponent(u, isEmailValid, isFirstNameValid, isLastNameValid, isValid))
			})

			dataRouter.Post("/", func(w http.ResponseWriter, r *http.Request) {
				u := &components.InlineValidationUser{}
				if err := datastar.ReadSignals(r, u); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}

				isEmailValid, isFirstNameValid, isLastNameValid, isValid := userValidation(u)

				sse := datastar.NewSSE(w, r)
				var node templ.Component
				if !isValid {
					node = components.InlineValidationUserComponent(u, isEmailValid, isFirstNameValid, isLastNameValid, isValid)
				} else {
					node = components.InlineValidationUserComponent()
				}

				sse.MergeFragmentTempl(node)
			})
		})

		indexRouter.Post("/login", func(w http.ResponseWriter, r *http.Request) {
			if _, err := userSession(w, r); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			sse := datastar.NewSSE(w, r)
			sse.Redirect("/dashboard")
		})

	})

	return nil
}

// Check if a user session exists
func getSessionID(store sessions.Store, r *http.Request) (string, error) {
	sess, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	id, ok := sess.Values["id"].(string)
	if !ok || id == "" {
		return "", nil // No session ID exists
	}
	return id, nil
}

// Create a new user session
func createSessionID(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {
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
