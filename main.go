package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/erik-petrov/dj_sanya_go/bot"
)

// bot params
var (
	GuildID  = "1009396276661583912" //472357061267816468
	BotToken = os.Getenv("DISCORD_CRASH_TOKEN")
)

func main() {
	boot := bot.Boot{
		GuildID: GuildID,
		Token: BotToken,
	}

	bot, err := bot.New(boot)
	if err != nil{
		log.Fatal(err)
	}

	log.Println("Adding commands...")

	err = bot.Start()
	if err != nil{
		log.Fatal(err)
	}
	
	defer bot.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	log.Println("Gracefully shutting down.")
}
