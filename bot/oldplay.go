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
	"time"

	"github.com/bwmarrin/discordgo"
)

var spotifyBearer string

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
	queue := LavalinkQueues.Get(i.GuildID)
	if queue.Type == QueueTypeRepeatTrack {
		queue.Type = QueueTypeNormal
		respond(s, i, "Stopped repeating!")
	} else {
		queue.Type = QueueTypeRepeatTrack
		respond(s, i, "Repeating currently playing track!")
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
	err = json.Unmarshal(resBody, &sp)

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
