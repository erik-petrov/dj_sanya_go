package music

import (
	"bytes"
	"context"
	"io"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/wader/goutubedl"
)

type Song struct {
	Title    string
	Duration float64
	Buffer   []byte
}

func StartPlaying(s *discordgo.Session, song Song, guildID string, channelID string) (err error) {

	// Join the provided voice channel.
	vc, err := s.ChannelVoiceJoin(guildID, channelID, false, true)
	if err != nil {
		return err
	}

	// Start speaking.
	vc.Speaking(true)

	// Send the buffer data.
	vc.OpusSend <- song.Buffer

	// Stop speaking
	vc.Speaking(false)

	return nil
}

func PlaySong(link string) Song {
	song := getMusicData(link)
	song.Buffer = makeSongBuffer(link)
	log.Println("done downloading")
	return song
}

func getMusicData(link string) Song {
	result, err := goutubedl.New(context.Background(), link, goutubedl.Options{})
	if err != nil {
		log.Fatalf("Couldn't get music data, err %v", err)
	}
	return Song{
		Title:    result.Info.Title,
		Duration: result.Info.Duration,
	}
}

func makeSongBuffer(link string) []byte {
	dr, err := goutubedl.Download(context.Background(), link, goutubedl.Options{
		Type:              goutubedl.TypeSingle,
		MergeOutputFormat: "opus",
	}, "")
	if err != nil {
		log.Fatalf("Couldn't download the song, err: %v", err)
	}
	downloadBuf := &bytes.Buffer{}
	n, err := io.Copy(downloadBuf, dr)
	if err != nil {
		log.Fatal("Couldn't copy downloaded into buffer")
	}
	dr.Close()
	if n != int64(downloadBuf.Len()) {
		log.Printf("copy n not equal to download buffer: %d!=%d", n, downloadBuf.Len())
	}
	return downloadBuf.Bytes()
}
