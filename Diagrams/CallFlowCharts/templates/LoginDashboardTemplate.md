flowchart LR
subgraph subGraph0["Dashboard Template"]
E@{ label: "Display Welcome Message: 'Welcome, sessionId'" }
G@{ label: "Trigger datastar.PostSSE('/api/dashboard/lobby/create')" }
F["Create Game Button"]
I@{ label: "Trigger datastar.GetSSE('/api/dashboard/lobby/watch')" }
H["Active Games Section"]
K@{ label: "Trigger datastar.DeleteSSE('/api/dashboard/lobby/purge')" }
J["Delete All Games Button"]
end
subgraph subGraph1["GameListItem Template"]
M{"Check Game Status"}
L["Display Game ID"]
O@{ label: "Provide 'Join Game' Link" }
P@{ label: "Show 'Delete Game' Button" }
Q@{ label: "Trigger datastar.DeleteSSE('/api/dashboard/lobby/{id}/delete')" }
end
A["Login Template"] --> B@{ label: "User Clicks 'Login' Button" }
B --> C@{ label: "Trigger datastar.GetSSE('/api/dashboard/login')" }
C --> D["Redirect to Dashboard"]
F --> G
H --> I
J --> K
D --> E
L --> M
M -- Available --> O
M -- Host --> P
P --> Q

    B@{ shape: rounded}
    C@{ shape: diamond}
    E@{ shape: rect}
    G@{ shape: diamond}
    I@{ shape: diamond}
    K@{ shape: diamond}
    O@{ shape: rect}
    P@{ shape: rect}
    Q@{ shape: diamond}
