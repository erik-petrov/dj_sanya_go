package bot

import (
	"errors"
	"log"
	"slices"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) onStop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if LavalinkClient == nil {
		respond(s, i, "Music is not available right now.")
		return
	}
	if err := b.StopLavalink(i.GuildID); err != nil {
		respond(s, i, "Error stopping: "+err.Error())
		return
	}
	respond(s, i, "Stopped track!")
}

func (b *Bot) onPause(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if LavalinkClient == nil {
		respond(s, i, "Music is not available right now.")
		return
	}
	paused := !GetOrCreatePlayer(i.GuildID).Paused()
	if err := b.PauseLavalink(i.GuildID, paused); err != nil {
		respond(s, i, "Error: "+err.Error())
		return
	}
	if paused {
		respond(s, i, "Paused!")
	} else {
		respond(s, i, "Continued!")
	}
}

func (b *Bot) onRepeat(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if LavalinkClient == nil {
		respond(s, i, "Music is not available right now.")
		return
	}
	if b.ToggleRepeat(i.GuildID) {
		respond(s, i, "Repeating currently playing track!")
	} else {
		respond(s, i, "Stopped repeating!")
	}
}

func (b *Bot) onSkip(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if LavalinkClient == nil {
		respond(s, i, "Music is not available right now.")
		return
	}
	if err := b.SkipLavalink(i.GuildID); err != nil {
		respond(s, i, "Error skipping: "+err.Error())
		return
	}
	respond(s, i, "Skipped!")
}

func (b *Bot) wakeUp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var ogVoice string
	var skipVcs []string

	g, _ := s.State.Guild(i.GuildID)
	vs := g.VoiceStates
	target := i.ApplicationCommandData().Options[0].UserValue(s)
	for _, voice := range vs {
		if target.ID == voice.UserID {
			ogVoice = voice.ChannelID
		}

		if !slices.Contains(skipVcs, voice.ChannelID) {
			skipVcs = append(skipVcs, voice.ChannelID)
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Waking up " + target.Mention() + "...",
		},
	})

	chans := g.Channels
	sort.Slice(chans, func(i, j int) bool {
		return chans[i].Position < chans[j].Position
	})

	for j := 0; j < 3; j++ {
		if _, err := findUserVoiceState(target.ID, g); err != nil {
			log.Println(err)
			return
		}

		for _, voice := range chans {
			if slices.Contains(skipVcs, voice.ID) {
				continue
			}

			if voice.Type != discordgo.ChannelTypeGuildVoice {
				continue
			}

			perms, err := s.State.UserChannelPermissions(s.State.User.ID, voice.ID)
			if err != nil {
				log.Println(err)
				break
			}

			if perms&discordgo.PermissionVoiceMoveMembers != discordgo.PermissionVoiceMoveMembers {
				continue
			}

			if ogVoice == voice.ID {
				continue
			}

			if voice.MemberCount != 0 {
				continue
			}

			err = s.GuildMemberMove(g.ID, target.ID, &voice.ID)
			if err != nil {
				log.Println(err)
				break
			}

			time.Sleep(300 * time.Millisecond)
		}

	}
	s.GuildMemberMove(g.ID, target.ID, &ogVoice)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Done!",
		},
	})
}

func editInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &msg,
	})
}

func findUserVoiceState(userid string, guild *discordgo.Guild) (*discordgo.VoiceState, error) {
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userid {
			return vs, nil
		}
	}
	return nil, errors.New("could not find user's voice state")
}
