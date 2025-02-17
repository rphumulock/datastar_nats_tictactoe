package components

type InlineValidationUserName struct {
	Name string `json:"name"`
}

type User struct {
	Name      string `json:"name"`
	SessionId string `json:"session_id"`
}

type GameLobby struct {
	Id           string `json:"id"`
	Name         string `json:"name"`
	HostId       string `json:"host_id"`
	ChallengerId string `json:"challenger_id"`
}

type GameState struct {
	Id      string    `json:"id"`
	Board   [9]string `json:"board"`
	XIsNext bool      `json:"turn"`
	Winner  string    `json:"winner"`
}
