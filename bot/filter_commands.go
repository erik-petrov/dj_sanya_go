package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Slash commands that manage the message filter. All are gated to Manage Messages
// in the client UI via DefaultMemberPermissions (same idea as adminOnlyPerm on
// /ban). Registered by merging filterCommands() into setupCommands.

var manageMessagesPerm = int64(discordgo.PermissionManageMessages)

// filterRemoveID is the custom ID of the /filter-config removal select menu.
// Routed early in onComponent (play.go) so it works even when Lavalink is down.
const filterRemoveID = "filter_remove"

// filterCommands returns the filter slash commands and their handlers.
func (b *Bot) filterCommands() ([]*discordgo.ApplicationCommand, map[string]func(*discordgo.Session, *discordgo.InteractionCreate)) {
	strOpt := func(name, desc string) []*discordgo.ApplicationCommandOption {
		return []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: name, Description: desc, Required: true}}
	}
	chanOpt := func(desc string) []*discordgo.ApplicationCommandOption {
		return []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: desc, Required: true}}
	}
	cmds := []*discordgo.ApplicationCommand{
		{Name: "add-regex", Description: "Add a block pattern (regex, case-insensitive) for this server", DefaultMemberPermissions: &manageMessagesPerm,
			Options: strOpt("pattern", `Regex. A link is a pattern too, e.g. discord\.gg/\S+`)},
		{Name: "add-whitelist", Description: "Add an allow pattern; a message matching it is never filtered", DefaultMemberPermissions: &manageMessagesPerm,
			Options: strOpt("pattern", "Regex (case-insensitive)")},
		{Name: "watch-channel", Description: "Toggle message filtering in a channel", DefaultMemberPermissions: &manageMessagesPerm,
			Options: chanOpt("Channel to watch / unwatch")},
		{Name: "set-notification-channel", Description: "Channel where filtered messages are logged", DefaultMemberPermissions: &manageMessagesPerm,
			Options: chanOpt("Log channel")},
		{Name: "set-bypass-role", Description: "Members with this role are never filtered", DefaultMemberPermissions: &manageMessagesPerm,
			Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionRole, Name: "role", Description: "Bypass role", Required: true}}},
		{Name: "filter-config", Description: "Show the filter config and remove patterns", DefaultMemberPermissions: &manageMessagesPerm},
	}
	h := map[string]func(*discordgo.Session, *discordgo.InteractionCreate){
		"add-regex":                b.onAddRegex,
		"add-whitelist":            b.onAddWhitelist,
		"watch-channel":            b.onWatchChannel,
		"set-notification-channel": b.onSetNotifyChannel,
		"set-bypass-role":          b.onSetBypassRole,
		"filter-config":            b.onFilterConfig,
	}
	return cmds, h
}

// respondEph replies to an interaction with an ephemeral (mod-only) message.
func respondEph(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg, Flags: discordgo.MessageFlagsEphemeral},
	})
}

func (b *Bot) onAddRegex(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.addPattern(s, i, false)
}

func (b *Bot) onAddWhitelist(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.addPattern(s, i, true)
}

// addPattern validates and appends a block (or whitelist) pattern.
func (b *Bot) addPattern(s *discordgo.Session, i *discordgo.InteractionCreate, whitelist bool) {
	pat := i.ApplicationCommandData().Options[0].StringValue()
	if _, err := regexp.Compile(pat); err != nil {
		respondEph(s, i, "❌ Неверное регулярное выражение: `"+err.Error()+"`")
		return
	}
	filterMu.Lock()
	g := guildCfg(i.GuildID)
	if whitelist {
		g.Whitelist = append(g.Whitelist, pat)
	} else {
		g.Regexes = append(g.Regexes, pat)
	}
	recompile(i.GuildID)
	filterMu.Unlock()
	saveFilter()
	kind := "блок"
	if whitelist {
		kind = "вайтлист"
	}
	respondEph(s, i, fmt.Sprintf("✅ Добавлен %s-паттерн: `%s`", kind, pat))
}

func (b *Bot) onWatchChannel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ch := i.ApplicationCommandData().Options[0].ChannelValue(s)
	if ch == nil {
		respondEph(s, i, "❌ Не указан канал.")
		return
	}
	filterMu.Lock()
	g := guildCfg(i.GuildID)
	var msg string
	if g.Watched[ch.ID] {
		delete(g.Watched, ch.ID)
		msg = "🔕 Больше не слежу за <#" + ch.ID + ">"
	} else {
		g.Watched[ch.ID] = true
		msg = "👁 Слежу за <#" + ch.ID + ">"
	}
	filterMu.Unlock()
	saveFilter()
	respondEph(s, i, msg)
}

