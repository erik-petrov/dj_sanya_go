package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
)

// parseSeek parses a /seek argument into a millisecond value. A leading "+"/"-"
// marks it relative (an offset from the current position); otherwise it's an
// absolute position. The time itself is "s", "m:ss", or "h:mm:ss", or a plain
// number of seconds.
func parseSeek(input string) (ms int64, relative bool, err error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return 0, false, errors.New("empty")
	}
	sign := int64(1)
	if s[0] == '+' || s[0] == '-' {
		relative = true
		if s[0] == '-' {
			sign = -1
		}
		s = strings.TrimSpace(s[1:])
	}
	if s == "" {
		return 0, false, errors.New("empty")
	}

	var totalSec int64
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) > 3 {
			return 0, false, errors.New("too many parts")
		}
		for _, p := range parts {
			n, e := strconv.Atoi(strings.TrimSpace(p))
			if e != nil || n < 0 {
				return 0, false, errors.New("bad number")
			}
			totalSec = totalSec*60 + int64(n)
		}
	} else {
		n, e := strconv.Atoi(s)
		if e != nil || n < 0 {
			return 0, false, errors.New("bad number")
		}
		totalSec = int64(n)
	}
	return sign * totalSec * 1000, relative, nil
}

// onSeek handles /seek: jump to an absolute time (1:23 / 90) or a relative offset
// (+30 / -15) within the current track.
func (b *Bot) onSeek(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if LavalinkClient == nil {
		respond(s, i, "Music is not available right now.")
		return
	}
	player := LavalinkClient.ExistingPlayer(snowflake.MustParse(i.GuildID))
	if player == nil || player.Track() == nil {
		respond(s, i, "Сейчас ничего не играет.")
		return
	}
	track := player.Track()
	if track.Info.IsStream {
		respond(s, i, "Трансляцию нельзя перематывать.")
		return
	}

	val, relative, err := parseSeek(i.ApplicationCommandData().Options[0].StringValue())
	if err != nil {
		respond(s, i, "Не понял время. Примеры: `1:23`, `90`, `+30`, `-15`")
		return
	}

	target := val
	if relative {
		target = int64(player.Position()) + val
	}
	if length := int64(track.Info.Length); length > 0 && target > length {
		target = length
	}
	if target < 0 {
		target = 0
	}

	if err := player.Update(context.TODO(), lavalink.WithPosition(lavalink.Duration(target))); err != nil {
		respond(s, i, "Не получилось перемотать: "+err.Error())
		return
	}
	respond(s, i, fmt.Sprintf("⏩ %s / %s", fmtDuration(lavalink.Duration(target)), fmtDuration(track.Info.Length)))
}
