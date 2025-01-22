flowchart TD
A["setupIndexRoute"] --> B["GET /"]
B --> C["Render Index Page via pages.Index()"]

    subgraph apiDashboard
        D["GET /api/dashboard/"]
        D --> E{"Check Session ID"}
        E -- Session Exists --> F["Render Dashboard via components.Dashboard(sessionId)"]
        E -- No Session --> G["Render Login via components.Login()"]

        H["GET /api/dashboard/login"]
        H --> I["Create New User Session"]
        I --> F["Render Dashboard via components.Dashboard(sessionId)"]
    end

    subgraph lobbyRoutes
        J["POST /api/dashboard/lobby/create"]
        J --> K["Create New GameState"]
        K --> L["Store GameState in 'games' Bucket"]

        M["DELETE /api/dashboard/lobby/{id}/delete"]
        M --> N["Delete GameState by ID from 'games' Bucket"]

        O["DELETE /api/dashboard/lobby/purge"]
        O --> P["Retrieve All Keys from 'games' Bucket"]
        P --> Q["Delete Each Key in 'games' Bucket"]
        Q --> R["Send Success Response"]

        S["GET /api/dashboard/lobby/watch"]
        S --> T["Start Watching 'games' Bucket Updates"]
        T --> U{"Update Type"}
        U -- KeyValuePut --> V["Process Update and Merge New GameListItem"]
        U -- KeyValueDelete --> W["Remove Deleted GameListItem"]
    end

    C --> apiDashboard
    F --> lobbyRoutes
