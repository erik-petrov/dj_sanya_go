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
			Title string `json:"title"`
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

func castYTDLPResponse(data interface{}) (interface{}, error) {
	// Marshal the interface{} back to JSON
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	// Unmarshal into a map to inspect structure
	var raw map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, err
	}

	// Check for the "entries" field to determine if it's a playlist
	if _, ok := raw["entries"]; ok {
		var playlist YTDLPResponsePlaylist
		if err := json.Unmarshal(jsonBytes, &playlist); err != nil {
			return nil, err
		}
		return playlist, nil
	}

	// Otherwise, assume it's a single video
	var single YTDLPResponse
	if err := json.Unmarshal(jsonBytes, &single); err != nil {
		return nil, err
	}
	return single, nil
}

func (b *Bot) onPlay(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var isPlaylist bool
	CurrentBotChannel = i.ChannelID

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Loading...",
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
		var songlink string

		if !checkSubstrings(link, "youtu.be", "youtube", "soundcloud", "open.spotify", "spotify.com") {
			songlink, err = getLinkTitle(link, b.ytToken, s, i)
		} else if checkSubstrings(link, "open.spotify", "spotify.com") {
			var song SpotifySong
			song, err = getSpotifyLinkName(link)
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Something went wrong: " + err.Error(),
				})
				log.Println(err.Error())
				return
			}

			songlink, err = getLinkTitle(song.Name+" "+song.Artist[0].Name+" lyrics", b.ytToken, s, i)
		} else {
			if checkSubstrings(link, "&list=") {
				isPlaylist = true
			}
			songlink = link
		}

		if err != nil {
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: "Something went wrong: " + err.Error(),
			})
			log.Println(err.Error())
			return
		}

		link = songlink

	case 11: //attachment
		attachmentID := i.ApplicationCommandData().Options[0].Value.(string)
		file := i.ApplicationCommandData().Resolved.Attachments[attachmentID]
		link = file.URL
		title = file.Filename
		attachment = true
	}

	songCh := make(chan interface{})
	errCh := make(chan error)
	wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.playSong(link, attachment, songCh, errCh, isPlaylist)
	}()

	song := <-songCh
	err := <-errCh

	if err != nil {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Something went wrong: " + err.Error(),
		})
		log.Println(err.Error())
		return
	}

	song, err = castYTDLPResponse(song)
	if err != nil {
		log.Println("Something went wrong during casting: " + err.Error())
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Something went wrong during casting: " + err.Error(),
		})
	}

	content := ""
	switch song.(type) {
	case YTDLPResponse:
		if attachment {
			bar, ok := song.(YTDLPResponse)
			if ok {
				bar.Title = title
			}
			content = "Играю: `" + bar.Title + "`"
		} else {
			bar, ok := song.(YTDLPResponse)
			if !ok {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Something went wrong: " + "type assertion failed",
				})
				return
			}

			if b.IsPlaying() {
				b.AddToQueue(bar)
				editInteraction(s, i, "Added `"+bar.Title+"` to queue!")
				return
			}

			content = "Играю: `" + bar.Title + "`\nДлительностью " + bar.Duration
			song = bar
		}
	case YTDLPResponsePlaylist:
		bar, ok := song.(YTDLPResponsePlaylist)

		if !ok {
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: "Something went wrong: " + "type assertion failed",
			})
			return
		}

		if b.IsPlaying() { //add instantly everything to queue
			for _, el := range bar.Entries {
				b.AddToQueue(el)
			}
			content = "Добавил плейлист в очередь"
		} else {
			toPlay := bar.Entries[0]
			for i := 1; i < len(bar.Entries); i++ {
				b.AddToQueue(bar.Entries[i])
			}
			content = "Играю: `" + toPlay.Title + "`\nДлительностью " + toPlay.Duration
			song = toPlay
		}
	default:
		log.Println("Something went wrong with type assertion")
		err = errors.New("something went wrong with type assertion")
	}

	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})

	if err != nil {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Something went wrong: " + err.Error(),
		})
		log.Println(err.Error())
		return
	}

	if b.IsPlaying() {
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
			var songOk YTDLPResponse
			var ok bool
			if songOk, ok = song.(YTDLPResponse); ok {
				rawURL := ""

				if len(songOk.RequestedDownloads[0].RequestedFormats) == 0 {
					rawURL = songOk.FallbackURL
				} else {
					rawURL = songOk.RequestedDownloads[0].RequestedFormats[1].URL
				}

				rawURL, err := b.downloadVideo(rawURL)
				if err != nil {
					log.Println("Error downloading sound:", err)
					s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
						Content: "Something went wrong: " + err.Error(),
					})
					return
				}

				err = b.startPlaying(s, rawURL, g.ID, vs.ChannelID)
				if err != nil {
					log.Println("Error playing sound:", err)
					s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
						Content: "Something went wrong: " + err.Error(),
					})
					return
				}
			} else {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Something went wrong: " + "type assertion failed when playing",
				})
				return
			}
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

func getLinkTitle(link string, token string, s *discordgo.Session, i *discordgo.InteractionCreate) (ytlink string, err error) {
	searchURL := "https://www.googleapis.com/youtube/v3/search?part=snippet&maxResults=1&q=" + url.QueryEscape(link) + "&type=video&key=" + token
	res, err := http.DefaultClient.Get(searchURL)
	if err != nil {
		log.Printf("couldnt make the search request: %s\n", err)
		return "", err
	}
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("could not read response body: %s\n", err)
		return "", err
	}
	var response YTResponse
	err = json.Unmarshal([]byte(resBody), &response)
	if err != nil {
		log.Printf("could not parse response: %s.\n body: %s\n request: %s\n", err, resBody, searchURL)
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
