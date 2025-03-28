package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/erik-petrov/dj_sanya_go/dca"
	"github.com/erik-petrov/dj_sanya_go/stream"
)

var (
	AfkTime                = 600
	MusicStream            *stream.StreamingSession
	ErrBotStandy           = errors.New("Bot is on standy")
	Playing                = false
	Queue                  = make([]YTDLPResponse, 0)
	CurrentVoiceConnection *discordgo.VoiceConnection
	Timers                 sync.Map
)

type YTDLPResponse struct {
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

	timer := AfkTime
	Timers.Store(CurrentBotChannel, timer)

	shit, err := s.Channel(CurrentVoiceConnection.ChannelID)

	if err != nil {
		log.Print("wtf")
		return
	}

	go func() {
		for timer > 1 {
			if (Playing || starting) && shit.MemberCount > 1 {
				timer = AfkTime
			}

			time.Sleep(1 * time.Second)
			timer -= 1
		}
		CurrentVoiceConnection.Disconnect()
		CurrentVoiceConnection = nil
		chnl, err := s.Channel(CurrentBotChannel)

		if err != nil {
			log.Print("couldnt get the bot channel")
			return
		}

		s.ChannelMessageSend(chnl.ID, "left cuz afk")
		Timers.Delete(CurrentBotChannel)
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
					log.Println("error downloading msuic: ", err.Error())
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
				log.Println("stream error: ", err.Error())
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

func (b *Bot) playSong(link string, attachment bool, songCh chan<- YTDLPResponse, errCh chan<- error) {
	if attachment {
		songCh <- YTDLPResponse{
			WebpageURL:         link,
			RequestedDownloads: []RequestedDownloads{{[]RequestedFormats{{}, {URL: link}}}}, //since 1st is usually the video
		}
		errCh <- nil
		return
	}
	resp, err := b.getMetadata(link)
	if err != nil {
		songCh <- YTDLPResponse{}
		errCh <- err
		return
	}
	songCh <- resp
	errCh <- nil
}

func (b *Bot) IsPlaying() bool {
	return Playing
}

func (b *Bot) AddToQueue(song YTDLPResponse) {
	Queue = append(Queue, song)
}

func (b *Bot) getMetadata(ytlink string) (link YTDLPResponse, err error) {
	path, err := exec.LookPath("yt-dlp")
	if errors.Is(err, exec.ErrDot) {
		err = nil
	}
	if err != nil {
		log.Fatal("error with yt-dlp finder: ", err)
		return YTDLPResponse{}, err
	}

	if path == "" {
		log.Fatal("yt-dlp not installed")
		return YTDLPResponse{}, errors.New("yt-dlp missing")
	}

	cookies := "--cookies"
	var cookiepath string
	if os.Getenv("TESTING") != "true" {
		cookiepath = "/cookies.txt"
	} else {
		cookiepath = "./cookies.txt"
	}

	if _, err := os.Stat(path); err != nil {
		cookies = ""
		cookiepath = ""
	}

	args := []string{
		"--no-call-home",
		"--no-cache-dir",
		"--skip-download",
		"--force-ipv4",
		"--restrict-filenames",
		cookies, cookiepath,
		// provide URL via stdin for security, youtube-dl has some run command args
		"--batch-file", "-",
		"-J", "-s",
	}

	ffmpeg := exec.Command("yt-dlp", args...)

	var oshibka bytes.Buffer
	ffmpeg.Stderr = &oshibka

	ffmpeg.Stdin = bytes.NewBufferString(ytlink + "\n")
	stdout, err := ffmpeg.StdoutPipe()

	if err != nil {
		return YTDLPResponse{}, err
	}

	if err := ffmpeg.Start(); err != nil {
		return YTDLPResponse{}, err
	}

	zalupa := json.NewDecoder(stdout)
	for {
		infoErr := zalupa.Decode(&link)
		if infoErr == io.EOF {
			break
		}

		if infoErr != nil {
			return YTDLPResponse{}, errors.New(oshibka.String())
		}
	}

	if err := ffmpeg.Wait(); err != nil {
		log.Println(oshibka.String())
		return YTDLPResponse{}, errors.New(oshibka.String())
	}

	return link, nil //link, nil
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
