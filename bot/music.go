package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/erik-petrov/dj_sanya_go/dca"
	"github.com/erik-petrov/dj_sanya_go/stream"
)

var (
	AfkTime                = 5 * time.Minute
	MusicStream            = new(stream.StreamingSession)
	ErrBotStandy           = errors.New("Bot is on standy")
	Playing                = false
	Queue                  = make([]yt_dlpResponse, 0)
	CurrentVoiceConnection = new(discordgo.VoiceConnection)
	Timers                 sync.Map
)

type yt_dlpResponse struct {
	Title              string               `json:"title"`
	WebpageURL         string               `json:"website_url"`
	Duration           string               `json:"duration_string"`
	RequestedDownloads []RequestedDownloads `json:"requested_downloads"`
	FallbackURL        string               `json:"url"`
}

type RequestedDownloads struct {
	RequestedFormats []RequestedFormats `json:"requested_formats"`
}

type RequestedFormats struct {
	URL string `json:"url"`
}

var (
	repeat   = false
	afk      = false
	starting = false
)

func encodeFile(song string) (ses *dca.EncodeSession, err error) {
	options := dca.StdEncodeOptions
	options.RawOutput = false
	options.Bitrate = 96
	options.Application = "lowdelay"

	ses, err = dca.EncodeFile(song, options)
	if err != nil {
		return
	}
	return
}

func (b *Bot) HandleVoiceStateUpdate(s *discordgo.Session, i *discordgo.VoiceStateUpdate) {
	if _, ok := Timers.Load(CurrentBotChannel); ok {
		return
	}

	//if not in channels
	if CurrentBotChannel == "" || CurrentVoiceConnection == nil {
		return
	}

	timer := time.NewTimer(AfkTime)
	Timers.Store(CurrentBotChannel, timer)

	go func() {
		for {
			select {
			case <-timer.C:
				CurrentVoiceConnection.Disconnect()
				timer.Stop()
				chnl, err := s.Channel(CurrentBotChannel)

				if err != nil {
					log.Print("couldnt get the bot channel")
					return
				}

				s.ChannelMessageSend(chnl.ID, "left cuz afk")
				Timers.Delete(CurrentBotChannel)
				return
			default:
				shit, err := s.Channel(CurrentVoiceConnection.ChannelID)

				if err != nil {
					log.Print("wtf")
					return
				}

				if Playing && shit.MemberCount > 1 {
					timer.Reset(AfkTime)
				}

				time.Sleep(1 * time.Second)
			}
		}
	}()
}

func (b *Bot) startPlaying(s *discordgo.Session, song string, guildID string, channelID string) (err error) {
	for starting {
		if !starting {
			break
		}
		time.Sleep(10 * time.Second)
	}
	starting = true
	// Join the provided voice channel.
	vc, err := s.ChannelVoiceJoin(guildID, channelID, false, true)
	CurrentVoiceConnection = vc
	if err != nil {
		return err
	}

	go func() {
		if afk {
			return
		}
		afk = true
		for {
			empty := false
			c, _ := s.GuildChannels(guildID)
			for _, cha := range c {
				if cha.MemberCount == 1 {
					empty = true
				}
			}

			if empty {
				vc.Disconnect()
			}
			time.Sleep(1 * time.Minute)
		}
	}()

	// Start speaking.
	vc.Speaking(true)

	done := make(chan error, 10)

	ses, err := encodeFile(song)
	if err != nil {
		return err
	}

	stre := stream.NewStream(ses, vc, done)
	stre.Start()

	MusicStream = stre
	if err != nil {
		return err
	}
	Playing = true
	starting = false
	defer func() { Playing = false }()
	defer vc.Speaking(false)
	defer ses.Cleanup()

	go func() {
		for {
			if !vc.Ready {
				return
			}

			err := <-done
			if errors.Is(err, stream.ErrStopped) {
				return
			}
			if len(Queue) >= 1 && !repeat && (errors.Is(err, io.EOF) || errors.Is(err, stream.ErrStreamIsDone)) {
				toPlay := Queue[0]
				Queue = Queue[1:]
				linkToPlay := ""
				if toPlay.FallbackURL != "" {
					linkToPlay = toPlay.FallbackURL
				} else {
					linkToPlay = toPlay.RequestedDownloads[0].RequestedFormats[1].URL
				}
				linkToPlay, err := b.downloadVideo(linkToPlay)
				if err != nil {
					log.Println(err.Error())
					return
				}
				b.startPlaying(s, linkToPlay, guildID, channelID)
			}
			if repeat {
				b.startPlaying(s, song, guildID, channelID)
			}
			if !Playing {
				return
			}
			if !errors.Is(err, stream.ErrStreamIsDone) && !(errors.Is(err, io.EOF) && repeat) {
				log.Println(err.Error())
				return
			}
		}

	}()

	err = <-done
	if err != nil && err != io.EOF {
		return err
	}

	if err == io.EOF || err == stream.ErrSkipped || err == stream.ErrStopped {
		return nil
	}
	return err
}

