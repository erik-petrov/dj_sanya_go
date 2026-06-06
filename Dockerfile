# ---- build stage ----
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
# The discordgo DAVE fork is wired in as a submodule via a go.mod `replace`, so
# it must be present before `go mod download` can resolve the module graph.
COPY third_party/ ./third_party/
RUN go mod download
COPY . .
# CGO disabled => a fully static binary. No libopus/ffmpeg/yt-dlp/python needed:
# Lavalink handles all audio, the bot only sends it commands.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bot .

# ---- run stage ----
# distroless/static ships ca-certificates + tzdata and nothing else (~2 MB base).
FROM gcr.io/distroless/static-debian12
COPY --from=build /bot /bot
ENTRYPOINT ["/bot"]
