# web

Everything the browser is served: templates, styles, and the compiled CSS.

There is no client-side JavaScript in this project. Datastar is loaded from a CDN
in [`layouts/base.templ`](./layouts/base.templ) and drives the UI entirely through
`data-*` attributes on server-rendered markup, so there is no bundler step and no
JS/TS source tree to maintain.

> [!WARNING]
> If any of these paths change, update `tailwind.config.js`, the `Taskfile.yml`
> tasks, and the `COPY` lines in the `Dockerfile` to match.

## Organization

| Directory | Holds |
| --- | --- |
| `components/` | Reusable fragments composed into pages, and the Go helpers and types backing them (`utils.go`, `session.go`, `admin_helpers.go`) |
| `layouts/` | Page shells — `base.templ` plus the `LoggedIn` / `LoggedOut` wrappers |
| `pages/` | One template per route: index, dashboard, game, admin |
| `styles/` | `styles.css`, the Tailwind entrypoint |
| `static/` | Build output — `index.css`, gitignored and embedded into the binary by [`static_prod.go`](../static_prod.go) |

## Generated files

`*_templ.go` files are generated from their `.templ` source by `task build:templ`;
edit the `.templ` and regenerate rather than editing them directly.

While `task live` is running, templ rewrites these into watch mode — string
literals are replaced with `templ.WriteWatchModeString` calls that read from
gitignored `*_templ.txt` sidecars. That form is for hot reload only and must not
be committed; run `task build:templ` after stopping the watcher to restore them.