func (b *Bot) playSong(link string, attachment bool) (yt_dlpResponse, error) {
	if attachment {
		return yt_dlpResponse{
			WebpageURL:         link,
			RequestedDownloads: []RequestedDownloads{{[]RequestedFormats{{}, {URL: link}}}}, //since 1st is usually the video
		}, nil
	}
	resp, err := b.getMetadata(link)
	if err != nil {
		return yt_dlpResponse{}, err
	}
	return resp, nil
}

func (b *Bot) IsPlaying() bool {
	return Playing
}

func (b *Bot) AddToQueue(song yt_dlpResponse) {
	Queue = append(Queue, song)
	log.Println(len(Queue))
}

func (b *Bot) getMetadata(ytlink string) (link yt_dlpResponse, err error) {
	path, err := exec.LookPath("yt-dlp")
	if errors.Is(err, exec.ErrDot) {
		err = nil
	}
	if err != nil {
		log.Fatal(err)
	}

	if path == "" {
		log.Fatal("yt-dlp not installed")
		return yt_dlpResponse{}, errors.New("yt-dlp missing")
	}

	args := []string{
		"--ignore-errors",
		"--no-call-home",
		"--no-cache-dir",
		"--skip-download",
		"--restrict-filenames",
		// provide URL via stdin for security, youtube-dl has some run command args
		"--batch-file", "-",
		"-J", "-s",
	}

	ffmpeg := exec.Command("yt-dlp", args...)
	ffmpeg.Stdin = bytes.NewBufferString(ytlink + "\n")

	stdout, _ := ffmpeg.StdoutPipe()

	if err != nil {
		return yt_dlpResponse{}, err
	}

	if err := ffmpeg.Start(); err != nil {
		return yt_dlpResponse{}, err
	}

	zalupa := json.NewDecoder(stdout)
	for {
		infoErr := zalupa.Decode(&link)
		if infoErr == io.EOF {
			break
		}
		if infoErr != nil {
			return yt_dlpResponse{}, err
		}
	}
	if err := ffmpeg.Wait(); err != nil {
		return yt_dlpResponse{}, err
	}

	return link, nil
}

func (b *Bot) downloadVideo(link string) (string, error) {
	filePath := "temp.ogg"
	path, err := exec.LookPath("ffmpeg")
	if errors.Is(err, exec.ErrDot) {
		err = nil
	}
	if err != nil {
		log.Fatal(err)
	}

	if path == "" {
		log.Fatal("ffmpeg not installed")
		return "", errors.New("ffmpeg missing")
	}

	args := []string{
		"-i", link,
		"-map", "0:a",
		"-acodec", "libopus",
		"-f", "ogg",
		"-y",
		filePath,
	}

	ffmpeg := exec.Command("ffmpeg", args...)

	if err := ffmpeg.Start(); err != nil {
		return "", err
	}
	if err := ffmpeg.Wait(); err != nil {
		log.Println(ffmpeg.Stderr)
		return "", err
	}

	return filePath, nil
}

func (b *Bot) togglePause() (bool, error) {
	MusicStream.SetPaused(!MusicStream.Paused())
	return MusicStream.Paused(), nil
}

func (b *Bot) stop() {
	MusicStream.SetFinished()
	repeat = false
	Playing = false
}

func (b *Bot) skip() {
	MusicStream.SetSkipped()
	repeat = false
}

func (b *Bot) toggleRepeat() (bool, error) {
	repeat = !repeat

	return repeat, nil
}
