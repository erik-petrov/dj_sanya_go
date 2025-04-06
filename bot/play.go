package bot

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

var (
	CurrentBotChannel string
	wg                sync.WaitGroup
	spotifyBearer     string
)

type YTResponse struct {
	Results []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`

		Snippet struct {
			Title      string `json:"title"`
			Thumbnails struct {
				Thumbnail struct {
					URL string `json:"url"`
				} `json:"high"`
			} `json:"thumbnails"`
			Channel string `json:"channelTitle"`
		}
	} `json:"items"`
}

type SpotifyResponse struct {
	Token string `json:"access_token"`
}

type Artist struct {
	Link string `json:"href"`
	Name string `json:"name"`
}

type Album struct {
	Image []struct {
		URL string `json:"url"`
	} `json:"images"`
	Name string `json:"name"`
	URL  string `json:"href"`
	Date string `json:"release_date"`
}

type SpotifySong struct {
	Artist []Artist `json:"artists"`
	Name   string   `json:"name"`
	Album  Album    `json:"album"`
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

		var err error
		var songlink YTResponse

		if checkSubstrings(link, "open.spotify", "spotify.com") {
			var song SpotifySong
			song, err = getSpotifyLinkName(link)
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Something went wrong: " + err.Error(),
				})
				return
			}

			songlink, err = getLinkTitle(song.Name+" "+song.Artist[0].Name+" lyrics", b.ytToken, s, i)
		}

		if err != nil {
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: "Something went wrong: " + err.Error(),
			})
			return
		}

	case 11: //attachment
		attachmentID := i.ApplicationCommandData().Options[0].Value.(string)
		file := i.ApplicationCommandData().Resolved.Attachments[attachmentID]
		link = file.URL
		title = file.Filename
		attachment = true
	}

	if attachment {
		b.playMusic(s, i, link, attachment, title)
	}
	//make a list with top 10 songs from search

}

func (b *Bot) playMusic(s *discordgo.Session, i *discordgo.InteractionCreate, link string, attachment bool, attachmentTitle string) {
	songCh := make(chan YTDLPResponse)
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
		song.Title = attachmentTitle
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

func getLinkTitle(link string, token string, s *discordgo.Session, i *discordgo.InteractionCreate) (queryRes YTResponse, err error) {
	searchURL := "https://www.googleapis.com/youtube/v3/search?part=snippet&maxResults=6&q=" + url.QueryEscape(link) + "&type=video&key=" + token
	res, err := http.DefaultClient.Get(searchURL)
	if err != nil {
		log.Printf("couldnt make the search request: %s\n", err)
		return YTResponse{}, err
	}
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("could not read response body: %s\n", err)
		return YTResponse{}, err
	}
	var response YTResponse
	err = json.Unmarshal([]byte(resBody), &response)
	if err != nil {
		log.Printf("could not parse response: %s.\n body: %s\n request: %s\n", err, resBody, searchURL)
		return YTResponse{}, err
	}
	if len(response.Results) == 0 {
		content := "Song not found"
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
		})
		return YTResponse{}, err
	}
	return response, nil
}

func findUserVoiceState(userid string, guild *discordgo.Guild) (*discordgo.VoiceState, error) {
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userid {
			return vs, nil
		}
	}
	return nil, errors.New("could not find user's voice state")
}

func getSpotifyLinkName(link string) (SpotifySong, error) {
	spotifyBearer, err := getSpotifyBearer()

	if err != nil {
		return SpotifySong{}, err
	}

	id := strings.Split(link[strings.LastIndex(link, "/")+1:], "?")[0]

	h := http.Header{}
	h.Add("Authorization", "Bearer "+spotifyBearer)

	url, _ := url.Parse("https://api.spotify.com/v1/tracks/" + id)

	req := http.Request{
		URL:    url,
		Method: http.MethodGet,
		Header: h,
	}

	res, err := http.DefaultClient.Do(&req)

	if err != nil {
		log.Println("Error while making HTTP request for spotify song: ", err)
		return SpotifySong{}, err
	}

	resBody, err := io.ReadAll(res.Body)

	if err != nil {
		log.Println("Error while making taking body out of the spotify song request: ", err)
		return SpotifySong{}, err
	}

	var sp SpotifySong
	err = json.Unmarshal([]byte(resBody), &sp)

	if err != nil {
		log.Println("Error while unmarshaling spotify song data: ", err)
		return SpotifySong{}, err
	}

	return sp, nil
}

func getSpotifyBearer() (string, error) {
	if !tokenExpired() {
		return spotifyBearer, nil
	}

	values := url.Values{}
	values.Add("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", "https://accounts.spotify.com/api/token", bytes.NewBufferString(values.Encode()))
	if err != nil {
		log.Println("error making a request: ", err)
		return "", err
	}
	req.Header.Add("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(os.Getenv("SPOTIFY_ID")+":"+os.Getenv("SPOTIFY_SECRET"))))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("error getting spotify bearer link: ", err)
		return "", err
	}

	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		return "", errors.New(string(body))
	}

	var response SpotifyResponse
	body, _ := io.ReadAll(res.Body)
	json.Unmarshal(body, &response)
	return response.Token, nil
}

func tokenExpired() bool {
	h := http.Header{}
	h.Add("Authorization", "Bearer "+spotifyBearer)

	cl := &http.Client{}
	url, _ := url.Parse("https://api.spotify.com/v1/search?q=+skibidi&type=track")
	req := &http.Request{
		Header: h,
		Method: http.MethodGet,
		URL:    url,
	}

	res, err := cl.Do(req)

	if err != nil {
		log.Println("error checking link expiry")
		return true
	}

	return res.StatusCode != 200
}
