package bot

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// Banned users can't use the bot at all: slash commands, the now-playing
// buttons, voice triggers, and the play-hook all reject them. The list persists
// to BAN_FILE (a JSON array of user IDs) so it survives restarts — mount that
// path on a volume to survive container recreation too.
var (
	bannedMu    sync.RWMutex
	bannedUsers = map[string]bool{}
	banFile     string

	// adminOnlyPerm hides /ban and /unban from non-admins in the client UI (the
	// real gate is the OWNER_ID check in the handlers).
	adminOnlyPerm = int64(discordgo.PermissionAdministrator)
)

// isOwnerID reports whether a user is a bot operator. Owners come from OWNER_IDS
// (comma-separated Discord user IDs); only they may ban/unban. Empty disables
// those commands.
func isOwnerID(userID string) bool {
	if userID == "" {
		return false
	}
	for _, id := range strings.Split(os.Getenv("OWNER_IDS"), ",") {
		if strings.TrimSpace(id) == userID {
			return true
		}
	}
	return false
}

// loadBans reads the persisted ban list. Call once on startup.
func loadBans() {
	owners := 0
	for _, id := range strings.Split(os.Getenv("OWNER_IDS"), ",") {
		if strings.TrimSpace(id) != "" {
			owners++
		}
	}
	log.Printf("bot owners configured: %d (from OWNER_IDS)", owners)

	banFile = os.Getenv("BAN_FILE")
	if banFile == "" {
		log.Println("BAN_FILE not set; bans are in-memory only (lost on restart)")
		return
	}
	data, err := os.ReadFile(banFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Println("could not read ban file:", err)
		}
		return
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		log.Println("could not parse ban file:", err)
		return
	}
	bannedMu.Lock()
	for _, id := range ids {
		bannedUsers[id] = true
	}
	n := len(bannedUsers)
	bannedMu.Unlock()
	log.Printf("loaded %d banned user(s)", n)
}

// saveBans writes the current ban list to BAN_FILE (best-effort).
func saveBans() {
	if banFile == "" {
		return
	}
	bannedMu.RLock()
	ids := make([]string, 0, len(bannedUsers))
	for id := range bannedUsers {
		ids = append(ids, id)
	}
	bannedMu.RUnlock()
	data, _ := json.Marshal(ids)
	if err := os.WriteFile(banFile, data, 0o644); err != nil {
		log.Println("could not write ban file:", err)
	}
}

// isBanned reports whether a user is barred from using the bot.
func isBanned(userID string) bool {
	bannedMu.RLock()
	defer bannedMu.RUnlock()
	return bannedUsers[userID]
}

// banUser bans a user and persists the list. Returns false if already banned.
func banUser(userID string) bool {
	bannedMu.Lock()
	if bannedUsers[userID] {
		bannedMu.Unlock()
		return false
	}
	bannedUsers[userID] = true
	bannedMu.Unlock()
	saveBans()
	return true
}

// unbanUser lifts a ban and persists the list. Returns false if not banned.
func unbanUser(userID string) bool {
	bannedMu.Lock()
	if !bannedUsers[userID] {
		bannedMu.Unlock()
		return false
	}
	delete(bannedUsers, userID)
	bannedMu.Unlock()
	saveBans()
	return true
}

// interactionUserID returns the invoking user's ID (guild member or DM user).
func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

// denyBanned replies to a banned user's command/button click.
func denyBanned(s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "🚫 Ты забанен и не можешь использовать бота.",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// botHasAdmin reports whether the bot itself has the Administrator permission
// where the interaction was invoked. Discord sends the app's computed
// permissions (AppPermissions) with every interaction — no API call needed.
func botHasAdmin(i *discordgo.InteractionCreate) bool {
	return i.AppPermissions&discordgo.PermissionAdministrator != 0
}

// denyBotNoAdmin replies when the bot lacks Administrator and refuses the command.
func denyBotNoAdmin(s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "🚫 Боту нужны права администратора на этом сервере.",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (b *Bot) isOwner(i *discordgo.InteractionCreate) bool {
	return isOwnerID(interactionUserID(i))
}

// banTarget resolves the ban/unban target from either the "user" option (a
// mention/picker) or the "id" option (a raw user ID, so you can act on someone
// not in the server). Prefers the user option; mention is a display string.
func banTarget(s *discordgo.Session, i *discordgo.InteractionCreate) (id, mention string) {
	for _, opt := range i.ApplicationCommandData().Options {
		if opt.Name == "user" {
			if u := opt.UserValue(s); u != nil {
				return u.ID, u.Mention()
			}
		}
	}
	for _, opt := range i.ApplicationCommandData().Options {
		if opt.Name == "id" {
			if v := strings.TrimSpace(opt.StringValue()); isSnowflake(v) {
				return v, "<@" + v + ">"
			}
		}
	}
	return "", ""
}

// isSnowflake reports whether s looks like a Discord ID (a non-empty run of
// digits). Guards against banning a typo'd string that could never match a user.
func isSnowflake(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// onBan handles /ban (owner only).
func (b *Bot) onBan(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.isOwner(i) {
		respond(s, i, "Только владелец бота может банить.")
		return
	}
	id, mention := banTarget(s, i)
	if id == "" {
		respond(s, i, "Укажи пользователя или корректный ID.")
		return
	}
	if isOwnerID(id) {
		respond(s, i, "Нельзя забанить владельца.")
		return
	}
	if banUser(id) {
		respond(s, i, "🚫 Забанен: "+mention)
	} else {
		respond(s, i, mention+" уже забанен.")
	}
}

// onUnban handles /unban (owner only).
func (b *Bot) onUnban(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.isOwner(i) {
		respond(s, i, "Только владелец бота может это делать.")
		return
	}
	id, mention := banTarget(s, i)
	if id == "" {
		respond(s, i, "Укажи пользователя или корректный ID.")
		return
	}
	if unbanUser(id) {
		respond(s, i, "✅ Разбанен: "+mention)
	} else {
		respond(s, i, mention+" не был забанен.")
	}
}
