package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type YTResponse struct {
	Results []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`
	} `json:"items"`
}

func (b *Bot) onPlay(s *discordgo.Session, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Loading..",
		},
	})
	link := i.ApplicationCommandData().Options[0].StringValue()
	if !checkSubstrings(link, "youtu.be", "youtube") {
		searchURL := "https://www.googleapis.com/youtube/v3/search?part=snippet&maxResults=1&q=" + link + "&type=video&key=" + b.ytToken
		res, err := http.DefaultClient.Get(searchURL)
		if err != nil {
			fmt.Printf("couldnt make the search request: %s\n", err)
			return
		}
		resBody, err := io.ReadAll(res.Body)
		if err != nil {
			fmt.Printf("could not read response body: %s\n", err)
			return
		}
		var response YTResponse
		err = json.Unmarshal([]byte(resBody), &response)
		if err != nil {
			fmt.Printf("could not parse response: %s\n", err)
			return
		}
		link = "https://youtube.com/watch?v=" + response.Results[0].ID.VideoID
	}
	song := b.playSong(link)
	content := "Играю: `" + song.Title + "` \nС длительностью в " + fmt.Sprint(song.Duration) + " секунд"
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
	if err != nil {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Something went wrong: " + err.Error(),
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
			err := b.startPlaying(s, song, g.ID, vs.ChannelID)
			if err != nil {
				log.Println("Error playing sound:", err)
			}

			return
		}
	}
}

func (b *Bot) onStop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.stop()
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Stopped track!",
		},
	})
}

func (b *Bot) onPause(s *discordgo.Session, i *discordgo.InteractionCreate) {
	paused, err := b.togglePause()
	text := ""
	if err != nil {
		text = "Music isn't playing!"
	} else {
		if paused {
			text = "Paused!"
		} else {
			text = "Continued!"
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: text,
		},
	})
}

func (b *Bot) onRepeat(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ans, err := b.toggleRepeat()
	content := ""

	if err != nil {
		content = "No music playing at this moment."
	} else {
		if ans {
			content = "Repeating currently playing track!"
		} else {
			content = "Stopped repeating!"
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
}

func checkSubstrings(str string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(str, sub) {
			return true
		}
	}
	return false
}
