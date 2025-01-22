flowchart TD
A["SetupRoutes(ctx, logger, router)"] --> B["Start Embedded NATS Server"]
B --> C["Configure NATS Server Options (Port: 1234, JetStream: Enabled)"]
C --> D["Wait for NATS Server to Start"]
D --> E{"NATS Server Started?"}
E -- Yes --> F["Create NATS Client"]
E -- No --> G["Return Error"]

    F --> H["Access JetStream"]
    H --> I{"JetStream Available?"}
    I -- Yes --> J["Create 'games' Key-Value Bucket"]
    I -- No --> G["Return Error"]
    J --> K["Create 'users' Key-Value Bucket"]

    K --> L["Set Up Session Store (Cookie-Based)"]
    L --> M["Set Up Application Routes"]
    M --> N["setupIndexRoute(router, sessionStore, js)"]
    M --> O["setupGameRoute(router, sessionStore, js)"]

    N --> P{"Routes Setup Successfully?"}
    O --> P
    P -- Yes --> Q["Return Cleanup Function"]
    P -- No --> G["Return Error"]

    subgraph Cleanup Process
        Q --> R["Close Embedded NATS Server"]
    end
