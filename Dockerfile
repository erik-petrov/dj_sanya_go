# syntax=docker/dockerfile:1
# ---- build stage ----
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
# The discordgo DAVE fork is wired in as a submodule via a go.mod `replace`, so
# it must be present before `go mod download` can resolve the module graph.
COPY third_party/ ./third_party/
# Persist the module cache across builds (BuildKit cache mount) so unchanged
# dependencies are never re-downloaded.
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
# CGO disabled => a fully static binary. No libopus/ffmpeg/yt-dlp/python needed:
# Lavalink handles all audio, the bot only sends it commands.
# The go-build cache mount makes a code change recompile only the packages that
# actually changed instead of the whole dependency graph (seconds, not minutes).
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /bot .

# ---- run stage ----
# distroless/static ships ca-certificates + tzdata and nothing else (~2 MB base).
FROM gcr.io/distroless/static-debian12
COPY --from=build /bot /bot
ENTRYPOINT ["/bot"]
