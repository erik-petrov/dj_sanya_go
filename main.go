package main

import (
	"github.com/joho/godotenv"
	"log"
	"os"
	"os/signal"

	"github.com/erik-petrov/dj_sanya_go/bot"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, relying on environment variables")
	}

	token := os.Getenv("BOT_TOKEN")

	if token == "" {
		log.Fatal("no token")
	}

	// GUILD_ID empty => register commands globally (every server the bot is in).
	// Set it (e.g. for local dev) to register instantly to a single guild.
	boot := bot.Boot{
		GuildID:   os.Getenv("GUILD_ID"),
		Token:     os.Getenv("BOT_TOKEN"),
		EarsToken: os.Getenv("EARS_BOT_TOKEN"),
		YtToken:   os.Getenv("YT_TOKEN"),
		SfToken:   os.Getenv("SPOTIFY_ID"),
		SfSecret:  os.Getenv("SPOTIFY_SECRET"),
	}

	bot, err := bot.New(boot)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Adding commands...")

	err = bot.Start()
	if err != nil {
		log.Fatal(err)
	}

	defer bot.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	log.Println("Gracefully shutting down.")
}
