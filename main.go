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
	godotenv.Load()

	token := os.Getenv("BOT_TOKEN")

	if token == "" {
		log.Fatal("no token")
	}

	boot := bot.Boot{
		GuildID:  GuildID,
		Token:    os.Getenv("BOT_TOKEN"),
		YT_Token: os.Getenv("YT_TOKEN"),
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
