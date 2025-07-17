package bot

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	s        *discordgo.Session
	guildID  string
	ytToken  string
	sfToken  string
	sfSecret string
}

type Boot struct {
	GuildID  string
	Token    string
	YtToken  string
	SfToken  string
	SfSecret string
}

func New(boot Boot) (*Bot, error) {
	s, err := discordgo.New("Bot " + boot.Token)
	return &Bot{s: s, guildID: boot.GuildID, ytToken: boot.YtToken, sfToken: boot.SfToken, sfSecret: boot.SfSecret}, err
}

func (b *Bot) Start() error {
	err := b.s.Open()
	b.setupCommands()
	b.sirusParsing()
	return err
}

func (b *Bot) Close() {
	err := b.s.Close()
	if err != nil {
		log.Println("Error closing Discord session:", err)
		return
	}
}

func (b *Bot) sirusParsing() {
	go func() {
		sirusDataChannel := "1340979801250467861"
		lastStatus := true
		for {
			name, status := b.CheckSirusUp()
			var str string
			if status {
				str = "`WoW Sirus " + name + " теперь имеет статус: онлайн! Скорее заходите чтобы получить 1.5х опыта!`"
			} else {
				str = "`WoW Sirus " + name + " теперь имеет статус: оффлайн! Следите за запуском чтобы не пропустить 1.5х опыта!`"
			}

			if lastStatus != status {
				_, err := b.s.ChannelMessageSend(sirusDataChannel, str)
				if err != nil {
					fmt.Println(err)
				}

				lastStatus = status
			}
			time.Sleep(10 * time.Second)
		}
	}()
}

func (b *Bot) debug(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var ch, _ = b.s.Channel(CurrentBotChannel)
	log.Println(b.GetUsersInVoice(ch))
}

func (b *Bot) queue(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var response string

	q := b.GetQueue()

	for i, ytdlpResponse := range q {
		response += strconv.Itoa(i) + ": [" + ytdlpResponse.Title + "](" + ytdlpResponse.WebpageURL + ")\n"
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Current queue: \n" + response,
		},
	})
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
		{
			Name:        "wakeup",
			Description: "Wakes the user up by shuffling them a lot.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "user",
					Description: "User to wake up",
					Required:    true,
				},
			},
		},
		{
			Name:        "debug",
			Description: "whatever i need to debug rn",
		},
		{
			Name:        "queue",
			Description: "Current queue",
		},
	}

	commandHandlers := map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"stop": b.onStop,

		"repeat": b.onRepeat,

		"pause": b.onPause,

		"play": b.onPlay,

		"skip": b.onSkip,

		"wakeup": b.wakeUp,

		"debug": b.debug,

		"queue": b.queue,
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
