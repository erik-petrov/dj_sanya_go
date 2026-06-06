module github.com/erik-petrov/dj_sanya_go

go 1.23.0

toolchain go1.24.3

require (
	github.com/bwmarrin/discordgo v0.29.0
	github.com/disgoorg/disgolink/v3 v3.1.0
	github.com/disgoorg/snowflake/v2 v2.0.3
	github.com/joho/godotenv v1.5.1
)

require (
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/disgoorg/json v1.2.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
)

replace github.com/bwmarrin/discordgo => ./third_party/discordgo
