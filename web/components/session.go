package components

import (
	"fmt"

	"github.com/a-h/templ"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// SessionKeepaliveSeconds is how often an open page pings the server to slide
// its session cookie. It must stay comfortably shorter than the server's
// sessionMaxAge; see routes/session.go.
const SessionKeepaliveSeconds = 600

// SessionKeepaliveAttrs builds the keepalive interval attribute. Datastar keeps
// the duration in the attribute *name*, which a template cannot interpolate, so
// building it here is what lets the period live in one constant instead of being
// retyped in every page that needs it.
//
// The unit is seconds on purpose: Datastar parses only "ms" and "s", so a value
// like "10m" is read as ten milliseconds rather than ten minutes.
func SessionKeepaliveAttrs() templ.Attributes {
	return templ.Attributes{
		fmt.Sprintf("data-on-interval__duration.%ds", SessionKeepaliveSeconds): datastar.PostSSE("/api/session/touch"),
	}
}
