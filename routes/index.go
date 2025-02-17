package routes

import (
	"context"
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

	handleGetIndex := func(w http.ResponseWriter, r *http.Request) {
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
	}

	router.Get("/", handleGetIndex)

	// API

	userValidation := func(u *components.InlineValidationUserName) bool {
		return len(u.Name) >= 2
	}

	loadInlineUser := func(r *http.Request) (*components.InlineValidationUserName, error) {
		inlineUser := &components.InlineValidationUserName{}
		if err := datastar.ReadSignals(r, inlineUser); err != nil {
			return nil, err
		}
		return inlineUser, nil
	}

	handleGetLoginComponent := func(w http.ResponseWriter, r *http.Request) {
		inlineUser, err := loadInlineUser(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		sse := datastar.NewSSE(w, r)
		isNameValid := userValidation(inlineUser)
		sse.MergeFragmentTempl(
			components.InlineValidationUserNameComponent(inlineUser, isNameValid),
		)
	}

	createUser := func(sessionId, name string) *components.User {
		return &components.User{
			SessionId: sessionId,
			Name:      name,
		}
	}

	userSession := func(w http.ResponseWriter, r *http.Request, inlineUser *components.InlineValidationUserName) (*components.User, error) {
		ctx := r.Context()

		SessionId, err := createSessionId(store, r, w)
		if err != nil {
			return nil, fmt.Errorf("failed to get session id: %w", err)
		}

		user := createUser(SessionId, inlineUser.Name)

		if err := PutData(ctx, usersKV, user.SessionId, user); err != nil {
			return nil, fmt.Errorf("failed to put user data: %w", err)
		}

		return user, nil
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

	router.Route("/api/index", func(indexRouter chi.Router) {
		indexRouter.Get("/", handleGetLoginComponent)
		indexRouter.Post("/login", handlePostLogin)
	})

	return nil
}
