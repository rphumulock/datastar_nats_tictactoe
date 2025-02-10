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
		return fmt.Errorf("failed to get 'users' KV bucket: %w", err)
	}

	saveUser := func(ctx context.Context, user *components.User) error {
		data, err := json.Marshal(user)
		if err != nil {
			return fmt.Errorf("failed to marshal user: %w", err)
		}
		if _, err := usersKV.Put(ctx, user.SessionId, data); err != nil {
			return fmt.Errorf("failed to store user in KV: %w", err)
		}
		return nil
	}

	userSession := func(w http.ResponseWriter, r *http.Request, inlineUser *components.InlineValidationUser) (*components.User, error) {
		sessCtx := r.Context()

		sessionID, err := createSessionId(store, r, w)
		if err != nil {
			return nil, fmt.Errorf("failed to get session id: %w", err)
		}

		user := &components.User{
			SessionId: sessionID,
			Name:      inlineUser.Name,
		}

		if err := saveUser(sessCtx, user); err != nil {
			return nil, fmt.Errorf("failed to save user: %w", err)
		}
		return user, nil
	}

	userValidation := func(u *components.InlineValidationUser) bool {
		return len(u.Name) >= 2
	}

	loadInlineUser := func(r *http.Request) (*components.InlineValidationUser, error) {
		inlineUser := &components.InlineValidationUser{}
		if err := datastar.ReadSignals(r, inlineUser); err != nil {
			return nil, err
		}
		return inlineUser, nil
	}

	handleGetLoginPage := func(w http.ResponseWriter, r *http.Request) {
		inlineUser, err := loadInlineUser(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		sse := datastar.NewSSE(w, r)
		isNameValid := userValidation(inlineUser)
		sse.MergeFragmentTempl(
			components.InlineValidationUserComponent(inlineUser, isNameValid),
			datastar.WithSelectorID("login"),
		)
	}

	handlePostLogin := func(w http.ResponseWriter, r *http.Request) {
		inlineUser, err := loadInlineUser(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if _, err := userSession(w, r, inlineUser); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		sse := datastar.NewSSE(w, r)
		sse.Redirect("/dashboard")
	}

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		sessionID, err := getSessionId(store, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if sessionID != "" {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		pages.Index().Render(r.Context(), w)
	})

	router.Route("/api/index", func(indexRouter chi.Router) {
		indexRouter.Get("/", handleGetLoginPage)
		indexRouter.Post("/login", handlePostLogin)
	})

	return nil
}
