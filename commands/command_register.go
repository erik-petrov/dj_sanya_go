package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/erik-petrov/dj_sanya_go/music"
)

var (
	Commands = []*discordgo.ApplicationCommand{
		{
			Name:        "test",
			Description: "Test command",
		},
		{
			Name:        "music-data-test",
			Description: "Literally just tests the go capability",
			Options: []*discordgo.ApplicationCommandOption{

				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "music-link",
					Description: "yt link to the music",
					Required:    true,
				},
			},
		},
	}

	CommandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"test": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "nice cock",
				},
			})
		},

		"music-data-test": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Loading..",
				},
			})
			link := i.ApplicationCommandData().Options[0]
			song := music.PlaySong(link.StringValue())
			if len(song.Buffer) == 0 {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Unable to download the song",
				})
				return
			}
			content := song.Title + " " + string(int(song.Duration))
			_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Something went wrong",
				})
				return
			}

			// Find the guild for that channel.
			g, err := s.State.Guild(i.GuildID)
			if err != nil {
				// Could not find guild.
				return
			}

			for _, vs := range g.VoiceStates {
				if vs.UserID == i.Member.User.ID {
					music.StartPlaying(s, song, g.ID, vs.ChannelID)
					if err != nil {
						fmt.Println("Error playing sound:", err)
					}

					return
				}
			}
		},
	}
)
