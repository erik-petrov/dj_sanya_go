package bot

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	s       *discordgo.Session
	guildID string
	ytToken string
}

type Boot struct {
	GuildID  string
	Token    string
	YT_Token string
}

func New(boot Boot) (*Bot, error) {
	s, err := discordgo.New("Bot " + boot.Token)
	return &Bot{s: s, guildID: boot.GuildID, ytToken: boot.YT_Token}, err
}

func (b *Bot) Start() error {
	err := b.s.Open()
	b.setupCommands()
	b.sirusParsing()
	return err
}

func (b *Bot) Close() {
	b.s.Close()
}

func (b *Bot) sirusParsing() {
	go func() {
		sirusDataChannel := "1340979801250467861"
		lastStatus := false
		for {
			name, status := b.CheckSirusUp()
			var str string
			if status {
				str = "`WoW Sirus " + name + " теперь имеет статус: онлайн! Скорее заходите чтобы получить 1.5х опыта!`"
			} else {
				str = "`WoW Sirus " + name + " теперь имеет статус: оффлайн! Следите за запуском чтобыы не пропустить 1.5х опыта!`"
			}

			if lastStatus != status {
				_, err := b.s.ChannelMessageSend(sirusDataChannel, str)
				if err != nil {
					fmt.Println(err)
				}

				lastStatus = status
			}
			time.Sleep(1 * time.Minute)
		}
	}()
}

func (b *Bot) setupCommands() {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "pause",
			Description: "Pauses currently playing music",
		},
		{
			Name:        "repeat",
			Description: "Repeats the currently playing track.",
		},
		{
			Name:        "stop",
			Description: "Stops the bot from playing.",
		},
		{
			Name:        "play",
			Description: "Plays music.",
			Options: []*discordgo.ApplicationCommandOption{

				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "query",
					Description: "music name/yt link",
					Required:    false,
				},
				{
					Type:        discordgo.ApplicationCommandOptionAttachment,
					Name:        "file",
					Description: "music file",
					Required:    false,
				},
			},
		},
		{
			Name:        "skip",
			Description: "Skips the currently playing song for a next one",
		},
	}

	commandHandlers := map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"stop": b.onStop,

		"repeat": b.onRepeat,

		"pause": b.onPause,

		"play": b.onPlay,

		"skip": b.onSkip,
	}

	b.s.AddHandler(b.HandleVoiceStateUpdate)

	b.s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})

	b.s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
	})

	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := b.s.ApplicationCommandCreate(b.s.State.User.ID, b.guildID, v)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.Name, err)
		}
		registeredCommands[i] = cmd
	}
}
