# ---- Stage 1: compile the TailwindCSS bundle ----
FROM docker.io/node:22-alpine AS styles

WORKDIR /src

# Pin pnpm to the version that wrote pnpm-lock.yaml, so the `allowBuilds` key in
# pnpm-workspace.yaml is understood and --frozen-lockfile stays reproducible.
RUN corepack enable && corepack prepare pnpm@11.24.0 --activate

# Dependencies first: editing a template shouldn't bust the install cache.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

# tailwind.config.js scans web/**/*.templ and web/**/*.go for class names,
# so the whole web/ tree has to be present before the CSS is generated.
COPY tailwind.config.js postcss.config.js ./
COPY web ./web
RUN pnpm exec tailwindcss -c tailwind.config.js \
      -i web/styles/styles.css \
      -o web/static/index.css \
      --minify

# ---- Stage 2: build the Go binary ----
FROM docker.io/golang:1.26-alpine AS build

RUN apk add --no-cache upx

WORKDIR /src
COPY . ./

# Must land before `go build`: static_prod.go embeds web/static/* into the binary,
# so the CSS is baked in at compile time. web/static/ is .dockerignore'd, which
# means a broken styles stage fails the build here instead of silently shipping
# an unstyled site.
COPY --from=styles /src/web/static/index.css ./web/static/index.css

RUN go mod download
RUN --mount=type=cache,target=/root/.cache/go-build \
go build -ldflags="-s" -o /bin/main .
RUN upx -9 -k /bin/main

FROM scratch
ENV PORT=9001
COPY --from=build /bin/main /
ENTRYPOINT ["/main"]
