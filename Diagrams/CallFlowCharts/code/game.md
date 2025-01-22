flowchart TD
A["setupGameRoute"] --> B["GET /game/{id}"]
B --> C["Render Game Page via pages.Game(id)"]

    subgraph gameAPI [Game API Routes]
        D["GET /api/game/{id}/init"]
        D --> E["Retrieve Game State"]
        E --> F{"Game Full?"}
        F -- Yes --> G["Return Error: Game Full"]
        F -- No --> H{"Player Already in Game?"}
        H -- No --> I["Add Player to Game"]
        H -- Yes --> J["Render Game Template via components.Game"]
        I --> J

        K["GET /api/game/{id}/watch"]
        K --> L["Start Watching Game Updates"]
        L --> M{"Update Type"}
        M -- KeyValuePut --> N["Update Game Board via components.GameBoard"]
        M -- KeyValueDelete --> O["Redirect to '/'"]

        P["POST /api/game/{id}/toggle/{cell}"]
        P --> Q["Validate Cell Index and Game State"]
        Q --> R{"Valid Move?"}
        R -- Yes --> S["Update Board with Move"]
        S --> T{"Winner or Tie?"}
        T -- Winner --> U["Set Winner"]
        T -- Tie --> V["Set Result as Tie"]
        U --> W["Update Game State"]
        V --> W

        X["POST /api/game/{id}/leave"]
        X --> Y["Remove Player from Game"]
        Y --> Z["Redirect to '/'"]

        AA["POST /api/game/{id}/reset"]
        AA --> AB{"Is Host?"}
        AB -- Yes --> AC["Reset Board and Game State"]
        AC --> AD["Update Game State"]
        AB -- No --> AE["Return Error: Not Your Game"]
    end

    C --> gameAPI
