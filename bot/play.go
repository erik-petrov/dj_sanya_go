package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)

var (
	CurrentBotChannel string
	wg                sync.WaitGroup
)

type YTResponse struct {
	Results []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`

		Snippet struct {
			Title string `jsin:"title"`
		}
	} `json:"items"`
}

func (b *Bot) onPlay(s *discordgo.Session, i *discordgo.InteractionCreate) {
	CurrentBotChannel = i.ChannelID

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Loading..",
		},
	})

	title := ""
	link := ""
	attachment := false
	if len(i.ApplicationCommandData().Options) == 0 {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "No music data given",
		})
		return
	}
	switch i.ApplicationCommandData().Options[0].Type {
	case 3: //string
		link = i.ApplicationCommandData().Options[0].StringValue()
		if !checkSubstrings(link, "youtu.be", "youtube", "soundcloud") {
			songlink, err := getLinkTitle(link, b.ytToken, s, i)
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Something went wrong: " + err.Error(),
				})
				return
			}
			link = songlink
		}
	case 11: //attachment
		attachmentID := i.ApplicationCommandData().Options[0].Value.(string)
		file := i.ApplicationCommandData().Resolved.Attachments[attachmentID]
		link = file.URL
		title = file.Filename
		attachment = true
	}

	songCh := make(chan yt_dlpResponse)
	errCh := make(chan error)
	wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.playSong(link, attachment, songCh, errCh)
	}()

	song := <-songCh
	err := <-errCh

	if err != nil {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Something went wrong: " + err.Error(),
		})
		return
	}

	if attachment {
		song.Title = title
	}

	if b.IsPlaying() {
		b.AddToQueue(song)
		editInteraction(s, i, "Added `"+song.Title+"` to queue!")
		return
	}

	content := ""
	if !attachment {
		content = "Играю: `" + song.Title + "`\nДлительностью " + song.Duration
	} else {
		content = "Играю: `" + song.Title + "`"
	}

	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
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
			rawURL := ""

			if len(song.RequestedDownloads[0].RequestedFormats) == 0 {
				rawURL = song.FallbackURL
			} else {
				log.Println(song.RequestedDownloads)
				rawURL = song.RequestedDownloads[0].RequestedFormats[1].URL
			}

			rawURL, err := b.downloadVideo(rawURL)
			if err != nil {
				log.Println("Error downloading sound:", err)
				return
			}

			err = b.startPlaying(s, rawURL, g.ID, vs.ChannelID)
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

func (b *Bot) onSkip(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.skip()
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Skipped!",
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

func editInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &msg,
	})
}

func getLinkTitle(link string, token string, s *discordgo.Session, i *discordgo.InteractionCreate) (ytlink string, err error) {
	searchURL := "https://www.googleapis.com/youtube/v3/search?part=snippet&maxResults=1&q=" + url.QueryEscape(link) + "&type=video&key=" + token
	res, err := http.DefaultClient.Get(searchURL)
	if err != nil {
		fmt.Printf("couldnt make the search request: %s\n", err)
		return "", err
	}
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("could not read response body: %s\n", err)
		return "", err
	}
	var response YTResponse
	err = json.Unmarshal([]byte(resBody), &response)
	if err != nil {
		fmt.Printf("could not parse response: %s.\n body: %s\n request: %s\n", err, resBody, searchURL)
		return "", err
	}
	if len(response.Results) == 0 {
		content := "Song not found"
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
		})
		return "", err
	}
	ytlink = "https://youtube.com/watch?v=" + response.Results[0].ID.VideoID
	return ytlink, nil
}
