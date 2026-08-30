package components

import (
	"context"
	"fmt"
)

// Themes are the DaisyUI themes compiled into the stylesheet, in the order the
// picker lists them. This must stay in sync with the `daisyui.themes` array in
// tailwind.config.js: a theme missing from that array has no CSS variables in
// the bundle, so selecting it here would strip the page of its colors.
var Themes = []string{
	"retro", "light", "dark", "cupcake", "bumblebee", "emerald", "corporate",
	"synthwave", "cyberpunk", "valentine", "halloween", "garden", "forest",
	"aqua", "lofi", "pastel", "fantasy", "wireframe", "black", "luxury",
	"dracula", "cmyk", "autumn", "business", "acid", "lemonade", "night",
	"coffee", "winter", "dim", "nord", "sunset",
}

// DefaultTheme is what a visitor sees before they have chosen anything.
const DefaultTheme = "retro"

var themeSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(Themes))
	for _, t := range Themes {
		m[t] = struct{}{}
	}
	return m
}()

// IsTheme reports whether name is a theme in the bundle. The theme arrives in a
// cookie and in a signal, both of which the client controls, and it ends up
// rendered into an attribute and into a Datastar expression - so an unrecognised
// name is rejected rather than echoed back.
func IsTheme(name string) bool {
	_, ok := themeSet[name]
	return ok
}

type themeCtxKey struct{}

// WithTheme stores the request's theme for Base to render. It travels in the
// context rather than as a parameter so that adding a theme to the page does not
// change the signature of every layout and page in the tree.
func WithTheme(ctx context.Context, theme string) context.Context {
	return context.WithValue(ctx, themeCtxKey{}, theme)
}

// ThemeFrom returns the theme for this request, falling back to the default when
// no middleware ran or the stored value is not a known theme.
func ThemeFrom(ctx context.Context) string {
	if t, ok := ctx.Value(themeCtxKey{}).(string); ok && IsTheme(t) {
		return t
	}
	return DefaultTheme
}

// ThemeSignals seeds the `theme` signal from the server-rendered value, so the
// signal and the data-theme attribute agree on the first paint.
func ThemeSignals(ctx context.Context) string {
	return fmt.Sprintf("{theme: '%s'}", ThemeFrom(ctx))
}

// setThemeExpr switches the theme locally and persists it. Assigning the signal
// repaints immediately through the data-attr binding on <html>; the POST only
// writes the cookie, so the change never waits on the network.
func setThemeExpr(theme string) string {
	return fmt.Sprintf("$theme = '%s'; @post('/api/theme')", theme)
}
