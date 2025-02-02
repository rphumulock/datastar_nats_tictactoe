package components

type InlineValidationUser struct {
	Name string `json:"name"`
}

type User struct {
	Name      string `json:"name"`
	SessionId string `json:"session_id"`
}

type OpenGames struct {
	Id       string `json:"id"`
	HostId   string `json:"host_id"`
	HostName string `json:"host_name"`
}

type GameState struct {
	Id           string    `json:"id"`
	HostId       string    `json:"host_id"`
	HostName     string    `json:"host_name"`
	ChallengerId string    `json:"challenger_id"`
	Board        [9]string `json:"board"`
	XIsNext      bool      `json:"turn"`
	Winner       string    `json:"winner"`
}
