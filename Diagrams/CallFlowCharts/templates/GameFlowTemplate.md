flowchart TD
A["Game Template"] --> B["Welcome Message: sessionId"]
B --> C{"Check if Host"}
C -- Yes --> D["Host: 'You!'"]
C -- No --> E["Host: Players[0]"]
B --> F["Game Board Section"]
F --> G["Render GameBoard Template"]
F --> H["Load via datastar.GetSSE('/api/game/{gameId}/watch')"]

    subgraph GameBoard Template
        I["Check Winner"]
        I -- Yes --> J["Render GameWinner Template"]
        I -- No --> K["Iterate Through Board Cells"]
        K --> L["Render Cell Template for Each Cell"]
    end

    subgraph Cell Template
        M["Check if Cell is Clicked or Winner Exists"]
        M -- Yes --> N["Disable Cell"]
        M -- No --> O["Enable Click and Trigger datastar.PostSSE('/api/game/{gameId}/toggle/{i}')"]
    end

    subgraph GameWinner Template
        P["Check Winner State"]
        P -- Tie --> Q["Show 'It's a Tie!' Message"]
        P -- Winner Exists --> R["Show 'Winner {winner} Wins!' Message"]
        Q --> S["Play Again Button: Trigger datastar.PostSSE('/api/game/{gameId}/reset')"]
        R --> S
        S --> T["Back to Dashboard Button"]
    end

    H --> I
