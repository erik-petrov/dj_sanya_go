package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Prefix command, one guild only: "sanya!kick 1h" disconnects everyone from all
// voice channels after the delay (Go duration: 1m, 1h, 90m, ...). Needs the
// Message Content intent (already on when the play-hook is enabled) and the
// bot's Move Members permission. In-memory timer — a restart cancels a pending kick.
const (
	kickGuildID = "802182206071767081"
	kickPrefix  = "sanya!kick"
)

func (b *Bot) setupKick() { b.s.AddHandler(b.onKickMessage) }

// parseKickDelay validates a Go duration string, capped at 24h.
func parseKickDelay(s string) (time.Duration, bool) {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 || d > 24*time.Hour {
		return 0, false
	}
	return d, true
}

func (b *Bot) onKickMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.GuildID != kickGuildID || m.Author == nil || m.Author.Bot {
		return
	}
	fields := strings.Fields(m.Content)
	if len(fields) == 0 || fields[0] != kickPrefix {
		return
	}

	// Only members who could disconnect people themselves may schedule it.
	perms, err := s.State.UserChannelPermissions(m.Author.ID, m.ChannelID)
	if err != nil || perms&discordgo.PermissionVoiceMoveMembers == 0 {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Нужно право «Перемещать участников».")
		return
	}
	if len(fields) < 2 {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Формат: `sanya!kick 1h` (1m = минута, 1h = час).")
		return
	}
	dur, ok := parseKickDelay(fields[1])
	if !ok {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Не понял время. Пример: `sanya!kick 1h` или `sanya!kick 30m` (макс. 24h).")
		return
	}

	_, _ = s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("⏰ Кикну всех из голосовых через %s.", fields[1]))
	time.AfterFunc(dur, func() { b.kickAllVoice(kickGuildID) })
}

// kickAllVoice disconnects everyone currently in a voice channel in the guild,
// except the bot's own accounts.
func (b *Bot) kickAllVoice(guildID string) {
	g, err := b.s.State.Guild(guildID)
	if err != nil {
		log.Println("kickAllVoice:", err)
		return
	}
	skip := map[string]bool{b.s.State.User.ID: true}
	if b.ears != nil {
		skip[b.ears.State.User.ID] = true
	}
	// Snapshot IDs first: GuildMemberMove mutates the guild's voice states.
	var ids []string
	for _, vs := range g.VoiceStates {
		if vs.ChannelID != "" && !skip[vs.UserID] {
			ids = append(ids, vs.UserID)
		}
	}
	for _, id := range ids {
		if err := b.s.GuildMemberMove(guildID, id, nil); err != nil {
			log.Printf("kickAllVoice: disconnect %s: %v", id, err)
		}
	}
	log.Printf("kickAllVoice: disconnected %d from voice in guild %s", len(ids), guildID)
}
