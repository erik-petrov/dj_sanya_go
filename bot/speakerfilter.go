package bot

import (
	"strings"
	"sync"
)

// scBotAllowlist holds lowercased identifiers (a user ID, "username", or
// "username#discriminator") of bots whose audio we DO still transcribe. Any
// other bot — including our own music playback, which bleeds in as a separate
// user in the channel — is ignored before transcription. Set in setupEars from
// the SC_BOT_ALLOWLIST env var; defaults to the TTS bot.
var scBotAllowlist map[string]bool

func parseBotAllowlist(s string) map[string]bool {
	if strings.TrimSpace(s) == "" {
		s = "TTS Bot#3590"
	}
	out := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
			out[p] = true
		}
	}
	return out
}

type speakerInfo struct {
	isBot         bool
	username      string
	discriminator string
}

var speakerCache sync.Map // userID -> speakerInfo

// speakerInfo fetches (and caches) whether a user is a bot, plus their name.
func (b *Bot) speakerInfo(userID string) speakerInfo {
	if v, ok := speakerCache.Load(userID); ok {
		return v.(speakerInfo)
	}
	var info speakerInfo
	if u, err := b.s.User(userID); err == nil && u != nil {
		info = speakerInfo{isBot: u.Bot, username: u.Username, discriminator: u.Discriminator}
	}
	speakerCache.Store(userID, info)
	return info
}

// shouldIgnoreSpeaker reports whether audio from userID should be dropped before
// transcription: our own music bot (its Lavalink playback bleeds in as another
// participant) and any other bot not on the allowlist. Real users always pass.
func (b *Bot) shouldIgnoreSpeaker(userID string) bool {
	if userID == b.s.State.User.ID {
		return true // our own music bot's playback
	}
	info := b.speakerInfo(userID)
	if !info.isBot {
		return false // a human — always transcribe
	}
	return !botAllowed(userID, info)
}

func botAllowed(userID string, info speakerInfo) bool {
	if scBotAllowlist[strings.ToLower(userID)] {
		return true
	}
	name := strings.ToLower(info.username)
	if name != "" && scBotAllowlist[name] {
		return true
	}
	if name != "" && info.discriminator != "" && scBotAllowlist[name+"#"+info.discriminator] {
		return true
	}
	return false
}
