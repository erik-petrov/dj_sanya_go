package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/erik-petrov/dj_sanya_go/bot"
	"github.com/joho/godotenv"
)

// bot params
var (
	GuildID = "1009396276661583912" //472357061267816468
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file")
		return
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
