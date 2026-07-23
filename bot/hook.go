package bot

import (
	"crypto/subtle"
	"log"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Play-hook: an external trigger (e.g. a browser extension) POSTs to a Discord
// webhook in a control channel with the message "<password> <userID> <url>".
// The bot is already on the gateway, so it sees that webhook message, checks the
// shared password, finds which voice channel that user is in across all of its
// guilds, and plays there. The transport is Discord itself — no exposed port.
//
// Reading the webhook message text needs the (privileged) Message Content
// intent, requested in New only when HOOK_PASSWORD is set.
var (
	hookPassword  string // shared secret; empty disables the feature
	hookChannelID string // optional: only react to this control channel
)

// hookEnabled reports whether the play-hook is configured (password present).
func hookEnabled() bool { return os.Getenv("HOOK_PASSWORD") != "" }

// setupHook wires the webhook-message play trigger, if HOOK_PASSWORD is set.
func (b *Bot) setupHook() {
	hookPassword = os.Getenv("HOOK_PASSWORD")
	if hookPassword == "" {
		log.Println("HOOK_PASSWORD not set; play-hook disabled")
		return
	}
	hookChannelID = os.Getenv("HOOK_CHANNEL_ID")
	b.s.AddHandler(b.onHookMessage)
	where := "any channel"
	if hookChannelID != "" {
		where = "channel " + hookChannelID
	}
	log.Printf("play-hook enabled (webhook trigger in %s)", where)
}

// onHookMessage handles a webhook message: authorize by password, then play the
// requested URL in the requesting user's current voice channel.
func (b *Bot) onHookMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Only webhook posts, and only in the control channel when one is configured.
	if m.WebhookID == "" {
		return
	}
	if hookChannelID != "" && m.ChannelID != hookChannelID {
		return
	}

	// Expect "<password> <userID> <url-or-query>".
	fields := strings.Fields(m.Content)
	if len(fields) < 3 {
		return
	}
	if subtle.ConstantTimeCompare([]byte(fields[0]), []byte(hookPassword)) != 1 {
		log.Println("[hook] rejected webhook message: bad password")
		return
	}
	// The message carries the password in plaintext, so remove it from channel
	// history (best-effort; needs Manage Messages in the control channel).
	_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
	userID := fields[1]
	query := strings.TrimSpace(strings.Join(fields[2:], " "))
	if userID == "" || query == "" {
		return
	}

	reply := func(msg string) { _, _ = s.ChannelMessageSend(m.ChannelID, msg) }

	guildID, channelID, ok := b.findUserVoiceAnywhere(userID)
	if !ok {
		reply("<@" + userID + "> не в голосовом канале — нечего воспроизводить.")
		return
	}

	// Route now-playing / status back to the control channel.
	announceChannels.Store(guildID, m.ChannelID)
	log.Printf("[hook] play %q for user %s in guild %s channel %s", query, userID, guildID, channelID)
	reply("▶️ <@" + userID + "> ставлю…")
	go b.playQuery(guildID, channelID, query, m.ChannelID)
}

// findUserVoiceAnywhere returns the guild and voice channel the user is in,
// searching every guild the bot shares with them (fine for a small bot). ok is
// false when they aren't connected to voice anywhere.
func (b *Bot) findUserVoiceAnywhere(userID string) (guildID, channelID string, ok bool) {
	for _, g := range b.s.State.Guilds {
		for _, vs := range g.VoiceStates {
			if vs.UserID == userID && vs.ChannelID != "" {
				return g.ID, vs.ChannelID, true
			}
		}
	}
	return "", "", false
}
