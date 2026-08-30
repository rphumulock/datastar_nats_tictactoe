package components

import "fmt"

// confirmed wraps a Datastar action in a browser confirm, so the destructive
// bulk buttons need a deliberate second click.
func confirmed(message, action string) string {
	return fmt.Sprintf("confirm(%s) && %s", jsString(message), action)
}

func jsString(s string) string {
	quoted := ""
	for _, r := range s {
		switch r {
		case '\'':
			quoted += `\'`
		case '\\':
			quoted += `\\`
		default:
			quoted += string(r)
		}
	}
	return "'" + quoted + "'"
}
