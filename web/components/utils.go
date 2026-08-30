package components

import (
	"fmt"
	"time"
)

type InlineValidationUserName struct {
	Name string `json:"name"`
}

type User struct {
	Name      string `json:"name"`
	SessionId string `json:"session_id"`
}

type GameLobby struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	HostId string `json:"host_id"`
	// HostName is denormalised from the users bucket so rendering a lobby
	// card needs no second lookup, and still shows a name if the host's
	// user record has since expired.
	HostName     string `json:"host_name"`
	ChallengerId string `json:"challenger_id"`
}

type GameState struct {
	Id      string    `json:"id"`
	Board   [9]string `json:"board"`
	XIsNext bool      `json:"turn"`
	Winner  string    `json:"winner"`
}

// AdminGame is a lobby joined with everything the admin table shows: whether
// its players still have live user records, how far the board got, and how long
// the lobby has been sitting there.
type AdminGame struct {
	Id               string
	Name             string
	HostId           string
	HostName         string
	HostOnline       bool
	ChallengerId     string
	ChallengerName   string
	ChallengerOnline bool
	Moves            int
	Winner           string
	Created          time.Time
	Age              string
	// Orphaned marks a lobby nobody can clean up from the dashboard, because
	// the host's user record is gone and only the host gets a delete button.
	Orphaned bool
}

type AdminUser struct {
	SessionId string
	Name      string
	// Online is true while the session holds an open SSE stream.
	Online  bool
	Created time.Time
	Age     string
	Games   int
}

type AdminSnapshot struct {
	Games    []AdminGame
	Users    []AdminUser
	Orphaned int
	Finished int
}

// HumanAge renders a KV entry's age compactly enough to fit a table cell.
func HumanAge(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
