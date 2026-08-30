package routes

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rphumulock/datastar-nats-tictactoe/web/components"

	datastar "github.com/starfederation/datastar-go/datastar"
)

const (
	// themeCookieName holds the visitor's DaisyUI theme choice.
	themeCookieName = "theme"

	// themeMaxAge outlives a session deliberately: the theme is a display
	// preference, not part of being signed in, so it should survive logging out.
	themeMaxAge = 365 * 24 * time.Hour
)

// themeContext puts the cookie's theme on the request context for Base to
// render. Doing it server-side is what keeps the first paint correct - the
// alternative, letting the client apply a stored theme after load, shows the
// default theme for a frame first.
func themeContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		theme := components.DefaultTheme
		if c, err := r.Cookie(themeCookieName); err == nil && components.IsTheme(c.Value) {
			theme = c.Value
		}
		next.ServeHTTP(w, r.WithContext(components.WithTheme(r.Context(), theme)))
	})
}

// setupThemeRoute persists a theme choice. The page has already repainted from
// the signal by the time this runs, so it writes the cookie and nothing else.
func setupThemeRoute(router chi.Router) error {
	handleSetTheme := func(w http.ResponseWriter, r *http.Request) {
		signals := struct {
			Theme string `json:"theme"`
		}{}
		if err := datastar.ReadSignals(r, &signals); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !components.IsTheme(signals.Theme) {
			http.Error(w, "unknown theme", http.StatusBadRequest)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     themeCookieName,
			Value:    signals.Theme,
			Path:     "/",
			MaxAge:   int(themeMaxAge / time.Second),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		w.WriteHeader(http.StatusNoContent)
	}

	router.Post("/api/theme", handleSetTheme)

	return nil
}
