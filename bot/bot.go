package bot

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	s         *discordgo.Session
	ears      *discordgo.Session // second session used only to receive voice
	guildID   string
	earsToken string
	ytToken   string
	sfToken   string
	sfSecret  string
}

type Boot struct {
	GuildID   string
	Token     string
	EarsToken string
	YtToken   string
	SfToken   string
	SfSecret  string
}

func New(boot Boot) (*Bot, error) {
	s, err := discordgo.New("Bot " + boot.Token)
	if err != nil {
		return nil, err
	}
	// Lavalink needs the voice state/server gateway events and a cached voice
	// state so we can find which channel the requesting user is in.
	s.Identify.Intents |= discordgo.IntentsGuildVoiceStates
	// The play-hook and the message filter read message text, which needs the
	// privileged Message Content intent (plus Guild Messages). Only request them
	// when one is enabled — asking for a privileged intent that isn't enabled in
	// the Developer Portal makes the gateway reject our login.
	if hookEnabled() || filterEnabled() {
		s.Identify.Intents |= discordgo.IntentsGuildMessages | discordgo.IntentMessageContent
	}
	s.State.TrackVoice = true
	return &Bot{s: s, guildID: boot.GuildID, earsToken: boot.EarsToken, ytToken: boot.YtToken, sfToken: boot.SfToken, sfSecret: boot.SfSecret}, nil
}

func (b *Bot) Start() error {
	loadBans()
	loadMusicChannels()
	if err := b.s.Open(); err != nil {
		return err
	}
	b.setupLavalink()
	b.setupPoToken()
	b.setupEars()
	b.setupCommands()
	b.setupHook()
	b.setupWeb()
	b.setupKick()
	b.setupFilter()
	b.sirusParsing()
	return nil
}

func (b *Bot) Close() {
	if b.ears != nil {
		if err := b.ears.Close(); err != nil {
			log.Println("Error closing ears session:", err)
		}
	}
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

// GetUsersInVoice returns how many users are currently in the given voice channel.
func (b *Bot) GetUsersInVoice(chn *discordgo.Channel) int {
	if chn == nil {
		return -1
	}
	gd, err := b.s.State.Guild(chn.GuildID)
	if err != nil {
		log.Println(err)
		return -1
	}
	amount := 0
	for _, h := range gd.VoiceStates {
		if h.ChannelID == chn.ID {
			amount++
		}
	}
	return amount
}

func (b *Bot) debug(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ch, _ := b.s.Channel(i.ChannelID)
	log.Println(b.GetUsersInVoice(ch))
}

func (b *Bot) queue(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if LavalinkClient == nil {
		respond(s, i, "Music is not available right now.")
		return
	}

	respond(s, i, queueText(i.GuildID))
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
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "deep",
					Description: "choose the track from the top-10 search results",
					Required:    false,
				},
			},
		},
		{
			Name:        "skip",
			Description: "Skips the currently playing song for a next one",
		},
		{
			Name:        "seek",
			Description: "Seek within the current track: 1:23, 90, +30, -15",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "time",
					Description: "Position (1:23 / 90) or offset (+30 / -15)",
					Required:    true,
				},
			},
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
		{
			Name:        "listen",
			Description: "Make the bot join your voice channel and listen for voice commands.",
		},
		{
			Name:        "unlisten",
			Description: "Stop listening and leave the voice channel.",
		},
		{
			Name:                     "ban",
			Description:              "Ban a user from using the bot (owner only).",
			DefaultMemberPermissions: &adminOnlyPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "user",
					Description: "User to ban",
					Required:    true,
				},
			},
		},
		{
			Name:                     "unban",
			Description:              "Unban a user (owner only).",
			DefaultMemberPermissions: &adminOnlyPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "user",
					Description: "User to unban",
					Required:    true,
				},
			},
		},
		{
			Name:                     "set-music-channel",
			Description:              "Set where now-playing and status messages are posted.",
			DefaultMemberPermissions: &manageMessagesPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionChannel,
					Name:        "channel",
					Description: "Music / announce channel",
					Required:    true,
				},
			},
		},
	}

	commandHandlers := map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"stop": b.onStop,

		"repeat": b.onRepeat,

		"pause": b.onPause,

		"play": b.onPlay,

		"skip": b.onSkip,

		"seek": b.onSeek,

		"wakeup": b.wakeUp,

		"debug": b.debug,

		"queue": b.queue,

		"listen": b.onListen,

		"unlisten": b.onUnlisten,

		"ban": b.onBan,

		"unban": b.onUnban,

		"set-music-channel": b.onSetMusicChannel,
	}

	b.s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		// Banned users can't use anything — commands or buttons.
		if uid := interactionUserID(i); uid != "" && isBanned(uid) {
			denyBanned(s, i)
			return
		}
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
				h(s, i)
			}
		case discordgo.InteractionMessageComponent:
			b.onComponent(s, i)
		}
	})

	b.s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
	})

	// Leave voice automatically once no humans remain in the bot's channel.
	b.s.AddHandler(b.onUserVoiceUpdate)

	if b.guildID == "" {
		log.Println("registering global slash commands (visible in every server; can take up to ~1h to propagate)")
	} else {
		log.Println("registering slash commands to guild", b.guildID)
	}

	// Merge in the message-filter commands when the filter is enabled; the
	// interaction router above shares the same commandHandlers map.
	if filterEnabled() {
		fcmds, fhandlers := b.filterCommands()
		commands = append(commands, fcmds...)
		for name, h := range fhandlers {
			commandHandlers[name] = h
		}
	}

	// Bulk-overwrite registers exactly this set (and prunes any stale commands)
	// in a single call. With an empty guildID it targets the global scope, so
	// the commands show up in every server the bot is in. Non-fatal: a transient
	// failure shouldn't take the whole bot down.
	if _, err := b.s.ApplicationCommandBulkOverwrite(b.s.State.User.ID, b.guildID, commands); err != nil {
		log.Println("failed to register slash commands:", err)
	}
}
