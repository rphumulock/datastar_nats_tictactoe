# Datastar NATS Tic Tac Toe

## Why this project exists

This is a learning project. I built it to mess around with three things I wanted
hands-on experience with:

- **[Datastar](https://data-star.dev/)** — driving a whole UI from the server over
  SSE, with no client-side application code.
- **CQRS** — separating writes (a move is a `POST` command that validates and
  writes to a bucket) from reads (SSE streams that watch buckets and re-render).
- **[NATS JetStream KV](https://docs.nats.io/)** — using a KV store, embedded
  in-process, as the only source of truth instead of a database.

Tic-tac-toe is just the excuse; it is small enough that the plumbing stays visible.

A real-time, multiplayer tic-tac-toe game built as a **hypermedia application**: the
server owns all state and streams HTML to the browser over SSE. There is no
client-side application code and no JSON API for the UI — a move is a `POST`, and
both players' boards update because the server pushes new markup down their open
streams.

State lives entirely in an **embedded NATS JetStream** server running inside the Go
process. There is no external database and no separate NATS to run; `go build`
produces one binary that is the whole application.

## Stack

| Layer | Tool |
| --- | --- |
| Server | [Go](https://go.dev/doc/) + [chi](https://github.com/go-chi/chi) |
| State | [NATS JetStream KV](https://docs.nats.io/), embedded in-process |
| Browser updates | [Datastar](https://data-star.dev/) v1.0.3 over SSE |
| Templates | [Templ](https://templ.guide/) |
| Styles | [Tailwind](https://tailwindcss.com/) x [DaisyUI](https://daisyui.com/) |

## How it works

1. A player enters a name on `/` and gets a session cookie. A `User` record is
   written to the `users` bucket.
2. `/dashboard` opens a long-lived SSE stream (`/api/dashboard/updates`) that
   watches the `gameLobbies` bucket and re-renders the lobby list on every change.
   Creating or joining a lobby is a plain `POST`; every connected dashboard sees
   the result because they are all watching the same bucket.
3. `/game/{id}` opens a second stream (`/api/game/{id}/updates`) watching that
   game's board. Clicking a cell `POST`s to `/api/game/{id}/toggle/{cell}`; the
   server validates the move, writes the new board to `gameBoards`, and both
   players' streams push the updated markup.

Every SSE stream also heartbeats into a `presence` bucket, which is what makes
abandoned lobbies detectable — see [Presence and Abandoned Games](#presence-and-abandoned-games).

Call-flow diagrams live in [`Diagrams/`](./Diagrams).

## Setup

```shell
git clone https://github.com/rphumulock/datastar-nats-tictactoe.git
cd datastar-nats-tictactoe

pnpm install
go mod tidy
```

Requires Go 1.26+, [pnpm](https://pnpm.io/), and [Task](https://taskfile.dev/).

## Development

Live reload is set up out of the box — [Air](https://github.com/air-verse/air) for
the Go binary, [templ](https://templ.guide/commands-and-tools/live-reload-with-other-tools)'s
proxy server for the browser, and Tailwind in watch mode.

```shell
task live
```

Then open **[`http://localhost:7331`](http://localhost:7331)** — the templ proxy,
which injects the reload script. The real server is on `8080` behind it.

> [!IMPORTANT]
> Point your browser at the **proxy** (`7331`) for live reload, but at the **real
> server** (`8080`) when you want to watch NATS KV changes surface in realtime —
> the proxy does not forward SSE cleanly.

To play both sides, open a second browser profile or a private window; the two
tabs need separate session cookies.

### Running without live reload

```shell
task run     # builds styles + templates, then runs ./tmp/main
```

Open [`http://localhost:8080`](http://localhost:8080).

### Other tasks

| Task | Does |
| --- | --- |
| `task build` | Compile styles, generate templates, build `tmp/main` |
| `task build:templ` | Regenerate `*_templ.go` from `*.templ` |
| `task build:styles` | Compile `web/styles/styles.css` → `web/static/index.css` |
| `task debug` | Build, then launch [delve](https://github.com/go-delve/delve) on the binary |

The templ tasks set [`TEMPL_EXPERIMENT=rawgo`](https://templ.guide/syntax-and-usage/raw-go/)
for you; you only need it if you invoke `templ generate` by hand.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port (`9001` in the Docker image) |
| `ADMIN_TOKEN` | random per run | Unlocks `/admin` — see [Admin Panel](#admin-panel) |

The embedded NATS server listens on `127.0.0.1:1234` and is not configurable.

## Routes

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/` | Login page (redirects to `/dashboard` if signed in) |
| `POST` | `/api/index/login` | Create user + session |
| `GET` | `/dashboard` | Lobby list |
| `GET` | `/api/dashboard/updates` | **SSE** — lobby list stream |
| `POST` | `/api/dashboard/create` | Create a lobby |
| `POST` | `/api/dashboard/{id}/join` | Join as challenger |
| `DELETE` | `/api/dashboard/{id}/delete` | Delete own lobby |
| `POST` | `/api/dashboard/logout` | Sign out |
| `GET` | `/game/{id}` | Game board |
| `GET` | `/api/game/{id}/updates` | **SSE** — board stream |
| `POST` | `/api/game/{id}/toggle/{cell}` | Play a cell |
| `POST` | `/api/game/{id}/reset` | Reset the board |
| `POST` | `/api/game/{id}/leave` | Leave the game |
| `POST` | `/api/session/touch` | Slide the session cookie from an SSE page |
| `POST` | `/api/theme` | Persist the DaisyUI theme choice to a cookie |
| `GET` | `/admin` | Admin panel |
| `POST` | `/api/admin/login` | Exchange `ADMIN_TOKEN` for access |
| `GET` | `/api/admin/updates` | **SSE** — admin tables stream |
| `DELETE` | `/api/admin/games/{id}`, `/api/admin/users/{id}` | Delete one record |
| `POST` | `/api/admin/purge/{orphaned,finished,older,games,users}` | Bulk cleanup |

## Data model

Four KV buckets, all created in [`routes/router.go`](./routes/router.go). Game data
expires after an hour; presence after 90 seconds.

| Bucket | Key | Value |
| --- | --- | --- |
| `users` | session id | `{"name":…,"session_id":…}` |
| `gameLobbies` | game id | `{"id":…,"name":…,"host_id":…,"host_name":…,"challenger_id":…}` |
| `gameBoards` | game id | `{"id":…,"board":[9 strings],"turn":bool,"winner":…}` |
| `presence` | session id | RFC3339 timestamp (TTL is the whole mechanism) |

`host_name` is denormalised into the lobby so a card still renders a name after the
host's user record expires.

### Inspecting NATS

Install the [nats-cli](https://github.com/nats-io/natscli) and point it at the
embedded server:

```shell
export NATS_URL=nats://127.0.0.1:1234

nats kv ls                            # list buckets
nats kv ls gameLobbies                # list lobby ids
nats kv get --raw gameBoards [id]     # read a board

# force a win to watch both browsers update
nats kv put gameBoards [id] '{"id":"[id]","board":["X","X","X","","","","","",""],"turn":false,"winner":"X"}'
```

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

## Themes

The nav carries a picker for all 32 DaisyUI themes; each row previews itself,
since the swatch carries its own `data-theme` and DaisyUI scopes its colour
variables by that attribute.

The choice is split between the two halves that are each good at one thing.
Clicking a theme assigns a Datastar signal, and `data-attr` on `<html>` repaints
from it immediately - no round trip. The same click `@post`s to `/api/theme`,
which only writes a year-long cookie. Middleware reads that cookie back on every
request so `<html data-theme>` is already correct in the server's response: the
theme survives navigation and restarts with no flash of the default on load,
which is what a client-side store applying the theme after load would give you.

`components.Themes` in [`web/components/theme.go`](./web/components/theme.go) is
the list, and it has to match the `daisyui.themes` array in `tailwind.config.js`
- a theme absent from that array has no variables in the bundle and would render
an uncoloured page. Both the cookie and the posted signal are checked against
that list before being rendered.

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

## Building

`task build` assembles the binary with [static assets embedded](./static_prod.go),
so the result is a single self-contained file — no NATS to run alongside it and
no assets to ship. Development builds use the `dev` tag to serve `web/static`
from disk instead ([`static_dev.go`](./static_dev.go)).

The [Dockerfile](./Dockerfile) does the same in three stages — Tailwind in Node,
the Go binary compressed with `upx`, then a `scratch` image holding only the
binary. It listens on `9001`.

```shell
docker build -t tictactoe:latest .
docker run --name tictactoe -p 8080:9001 tictactoe:latest
```

> [!NOTE]
> State lives in the process. Two instances each get their own embedded NATS and
> cannot see each other's games, so this runs as a single process — which is all
> it is meant to do. It is a project to read and tinker with, not to deploy.

## IDE Support

- [Templ / TailwindCSS support](https://templ.guide/commands-and-tools/ide-support)
- VS Code: [launch.json](./.vscode/launch.json) ships a `Debug Main` configuration,
  plus [settings.json](./.vscode/settings.json) and
  [recommended extensions](./.vscode/extensions.json)

## Contributing

Completely open to PRs and feature requests.

## References

- [Datastar docs](https://data-star.dev/)
- [NATS KV](https://docs.nats.io/nats-concepts/jetstream/key-value-store)
- [templ](https://templ.guide/)
