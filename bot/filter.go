package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Message filter: moderators configure regex block/allow patterns per guild via
// slash commands (see filter_commands.go); a message in a watched channel that
// matches a block pattern — and no whitelist pattern, from a member without the
// bypass role — is deleted and logged to a notification channel with the body,
// offender, and timestamp. Delete + log only: no bans, timeouts, or strikes.
//
// Opt-in via FILTER_ENABLED because reading message text needs the privileged
// Message Content intent (asking for it when it isn't enabled in the Developer
// Portal makes the gateway reject login). Config persists to FILTER_FILE (JSON),
// the same pattern as the ban list in ban.go.

// filterGuild is one guild's filter configuration (JSON-persisted).
type filterGuild struct {
	Regexes       []string        `json:"regexes"`
	Whitelist     []string        `json:"whitelist"`
	Watched       map[string]bool `json:"watched"` // channel IDs to filter
	NotifyChannel string          `json:"notify_channel"`
	BypassRole    string          `json:"bypass_role"`
}

// filterRe pairs a source pattern with its compiled form so scan can report which
// pattern fired even if a manually-edited config held an invalid one (skipped).
type filterRe struct {
	src string
	re  *regexp.Regexp
}

var (
	filterMu   sync.RWMutex
	filterCfg  = map[string]*filterGuild{} // guildID -> config
	filterFile string
	// Compiled caches, rebuilt on load and on every change; keyed by guild ID.
	filterBlockRe = map[string][]filterRe{}
	filterAllowRe = map[string][]filterRe{}
)

// filterEnabled reports whether the message filter is turned on.
func filterEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FILTER_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// guildCfg returns the guild's config, creating an empty one. Caller holds filterMu.
func guildCfg(guildID string) *filterGuild {
	g := filterCfg[guildID]
	if g == nil {
		g = &filterGuild{Watched: map[string]bool{}}
		filterCfg[guildID] = g
	}
	if g.Watched == nil {
		g.Watched = map[string]bool{}
	}
	return g
}

// compileAll compiles patterns case-insensitively, skipping (and logging) any that
// don't compile. The add-* commands reject invalid patterns up front, so a skip
// here only happens for a hand-edited config file.
func compileAll(pats []string) []filterRe {
	out := make([]filterRe, 0, len(pats))
	for _, p := range pats {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			log.Printf("filter: skipping invalid pattern %q: %v", p, err)
			continue
		}
		out = append(out, filterRe{src: p, re: re})
	}
	return out
}

// recompile rebuilds a guild's compiled caches from its patterns. Caller holds filterMu.
func recompile(guildID string) {
	g := filterCfg[guildID]
	if g == nil {
		delete(filterBlockRe, guildID)
		delete(filterAllowRe, guildID)
		return
	}
	filterBlockRe[guildID] = compileAll(g.Regexes)
	filterAllowRe[guildID] = compileAll(g.Whitelist)
}

// loadFilter reads the persisted per-guild config. Call once on startup.
func loadFilter() {
	filterFile = os.Getenv("FILTER_FILE")
	if filterFile == "" {
		log.Println("FILTER_FILE not set; filter config is in-memory only (lost on restart)")
		return
	}
	data, err := os.ReadFile(filterFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Println("could not read filter file:", err)
		}
		return
	}
	filterMu.Lock()
	defer filterMu.Unlock()
	if err := json.Unmarshal(data, &filterCfg); err != nil {
		log.Println("could not parse filter file:", err)
		return
	}
	for gid := range filterCfg {
		recompile(gid)
	}
	log.Printf("loaded message-filter config for %d guild(s)", len(filterCfg))
}

// saveFilter writes the current config to FILTER_FILE (best-effort).
func saveFilter() {
	if filterFile == "" {
		return
	}
	filterMu.RLock()
	data, _ := json.MarshalIndent(filterCfg, "", "  ")
	filterMu.RUnlock()
	if err := os.WriteFile(filterFile, data, 0o644); err != nil {
		log.Println("could not write filter file:", err)
	}
}

// scan returns the first block pattern that matches content when no whitelist
// pattern does. ("", false) means take no action.
func scan(guildID, content string) (string, bool) {
	filterMu.RLock()
	defer filterMu.RUnlock()
	blocks := filterBlockRe[guildID]
	if len(blocks) == 0 {
		return "", false
	}
	for _, fr := range filterAllowRe[guildID] {
		if fr.re.MatchString(content) {
			return "", false // whitelisted: spare the whole message
		}
	}
	for _, fr := range blocks {
		if fr.re.MatchString(content) {
			return fr.src, true
		}
	}
	return "", false
}

// setupFilter loads config and registers the message handlers. No-op if disabled.
func (b *Bot) setupFilter() {
	if !filterEnabled() {
		return
	}
	loadFilter()
	b.s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		b.onFilterMessage(s, m.Message)
	})
	// Editing a clean message into a slur is the obvious bypass — same scan, ~free.
	b.s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		b.onFilterMessage(s, m.Message)
	})
	log.Println("message filter enabled")
}

// onFilterMessage deletes and logs a message that trips a block pattern.
func (b *Bot) onFilterMessage(s *discordgo.Session, m *discordgo.Message) {
	if m == nil || m.GuildID == "" || m.Author == nil || m.Author.Bot || m.WebhookID != "" {
		return
	}

	filterMu.RLock()
	g := filterCfg[m.GuildID]
	watched := g != nil && g.Watched[m.ChannelID]
	var notify, bypass string
	if g != nil {
		notify, bypass = g.NotifyChannel, g.BypassRole
	}
	filterMu.RUnlock()
	if !watched {
		return
	}

	// Bypass role. On edits m.Member may be absent; fall back to cached state.
	if bypass != "" {
		member := m.Member
		if member == nil {
			member, _ = s.State.Member(m.GuildID, m.Author.ID)
		}
		if member != nil {
			for _, r := range member.Roles {
				if r == bypass {
					return
				}
			}
		}
	}

	pattern, ok := scan(m.GuildID, m.Content)
	if !ok {
		return
	}
	if err := s.ChannelMessageDelete(m.ChannelID, m.ID); err != nil {
		log.Printf("filter: delete failed in %s: %v", m.ChannelID, err)
	}
	b.filterLog(s, m, pattern, notify)
}

// filterLog posts the deletion notice (offender, channel, rule, body, timestamp).
func (b *Bot) filterLog(s *discordgo.Session, m *discordgo.Message, pattern, notify string) {
	if notify == "" {
		return
	}
	body := m.Content
	if body == "" {
		body = "*(без текста)*"
	} else {
		if len([]rune(body)) > 900 {
			body = truncate(body, 900)
		}
		// Spoiler-wrap so the slur isn't re-displayed in the open.
		body = "|| " + strings.ReplaceAll(body, "||", "​|​|") + " ||"
	}
	embed := &discordgo.MessageEmbed{
		Title: "🛑 Сообщение удалено фильтром",
		Color: 0xED4245,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Нарушитель", Value: fmt.Sprintf("%s (`%s`)", m.Author.Mention(), m.Author.ID)},
			{Name: "Канал", Value: fmt.Sprintf("<#%s>", m.ChannelID)},
			{Name: "Правило", Value: "`" + truncate(pattern, 200) + "`"},
			{Name: "Текст", Value: body},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := s.ChannelMessageSendEmbed(notify, embed); err != nil {
		log.Printf("filter: log to %s failed: %v", notify, err)
	}
}
