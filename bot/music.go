package bot

import (
	"encoding/json"
	"log"
	"os"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// Per-guild music channel: an explicit override for where now-playing / status
// posts, so the Repeat button after a restart and webhook plays are deterministic
// instead of relying on defaultAnnounceChannel's guess. Set with /set-music-channel;
// persisted to MUSIC_CHANNEL_FILE (JSON), same pattern as the ban list in ban.go.
var (
	musicMu       sync.RWMutex
	musicChannels = map[string]string{} // guildID -> channelID
	musicFile     string
)

// loadMusicChannels reads the persisted per-guild music channels. Call on startup.
func loadMusicChannels() {
	musicFile = os.Getenv("MUSIC_CHANNEL_FILE")
	if musicFile == "" {
		return
	}
	data, err := os.ReadFile(musicFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Println("could not read music-channel file:", err)
		}
		return
	}
	musicMu.Lock()
	defer musicMu.Unlock()
	if err := json.Unmarshal(data, &musicChannels); err != nil {
		log.Println("could not parse music-channel file:", err)
		return
	}
	log.Printf("loaded music channel for %d guild(s)", len(musicChannels))
}

// saveMusicChannels writes the current map to MUSIC_CHANNEL_FILE (best-effort).
func saveMusicChannels() {
	if musicFile == "" {
		return
	}
	musicMu.RLock()
	data, _ := json.MarshalIndent(musicChannels, "", "  ")
	musicMu.RUnlock()
	if err := os.WriteFile(musicFile, data, 0o644); err != nil {
		log.Println("could not write music-channel file:", err)
	}
}

// musicChannel returns the guild's explicit music channel, or "".
func musicChannel(guildID string) string {
	musicMu.RLock()
	defer musicMu.RUnlock()
	return musicChannels[guildID]
}

// onSetMusicChannel handles /set-music-channel: pin where now-playing/status posts.
func (b *Bot) onSetMusicChannel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ch := i.ApplicationCommandData().Options[0].ChannelValue(s)
	if ch == nil {
		respondEph(s, i, "❌ Не указан канал.")
		return
	}
	musicMu.Lock()
	musicChannels[i.GuildID] = ch.ID
	musicMu.Unlock()
	saveMusicChannels()
	respondEph(s, i, "🎵 Музыкальный канал: <#"+ch.ID+">")
}
