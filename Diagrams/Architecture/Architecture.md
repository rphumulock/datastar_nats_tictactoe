flowchart TD
subgraph Application
A["main.go"]
A --> B["routes.SetupRoutes(ctx, logger, router)"]
end

    subgraph Embedded NATS Server
        C["NATS Server (JetStream Enabled)"]
        C --> D["Key-Value Bucket: 'games'"]
        C --> E["Key-Value Bucket: 'users'"]
    end

    subgraph HTTP Server
        F["HTTP Router (chi)"]
        F --> G["Static File Handling (/static/*)"]
        F --> H["API Routes"]
    end

    subgraph Routes
        H --> I["Index Routes"]
        H --> J["Game Routes"]
    end

    subgraph Index Routes
        I --> K["GET /"]
        I --> L["GET /api/dashboard"]
        I --> M["GET /api/dashboard/login"]
        I --> N["POST /api/dashboard/lobby/create"]
        I --> O["DELETE /api/dashboard/lobby/{id}/delete"]
        I --> P["DELETE /api/dashboard/lobby/purge"]
        I --> Q["GET /api/dashboard/lobby/watch"]
    end

    subgraph Game Routes
        J --> R["GET /game/{id}"]
        J --> S["GET /api/game/{id}/init"]
        J --> T["GET /api/game/{id}/watch"]
        J --> U["POST /api/game/{id}/toggle/{cell}"]
        J --> V["POST /api/game/{id}/leave"]
        J --> W["POST /api/game/{id}/reset"]
    end

    subgraph Components
        X["Frontend Templates"]
        X --> Y["components.Login()"]
        X --> Z["components.Dashboard(sessionId)"]
        X --> AA["components.Game(mvc, sessionId)"]
        X --> AB["components.GameListItem(mvc, sessionId)"]
        X --> AC["components.GameBoard(gameState, sessionId)"]
        X --> AD["components.GameWinner(gameState, sessionId)"]
    end

    A --> F
    F --> X
    B --> C
    B --> F
    C --> D
    C --> E
