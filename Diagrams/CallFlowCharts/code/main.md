flowchart TD
A["main()"] --> B["Initialize Logger"]
B --> C["Get Port (Default: 8080)"]
C --> D["Set Up Context with Signal Handling"]
D --> E["Call run(ctx, logger, port)"]

    subgraph run Function
        E --> F["Create errgroup Context"]
        F --> G["Start Server (startServer)"]
        G --> H["Wait for Server Execution (g.Wait)"]
        H --> I{"Error Occurred?"}
        I -- Yes --> J["Log Error and Return"]
        I -- No --> K["Exit Successfully"]
    end

    subgraph startServer Function
        G --> L["Create Router with chi.NewMux"]
        L --> M["Add Middleware (Logger, Recoverer)"]
        M --> N["Serve Static Files at /static/*"]
        N --> O["Set Up Routes via routes.SetupRoutes"]
        O --> P{"Routes Setup Successfully?"}
        P -- Yes --> Q["Create HTTP Server"]
        P -- No --> R["Log Error and Return"]
        Q --> S["Listen and Serve HTTP Requests"]
        S --> T["Handle Shutdown on Context Cancellation"]
    end