func (b *Bot) onSetNotifyChannel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ch := i.ApplicationCommandData().Options[0].ChannelValue(s)
	if ch == nil {
		respondEph(s, i, "❌ Не указан канал.")
		return
	}
	filterMu.Lock()
	guildCfg(i.GuildID).NotifyChannel = ch.ID
	filterMu.Unlock()
	saveFilter()
	respondEph(s, i, "📋 Логи фильтра → <#"+ch.ID+">")
}

func (b *Bot) onSetBypassRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	role := i.ApplicationCommandData().Options[0].RoleValue(s, i.GuildID)
	if role == nil {
		respondEph(s, i, "❌ Не указана роль.")
		return
	}
	filterMu.Lock()
	guildCfg(i.GuildID).BypassRole = role.ID
	filterMu.Unlock()
	saveFilter()
	respondEph(s, i, "🛡 Роль обхода: <@&"+role.ID+">")
}

func (b *Bot) onFilterConfig(s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: filterConfigData(i.GuildID),
	})
}

// onFilterRemove handles the /filter-config removal select menu.
func (b *Bot) onFilterRemove(s *discordgo.Session, i *discordgo.InteractionCreate) {
	dropRe, dropWl := map[int]bool{}, map[int]bool{}
	for _, v := range i.MessageComponentData().Values {
		kind, idxStr, _ := strings.Cut(v, ":")
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		switch kind {
		case "re":
			dropRe[idx] = true
		case "wl":
			dropWl[idx] = true
		}
	}
	filterMu.Lock()
	if g := filterCfg[i.GuildID]; g != nil {
		g.Regexes = keepUnmarked(g.Regexes, dropRe)
		g.Whitelist = keepUnmarked(g.Whitelist, dropWl)
		recompile(i.GuildID)
	}
	filterMu.Unlock()
	saveFilter()

	data := filterConfigData(i.GuildID)
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: data.Embeds, Components: data.Components},
	})
}

// keepUnmarked returns items whose index is not in drop.
func keepUnmarked(items []string, drop map[int]bool) []string {
	out := make([]string, 0, len(items))
	for i, it := range items {
		if !drop[i] {
			out = append(out, it)
		}
	}
	return out
}

// filterConfigData renders the config embed plus a removal select menu (block +
// whitelist patterns, capped at Discord's 25-option limit). Ephemeral.
func filterConfigData(guildID string) *discordgo.InteractionResponseData {
	filterMu.RLock()
	var regexes, whitelist, watched []string
	notify, bypass := "", ""
	if g := filterCfg[guildID]; g != nil {
		regexes = append(regexes, g.Regexes...)
		whitelist = append(whitelist, g.Whitelist...)
		for ch := range g.Watched {
			watched = append(watched, "<#"+ch+">")
		}
		notify, bypass = g.NotifyChannel, g.BypassRole
	}
	filterMu.RUnlock()

	listField := func(name string, items []string) *discordgo.MessageEmbedField {
		val := "нет"
		if len(items) > 0 {
			lines := make([]string, len(items))
			for i, it := range items {
				lines[i] = fmt.Sprintf("%d. `%s`", i+1, it)
			}
			val = strings.Join(lines, "\n")
			if len([]rune(val)) > 1000 {
				val = truncate(val, 1000)
			}
		}
		return &discordgo.MessageEmbedField{Name: name, Value: val}
	}
	plain := func(v, empty string) string {
		if v == "" {
			return empty
		}
		return v
	}
	watchedVal := "нет"
	if len(watched) > 0 {
		watchedVal = strings.Join(watched, " ")
	}

	embed := &discordgo.MessageEmbed{
		Title: "🛡 Конфигурация фильтра",
		Fields: []*discordgo.MessageEmbedField{
			listField("Блок-паттерны", regexes),
			listField("Вайтлист", whitelist),
			{Name: "Каналы", Value: watchedVal},
			{Name: "Канал логов", Value: plain(mention(notify, "#"), "не задан")},
			{Name: "Роль обхода", Value: plain(mention(bypass, "@&"), "не задана")},
		},
	}

	var opts []discordgo.SelectMenuOption
	for i, p := range regexes {
		if len(opts) >= 25 {
			break
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate("🔧 "+p, 100), Value: fmt.Sprintf("re:%d", i)})
	}
	for i, p := range whitelist {
		if len(opts) >= 25 {
			break
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate("✅ "+p, 100), Value: fmt.Sprintf("wl:%d", i)})
	}
	var comps []discordgo.MessageComponent
	if len(opts) > 0 {
		minOne := 1
		comps = []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				MenuType:    discordgo.StringSelectMenu,
				CustomID:    filterRemoveID,
				Placeholder: "Удалить паттерн(ы)",
				MinValues:   &minOne,
				MaxValues:   len(opts),
				Options:     opts,
			},
		}}}
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{embed}, Components: comps, Flags: discordgo.MessageFlagsEphemeral}
}

// mention wraps a snowflake as a channel/role mention, or "" if empty.
func mention(id, prefix string) string {
	if id == "" {
		return ""
	}
	return "<" + prefix + id + ">"
}
