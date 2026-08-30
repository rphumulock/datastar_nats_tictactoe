package routes

import (
	"log"

	"github.com/delaneyj/toolbelt"
	"github.com/rphumulock/datastar-nats-tictactoe/web/components"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// Toasts replace the browser's alert(): alert is unstyleable, and it blocks the
// main thread until it is dismissed, which stalls the SSE-driven board behind it.
// A toast is appended to the page instead, so a rejected move never stops the
// game from updating underneath it.
//
// Use these only when the player stays on the page. A message issued alongside a
// redirect is never read - the navigation discards it - so those paths just
// redirect.
func toastWarning(sse *datastar.ServerSentEventGenerator, message string) {
	pushToast(sse, message, "alert-warning")
}

func toastError(sse *datastar.ServerSentEventGenerator, message string) {
	pushToast(sse, message, "alert-error")
}

func pushToast(sse *datastar.ServerSentEventGenerator, message, kind string) {
	id := "toast-" + toolbelt.NextEncodedID()
	// If the toast could not be written the stream is already broken, so there
	// is nowhere left to report it to but the log.
	if err := sse.PatchElementTempl(
		components.Toast(id, message, kind),
		datastar.WithSelector("#toast-host"),
		datastar.WithMode(datastar.ElementPatchModeAppend),
	); err != nil {
		log.Printf("toast: patch failed: %v", err)
	}
}
