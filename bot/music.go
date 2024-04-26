package bot

import (
	"context"
	"errors"
	"io"
	"log"
	"os/exec"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/erik-petrov/dj_sanya_go/dca"
	"github.com/erik-petrov/dj_sanya_go/stream"
	"github.com/wader/goutubedl"
)

var (
	MusicStream  = new(stream.StreamingSession)
	ErrBotStandy = errors.New("Bot is on standy")
)

type Song struct {
	Title    string
	Link     string
	RawLink  string
	Duration float64
}

var repeat bool = false

func encodeFile(song Song) (ses *dca.EncodeSession, err error) {
	options := dca.StdEncodeOptions
	options.RawOutput = true
	options.Bitrate = 96
	options.Application = "lowdelay"

	ses, err = dca.EncodeFile(song.RawLink, options)
	if err != nil {
		return
	}
	return
}

func (b *Bot) startPlaying(s *discordgo.Session, song Song, guildID string, channelID string) (err error) {

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
	defer ses.Cleanup()

	go func() {
		for {
			err := <-done
			if !errors.Is(err, stream.ErrStreamIsDone) {
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
		// Stop speaking
		vc.Speaking(false)
		return nil
	}
	return err
}

func (b *Bot) playSong(link string) Song {
	song := b.getMusicData(link)
	rawlink, err := b.getLink(link)
	if err != nil {
		log.Println("couldnt get raw link")
	}
	song.Link = link
	song.RawLink = rawlink
	return song
}

func (b *Bot) getMusicData(link string) Song {
	result, err := goutubedl.New(context.Background(), link, goutubedl.Options{})
	if err != nil {
		log.Fatalf("Couldn't get music data, err %v", err)
	}
	return Song{
		Title:    result.Info.Title,
		Duration: result.Info.Duration,
	}
}

func (b *Bot) getLink(ytlink string) (link string, err error) {
	path, err := exec.LookPath("yt-dlp")
	if errors.Is(err, exec.ErrDot) {
		err = nil
	}
	if err != nil {
		log.Fatal(err)
	}

	if path == "" {
		log.Fatal("yt-dlp not installed")
		return "", errors.New("yt-dlp missing")
	}

	args := []string{
		"--get-url",
	}

	args = append(args, ytlink)

	ffmpeg := exec.Command("yt-dlp", args...)

	// logln(ffmpeg.Args)
	std, err := ffmpeg.Output()
	if err != nil {
		return "", err
	}

	link = strings.Split(string(std), "\n")[1]

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
