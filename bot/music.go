package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os/exec"

	"github.com/bwmarrin/discordgo"
	"github.com/erik-petrov/dj_sanya_go/dca"
	"github.com/erik-petrov/dj_sanya_go/stream"
)

var (
	MusicStream  = new(stream.StreamingSession)
	ErrBotStandy = errors.New("Bot is on standy")
	Playing      = false
)

type yt_dlpResponse struct {
	Title              string `json:"title"`
	WebpageURL         string `json:"website_url"`
	Duration           string `json:"duration_string"`
	RequestedDownloads []struct {
		RequestedFormats []struct {
			URL string `json:"url"`
		} `json:"requested_formats"`
	} `json:"requested_downloads"`
}

var repeat bool = false

func encodeFile(song string) (ses *dca.EncodeSession, err error) {
	options := dca.StdEncodeOptions
	options.RawOutput = true
	options.Bitrate = 96
	options.Application = "lowdelay"

	ses, err = dca.EncodeFile(song, options)
	if err != nil {
		return
	}
	return
}

func (b *Bot) startPlaying(s *discordgo.Session, song string, guildID string, channelID string) (err error) {

	// Join the provided voice channel.
	vc, err := s.ChannelVoiceJoin(guildID, channelID, false, true)
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
	defer func() { Playing = false }()
	defer vc.Speaking(false)
	defer ses.Cleanup()

	go func() {
		for {
			err := <-done
			if !errors.Is(err, stream.ErrStreamIsDone) && !(errors.Is(err, io.EOF) && repeat) {
				log.Println(err.Error())
				return
			}
			if repeat {
				b.startPlaying(s, song, guildID, channelID)
			}

		}

	}()

	err = <-done
	if err != nil && err != io.EOF {
		return err
	}

	if err == io.EOF {
		return nil
	}
	return err
}

func (b *Bot) playSong(link string) (yt_dlpResponse, error) {
	resp, err := b.getMetadata(link)
	if err != nil {
		return yt_dlpResponse{}, err
	}
	return resp, nil
}

func (b *Bot) IsPlaying() bool {
	return Playing
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

func (b *Bot) togglePause() (bool, error) {
	MusicStream.SetPaused(!MusicStream.Paused())
	return MusicStream.Paused(), nil
}

func (b *Bot) stop() {
	MusicStream.SetFinished()
}

func (b *Bot) toggleRepeat() (bool, error) {
	repeat = !repeat

	return repeat, nil
}
