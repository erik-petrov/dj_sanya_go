package main

import (
	"github.com/joho/godotenv"
	"log"
	"os"
	"os/signal"

	"github.com/erik-petrov/dj_sanya_go/bot"
)

// bot params
var (
	GuildID = "1493654034203148380" //472357061267816468 1009396276661583912
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

	boot := bot.Boot{
		GuildID:  GuildID,
		Token:    os.Getenv("BOT_TOKEN"),
		YtToken:  os.Getenv("YT_TOKEN"),
		SfToken:  os.Getenv("SPOTIFY_ID"),
		SfSecret: os.Getenv("SPOTIFY_SECRET"),
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
