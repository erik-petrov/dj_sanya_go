package bot

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

// onUserVoiceUpdate watches voice-state changes and makes the bot leave once the
// channel it's in has no human members left (only bots, or empty).
func (b *Bot) onUserVoiceUpdate(s *discordgo.Session, e *discordgo.VoiceStateUpdate) {
	if e.GuildID == "" {
		return
	}
	ch := b.botVoiceChannel(e.GuildID)
	if ch == "" {
		return // the bot isn't in a voice channel in this guild
	}
	if b.humanCount(e.GuildID, ch) == 0 {
		log.Printf("[voice] no humans left in guild %s; leaving", e.GuildID)
		b.leaveVoice(e.GuildID, "👋 В канале не осталось людей — выхожу.")
	}
}

// botVoiceChannel returns the voice channel the bot currently occupies in a
// guild: the music session's own voice state, falling back to the ears
// listener's channel. Empty string if the bot isn't in voice there.
func (b *Bot) botVoiceChannel(guildID string) string {
	if vs, err := b.s.State.VoiceState(guildID, b.s.State.User.ID); err == nil && vs != nil && vs.ChannelID != "" {
		return vs.ChannelID
	}
	if l, ok := earsListeners.Load(guildID); ok {
		return l.(*earsListener).channelID
	}
	return ""
}

// humanCount returns how many non-bot members are in the given voice channel.
// Returns -1 when the guild state is unknown, so callers don't leave on a miss.
func (b *Bot) humanCount(guildID, channelID string) int {
	g, err := b.s.State.Guild(guildID)
	if err != nil {
		return -1
	}
	count := 0
	for _, vs := range g.VoiceStates {
		if vs.ChannelID != channelID {
			continue
		}
		if vs.UserID == b.s.State.User.ID {
			continue // our music bot
		}
		if b.ears != nil && vs.UserID == b.ears.State.User.ID {
			continue // our ears bot
		}
		if b.speakerInfo(vs.UserID).isBot {
			continue // any other bot
		}
		count++
	}
	return count
}

// leaveVoice fully disconnects the bot from a guild's voice: stops Lavalink
// playback (music bot leaves), stops listening (ears bot leaves), and clears the
// per-guild announce channel. If reason is non-empty it's posted there first.
func (b *Bot) leaveVoice(guildID, reason string) {
	announceID := ""
	if ch, ok := announceChannels.Load(guildID); ok {
		announceID, _ = ch.(string)
	}
	if announceID != "" && reason != "" {
		_, _ = b.s.ChannelMessageSend(announceID, reason)
	}
	if LavalinkClient != nil {
		_ = b.StopLavalink(guildID)
	}
	if b.ears != nil {
		_ = b.stopListening(guildID)
	}
	announceChannels.Delete(guildID)
}
