package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rphumulock/datastar_nats_tictactoe/web/components"
	"github.com/rphumulock/datastar_nats_tictactoe/web/pages"

	datastar "github.com/starfederation/datastar/sdk/go"
)

func setupIndexRoute(router chi.Router, store sessions.Store, js jetstream.JetStream) error {
	ctx := context.Background()

	usersKV, err := js.KeyValue(ctx, "users")
	if err != nil {
		return fmt.Errorf("failed to get users key value: %w", err)
	}

	saveUser := func(ctx context.Context, user *components.User) error {
		b, err := json.Marshal(user)
		if err != nil {
			return fmt.Errorf("failed to marshal mvc: %w", err)
		}
		if _, err := usersKV.Put(ctx, user.SessionId, b); err != nil {
			return fmt.Errorf("failed to put key value: %w", err)
		}
		return nil
	}

	userSession := func(w http.ResponseWriter, r *http.Request, u *components.InlineValidationUser) (*components.User, error) {
		ctx := r.Context()

		SessionId, err := createSessionId(store, r, w)
		if err != nil {
			return nil, fmt.Errorf("failed to get session id: %w", err)
		}

		user := &components.User{
			SessionId: SessionId,
			Name:      u.Name,
		}
		if err := saveUser(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to save mvc: %w", err)
		}
		return user, nil
	}

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		user, err := getSessionId(store, r)
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

		userValidation := func(u *components.InlineValidationUser) (isNameValid bool) {
			isNameValid = len(u.Name) >= 2
			return isNameValid
		}

		indexRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			u := &components.InlineValidationUser{}
			if err := datastar.ReadSignals(r, u); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			isNameValid := userValidation(u)
			sse := datastar.NewSSE(w, r)
			sse.MergeFragmentTempl(
				components.InlineValidationUserComponent(u, isNameValid),
				datastar.WithSelectorID("login"),
			)
		})

		indexRouter.Post("/login", func(w http.ResponseWriter, r *http.Request) {
			u := &components.InlineValidationUser{}
			if err := datastar.ReadSignals(r, u); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if _, err := userSession(w, r, u); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			sse := datastar.NewSSE(w, r)
			sse.Redirect("/dashboard")
		})

	})

	return nil
}
