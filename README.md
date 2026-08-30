# NORTHSTAR - A Hypermedia Application Starter Template

# Stack

- [Go](https://go.dev/doc/)
- [NATS](https://docs.nats.io/)
- [Datastar](https://github.com/@starfederation/datastar)
- [Templ](https://templ.guide/)
  - [Tailwind](https://tailwindcss.com/) x [DaisyUI](https://daisyui.com/) x [esbuild](https://esbuild.github.io/)

# Setup

1. Clone this repository

```shell
git clone https://github.com/zangster300/northstar.git
```

2. Install Dependencies

```shell
pnpm install
go mod tidy
```

3. Create 🚀

# Development

Live Reload is setup out of the box - powered by [Air](https://github.com/air-verse/air) and [templ](https://templ.guide/commands-and-tools/live-reload-with-other-tools#putting-it-all-together)'s proxy server

Use the [live task](./Taskfile.yml#L78) from the [Taskfile](https://taskfile.dev/) to start the server

```shell
task live
```

Navigate to [`http://localhost:7331`](http://localhost:7331) in your favorite web browser to begin

## Debugging

The [debug task](<(./Taskfile.yml#L33)>) will launch [delve](https://github.com/go-delve/delve) to begin a debugging session with your project's binary

```shell
task debug
```

## IDE Support

- [Templ / TailwindCSS Support](https://templ.guide/commands-and-tools/ide-support)

### Visual Studio Code Integration

[Reference](https://code.visualstudio.com/docs/languages/go)

- [launch.json](./.vscode/launch.json)
- [settings.json](./.vscode/settings.json)

a `Debug Main` configuration has been added to the [launch.json](./.vscode/launch.json) file to set breakpoints

# Starting the Server

```shell
task run
```

Navigate to [`http://localhost:8080`](http://localhost:8080) in your favorite web browser

# Deployment

## Building an Executable

The `task build` [task](./Taskfile.yml#L26) will assemble and build a binary [with static assets embedded](./static_prod.go#L19)

## Docker

```shell
# build an image
docker build -t northstar:latest .

# run the image in a container
docker run --name northstar -p 8080:9001 northstar:latest
```

[Dockerfile](./Dockerfile)

# Contributing

Completely open to PR's and feature requests

# References

## Server

- [go](https://go.dev/)
- [nats](https://docs.nats.io/)
- [datastar](https://datastar.fly.dev/)
- [templ](https://templ.guide/)

> [!IMPORTANT]  
> The `TODO` example relies on the [`TEMPL_EXPERIMENT=rawgo`](https://templ.guide/syntax-and-usage/raw-go/) environment variable being set

### Embedded NATS

An embedded NATS server that powers the `TODO` application is configured and booted up in the [router.go](./handlers/router.go#L16) file

To interface with it, you should install the [nats-cli](https://github.com/nats-io/natscli)

Here are some commands to inspect and make changes to the bucket backing the `TODO` app:

```shell
# list key value buckets
nats kv ls

# list keys in the `todos` bucket
nats kv ls todos

# get the value for [key]
nats kv get --raw todos [key]

# put a value into [key]
nats kv put todos [key] '{"todos":[{"text":"Hello, NATS!","completed":true}],"editingIdx":-1,"mode":0}'
```

> [!IMPORTANT]  
> To see these updates take place in realtime within the `TODO` example, make sure your browser is pointed to the real server and not the templ proxy server!

## Client

- [tailwindcss](https://tailwindcss.com/)
- [daisyui](https://daisyui.com/)
- [esbuild](https://esbuild.github.io/)
- [lit-html](https://lit.dev/)

### Web Components x Datastar

[🔗 Web Components Setup](./web/libs/lit-html/README.md)

## Hypermedia Architecture

- [Hypermedia-Driven Applications](https://htmx.org/essays/hypermedia-driven-applications/)
- [HTMX Sucks](https://htmx.org/essays/htmx-sucks/)
- [HTMX Sucks (kinda)](https://datastar.fly.dev/essays/htmx_sucks)
- [Streams All the Way Down](https://datastar.fly.dev/essays/event_streams_all_the_way_down)

## Feedback Messages

Rejected actions - an occupied cell, a move out of turn, a lobby that filled up
first - surface as toasts appended to `#toast-host`, a container that lives in
the base layout. `alert()` is not used: it cannot be styled, and it blocks the
main thread until dismissed, which stalls the SSE-driven board behind it.

Each toast carries a fresh id so appending a second message never morphs the
first in place, and removes itself via `data-init__delay.5s="el.remove()"`.
Helpers are in [`routes/toast.go`](./routes/toast.go). Messages that would
accompany a redirect are dropped rather than sent - the navigation discards them
before they can be read.

## Sessions

The session cookie **slides**: every request re-issues it, so its 45-minute clock
only runs while the tab is idle. Because an SSE stream is a single long-lived
request that never re-issues a cookie on its own, the dashboard and game pages
also ping `/api/session/touch` every 10 minutes (`data-on-interval__duration.600s`
- Datastar durations parse only `ms` and `s`, so `10m` would mean ten
milliseconds), which slides the cookie and resets the TTL on the player's user
record. An active player is no longer logged
out mid-game and handed a new session id - which is what used to strand their
lobbies under a host nobody could sign in as.

The sliding refresh is chi middleware ([`routes/session.go`](./routes/session.go))
rather than per-handler code, because `Set-Cookie` has to be written before the
response starts - which for the SSE endpoints is the moment their generator is
created.

## Presence and Abandoned Games

A lobby is only shown to players while its host is **present** - that is, while
the host's browser holds an open SSE stream, either on the dashboard or on a
game board. Every such stream heartbeats a key into a `presence` KV bucket whose
TTL (90s) expires it shortly after the browser goes away.

That single signal drives both halves of the cleanup:

- The dashboard render filters out lobbies whose host is not present, so a game
  belonging to a closed browser stops appearing right away.
- A background reaper (every minute) purges those lobbies and their boards, and
  reopens lobbies whose *challenger* has gone, so the host gets a joinable game
  back instead of a dead one. Lobbies are given a two-minute grace period from
  their last write, so nothing is purged before its host's first heartbeat.

Tuning lives in [`routes/presence.go`](./routes/presence.go).

## Admin Panel

`/admin` lists every game lobby and user session held in NATS KV, with the state
the dashboard hides: which lobbies are **orphaned** (the host's user record is
gone, so nobody has a delete button for them), how far each board got, and how
long each key has been around.

Unlock it with `ADMIN_TOKEN`:

```shell
ADMIN_TOKEN=some-secret task run
```

If `ADMIN_TOKEN` is unset a random token is generated per run and printed to the
server log, so a local server stays usable without leaving a deployed one open.

Rows the dashboard is hiding are flagged `hidden from dashboard`, and the user
table marks who is currently connected, so the panel shows exactly what players
would and would not see.

Actions: delete a single game or user, purge orphaned games, purge finished
games, purge games older than N minutes, purge all games, purge all users. Day
to day the reaper handles the first of those on its own; the panel is for
immediate cleanup and for data left over from an earlier run.

> [!NOTE]
> The embedded NATS server is started without a `StoreDir`, so JetStream falls
> back to `/tmp/nats/jetstream` and lobbies survive a restart of the app. The KV
> buckets expire entries after an hour; the panel is for the stretch before that.
