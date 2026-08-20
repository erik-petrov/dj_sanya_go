package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
)

// LavalinkClient is the disgolink client used to talk to the Lavalink node. It
// stays nil until a node is successfully connected in setupLavalink; while nil
// the music commands reply that playback is unavailable.
var LavalinkClient disgolink.Client

// LavalinkQueues holds the per-guild track queue for Lavalink playback.
var LavalinkQueues = &QueueManager{queues: make(map[string]*LavalinkQueue)}

// announceChannels maps a guild ID to the text channel where /play was last
// used, so "now playing" messages for that guild go to the right place.
var announceChannels sync.Map

var (
	// urlPattern matches anything that already looks like a URL.
	urlPattern = regexp.MustCompile(`^https?://[-a-zA-Z0-9+&@#/%?=~_|!:,.;]*[-a-zA-Z0-9+&@#/%=~_|]?`)
	// searchPattern matches Lavalink search prefixes such as "ytsearch:" or "scsearch:".
	searchPattern = regexp.MustCompile(`^(.{2})search:(.+)`)
)

// respond replies to a slash-command interaction with a plain text message.
// The reply is shown in the same channel the interaction was triggered in,
// which is the default behaviour of an interaction response.
func respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
	if err != nil {
		log.Println("error responding to interaction:", err)
	}
}

// sendToChannel posts a standalone message in the same channel the interaction
// happened in, independent of the interaction's response lifecycle.
func sendToChannel(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if _, err := s.ChannelMessageSend(i.ChannelID, content); err != nil {
		log.Println("error sending message to channel:", err)
	}
}

// QueueType controls how the queue behaves once a track finishes.
type QueueType string

const (
	QueueTypeNormal      QueueType = "normal"
	QueueTypeRepeatTrack QueueType = "repeat_track"
	QueueTypeRepeatQueue QueueType = "repeat_queue"
)

// LavalinkQueue is a single guild's queue of Lavalink tracks.
type LavalinkQueue struct {
	mu     sync.Mutex
	tracks []lavalink.Track
	Type   QueueType
}

func (q *LavalinkQueue) Add(tracks ...lavalink.Track) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tracks = append(q.tracks, tracks...)
}

// Next pops the next track off the front of the queue.
func (q *LavalinkQueue) Next() (lavalink.Track, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tracks) == 0 {
		return lavalink.Track{}, false
	}
	track := q.tracks[0]
	q.tracks = q.tracks[1:]
	return track, true
}

func (q *LavalinkQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tracks = nil
}

// List returns a copy of the queued tracks, safe to range over.
func (q *LavalinkQueue) List() []lavalink.Track {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]lavalink.Track(nil), q.tracks...)
}

// QueueManager owns the queues for every guild the bot plays in.
type QueueManager struct {
	mu     sync.Mutex
	queues map[string]*LavalinkQueue
}

// Get returns the guild's queue, creating an empty one on first use.
func (m *QueueManager) Get(guildID string) *LavalinkQueue {
	m.mu.Lock()
	defer m.mu.Unlock()
	queue, ok := m.queues[guildID]
	if !ok {
		queue = &LavalinkQueue{Type: QueueTypeNormal}
		m.queues[guildID] = queue
	}
	return queue
}

func (m *QueueManager) Delete(guildID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.queues, guildID)
}

// setupLavalink builds the disgolink client, registers the player event
// listeners, wires Discord voice updates through to Lavalink and connects to
// the node described by the LAVALINK_* environment variables. If no node is
// configured or the connection fails it logs and returns, leaving
// LavalinkClient nil so the bot keeps using the legacy player. Must be called
// after the Discord session is open (it needs the bot's user ID).
func (b *Bot) setupLavalink() {
	address := os.Getenv("LAVALINK_ADDRESS")
	if address == "" {
		log.Println("LAVALINK_ADDRESS not set; Lavalink disabled, using legacy player")
		return
	}

	client := disgolink.New(snowflake.MustParse(b.s.State.User.ID),
		disgolink.WithListenerFunc(b.onTrackEnd),
		disgolink.WithListenerFunc(b.onTrackException),
		disgolink.WithListenerFunc(b.onTrackStuck),
	)

	password := os.Getenv("LAVALINK_PASSWORD")
	if password == "" {
		password = "youshallnotpass"
	}
	secure, _ := strconv.ParseBool(os.Getenv("LAVALINK_SECURE"))

	// Retry the initial connection so we tolerate Lavalink still booting when
	// both containers start together under docker-compose. A cold start that
	// downloads plugins (e.g. LavaSrc) can take a couple of minutes — well past
	// the container being "up" — so keep trying for a few minutes before falling
	// back to the legacy player.
	connected := false
	deadline := time.Now().Add(3 * time.Minute)
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := client.AddNode(ctx, disgolink.NodeConfig{
			Name:     "main",
			Address:  address,
			Password: password,
			Secure:   secure,
		})
		cancel()
		if err == nil {
			connected = true
			break
		}
		log.Printf("Lavalink connection attempt %d failed: %v", attempt, err)
		client.RemoveNode("main")
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if !connected {
		log.Println("could not connect to Lavalink node after 3m; using legacy player")
		return
	}

	// Forward Discord voice gateway updates to Lavalink so it can establish the
	// voice connection on our behalf.
	b.s.AddHandler(b.onVoiceStateUpdate)
	b.s.AddHandler(b.onVoiceServerUpdate)

	LavalinkClient = client
	log.Println("Lavalink connected at", address)

	// An ungraceful prior shutdown (SIGKILL) can leave the bot ghosted into a
	// voice channel; the next play then binds to that stale session and is
	// silent. Once the guild voice states have arrived, leave any channel we're
	// "in" without an active player — that can only be such a ghost.
	go func() {
		time.Sleep(5 * time.Second)
		me := b.s.State.User.ID
		for _, g := range b.s.State.Guilds {
			vs, err := b.s.State.VoiceState(g.ID, me)
			if err != nil || vs == nil || vs.ChannelID == "" {
				continue
			}
			if LavalinkClient.ExistingPlayer(snowflake.MustParse(g.ID)) != nil {
				continue // a real play created a player — not a ghost
			}
			log.Printf("clearing stale voice state in guild %s", g.ID)
			_ = b.s.ChannelVoiceJoinManual(g.ID, "", false, false)
		}
	}()
}

// onVoiceStateUpdate forwards our own voice state changes to Lavalink and drops
// the queue when we leave a channel.
func (b *Bot) onVoiceStateUpdate(s *discordgo.Session, event *discordgo.VoiceStateUpdate) {
	if event.UserID != s.State.User.ID {
		return
	}

	var channelID *snowflake.ID
	if event.ChannelID != "" {
		id := snowflake.MustParse(event.ChannelID)
		channelID = &id
	}
	LavalinkClient.OnVoiceStateUpdate(context.TODO(), snowflake.MustParse(event.GuildID), channelID, event.SessionID)

	if event.ChannelID == "" {
		// Dropped the voice link (stop / skip-to-empty / kicked): clear the
		// queue but KEEP the announce channel, so a later play still posts its
		// "now playing" there. A full teardown (/unlisten, auto-leave) clears
		// the announce channel itself via leaveVoice.
		LavalinkQueues.Delete(event.GuildID)
	}
}

// onVoiceServerUpdate forwards Discord voice server updates to Lavalink.
func (b *Bot) onVoiceServerUpdate(s *discordgo.Session, event *discordgo.VoiceServerUpdate) {
	LavalinkClient.OnVoiceServerUpdate(context.TODO(), snowflake.MustParse(event.GuildID), event.Token, event.Endpoint)
}

// nowPlayingMsg formats a "now playing" line, appending a masked "Video link"
// hyperlink to the track's URL when Lavalink provides one.
func nowPlayingMsg(t lavalink.Track) string {
	msg := "▶️ Playing: `" + t.Info.Title + "`"
	if t.Info.URI != nil && *t.Info.URI != "" {
		msg += "\n[Video link](" + *t.Info.URI + ")"
	}
	return msg
}

// Custom IDs of the interactive controls on now-playing messages and the
// deep-search picker.
const (
	skipButtonID    = "np_skip"
	repeatButtonID  = "np_repeat"
	pauseButtonID   = "np_pause"
	restartButtonID = "np_restart"
	stopButtonID    = "np_stop"
	queueButtonID   = "np_queue"
	pickMenuID      = "np_pick"
)

// maxFileBytes caps the size of an uploaded file we'll try to play; larger ones
// are rejected with a message instead of failing silently. Matches Discord's
// default (non-boost) upload limit.
const maxFileBytes = 10 << 20 // 10 MB

// repeatStore maps a short token to a track's replay info, used by the Repeat
// button when the track's URI is too long to embed in a Discord custom ID (100
// char cap) — e.g. uploaded files, whose signed CDN URLs are long. Tokens are
// process-local, so buttons on old messages stop working after a restart.
var (
	repeatStore   sync.Map // token(string) -> repeatTrack
	repeatCounter atomic.Uint64
)

type repeatTrack struct {
	identifier  string // what to load to replay the track (its URI/URL)
	displayName string // title override for the replay's now-playing ("" = none)
}

// trackURI returns a track's URI, or "" when it has none.
func trackURI(t lavalink.Track) string {
	if t.Info.URI != nil {
		return *t.Info.URI
	}
	return ""
}

// nowPlayingComponents returns the interactive controls attached to a
// now-playing message: Skip and Repeat. The Repeat button carries the track's
// URI so it can replay the song when clicked after playback has finished.
func nowPlayingComponents(t lavalink.Track) []discordgo.MessageComponent {
	// Repeat/Restart need to know which track their message is about: embed the
	// URI in the custom ID when it fits the 100-char cap (measured against the
	// longest prefix), else stash it under a short token.
	payload := ""
	if uri := trackURI(t); uri != "" {
		if len(restartButtonID)+1+len(uri) <= 100 {
			payload = ":" + uri // short URI (YouTube etc.): embed directly, stateless
		} else {
			// Long URI (e.g. an uploaded file's signed CDN URL): stash it under a
			// short token so the button stays within Discord's 100-char cap.
			tok := strconv.FormatUint(repeatCounter.Add(1), 36)
			repeatStore.Store(tok, repeatTrack{identifier: uri, displayName: t.Info.Title})
			payload = ":#" + tok
		}
	}
	btn := func(label, id string) discordgo.Button {
		return discordgo.Button{Label: label, Style: discordgo.SecondaryButton, CustomID: id}
	}
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			btn("Pause ⏸️", pauseButtonID),
			btn("Skip ⏭️", skipButtonID),
			btn("Repeat 🔁", repeatButtonID+payload),
		}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			btn("Restart ⏮️", restartButtonID+payload),
			btn("Stop ⏹️", stopButtonID),
			btn("Queue 📋", queueButtonID),
		}},
	}
}

// sendNowPlaying posts the now-playing message — with a Skip button — to the
// guild's announce channel, when one is set.
func (b *Bot) sendNowPlaying(guildID string, t lavalink.Track) {
	ch, ok := announceChannels.Load(guildID)
	if !ok {
		return
	}
	_, _ = b.s.ChannelMessageSendComplex(ch.(string), &discordgo.MessageSend{
		Content:    nowPlayingMsg(t),
		Components: nowPlayingComponents(t),
	})
}

// onComponent routes message-component interactions: the now-playing buttons
// and the deep-search picker. Most handlers just ack the click; pause and the
// picker instead respond by editing the clicked message, so they self-ack.
func (b *Bot) onComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	name, payload, _ := strings.Cut(customID, ":")

	// The /filter-config removal menu is independent of music playback.
	if name == filterRemoveID {
		b.onFilterRemove(s, i)
		return
	}

	ack := func() {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})
	}

	if LavalinkClient == nil {
		ack()
		return
	}

	switch name {
	case skipButtonID:
		ack()
		if err := b.SkipLavalink(i.GuildID); err != nil {
			b.ephemeralFollowup(s, i, "Сейчас нечего пропускать.")
		}
	case repeatButtonID:
		ack()
		b.onRepeatButton(s, i, payload)
	case restartButtonID:
		ack()
		b.onRestartButton(s, i, payload)
	case stopButtonID:
		ack()
		if err := b.StopLavalink(i.GuildID); err != nil {
			b.ephemeralFollowup(s, i, "Не получилось остановить: "+err.Error())
		} else {
			b.ephemeralFollowup(s, i, "⏹️ Остановлено")
		}
	case queueButtonID:
		ack()
		b.ephemeralFollowup(s, i, queueText(i.GuildID))
	case pauseButtonID:
		b.onPauseButton(s, i)
	case pickMenuID:
		b.onPickMenu(s, i, payload)
	default:
		ack()
	}
}

// resolveRepeatPayload turns a Repeat/Restart button payload — a track URI, or
// "#token" into repeatStore for long URIs — into a playable identifier plus an
// optional display-name override (uploaded files keep their filename).
func resolveRepeatPayload(payload string) (identifier, displayName string) {
	if tok, isToken := strings.CutPrefix(payload, "#"); isToken {
		if v, found := repeatStore.Load(tok); found {
			rt := v.(repeatTrack)
			return rt.identifier, rt.displayName
		}
		return "", ""
	}
	return payload, ""
}

// onRepeatButton handles the now-playing Repeat button. If the song this button
// belongs to is the one currently playing, it toggles single-track repeat.
// Otherwise (a different song is playing, or playback has finished) it plays the
// button's song now — or queues it behind whatever is currently playing.
func (b *Bot) onRepeatButton(s *discordgo.Session, i *discordgo.InteractionCreate, payload string) {
	identifier, displayName := resolveRepeatPayload(payload)

	// Toggle repeat only when THIS button's song is the current track.
	if identifier != "" && identifier == b.currentTrackURI(i.GuildID) {
		if b.ToggleRepeat(i.GuildID) {
			b.ephemeralFollowup(s, i, "🔁 Повтор текущего трека включён")
		} else {
			b.ephemeralFollowup(s, i, "🔁 Повтор выключен")
		}
		return
	}

	if identifier == "" {
		b.ephemeralFollowup(s, i, "Нечего повторить.")
		return
	}
	// Not the current track: play it now if idle, or queue it behind what's
	// playing (loadAndPlay decides). Report the result back to the clicker only.
	ch := b.botVoiceChannel(i.GuildID)
	if ch == "" {
		if vs, err := s.State.VoiceState(i.GuildID, i.Member.User.ID); err == nil && vs != nil {
			ch = vs.ChannelID
		}
	}
	if ch == "" {
		b.ephemeralFollowup(s, i, "Зайдите в голосовой канал, чтобы повторить.")
		return
	}
	go b.loadAndPlay(i.GuildID, ch, identifier, displayName, func(m string) { b.ephemeralFollowup(s, i, m) })
}

// onRestartButton seeks the current track back to 0:00 — only when the clicked
// message's track is the one actually playing.
func (b *Bot) onRestartButton(s *discordgo.Session, i *discordgo.InteractionCreate, payload string) {
	identifier, _ := resolveRepeatPayload(payload)
	if identifier == "" || identifier != b.currentTrackURI(i.GuildID) {
		b.ephemeralFollowup(s, i, "Этот трек сейчас не играет.")
		return
	}
	player := LavalinkClient.ExistingPlayer(snowflake.MustParse(i.GuildID))
	if player == nil {
		b.ephemeralFollowup(s, i, "Сейчас ничего не играет.")
		return
	}
	if err := player.Update(context.TODO(), lavalink.WithPosition(0)); err != nil {
		b.ephemeralFollowup(s, i, "Не получилось перемотать: "+err.Error())
		return
	}
	b.ephemeralFollowup(s, i, "⏮️ Сначала")
}

// onPauseButton toggles pause and flips the clicked message's button label
// between "Pause ⏸️" and "Resume ▶️" via an in-place message update.
func (b *Bot) onPauseButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ack := func() {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})
	}
	player := LavalinkClient.ExistingPlayer(snowflake.MustParse(i.GuildID))
	if player == nil || player.Track() == nil {
		ack()
		b.ephemeralFollowup(s, i, "Сейчас ничего не играет.")
		return
	}
	paused := !player.Paused()
	if err := player.Update(context.TODO(), lavalink.WithPaused(paused)); err != nil {
		ack()
		b.ephemeralFollowup(s, i, "Не получилось: "+err.Error())
		return
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    i.Message.Content,
			Components: flipPauseLabel(i.Message.Components, paused),
		},
	}); err != nil {
		log.Println("error updating pause button:", err)
	}
}

// flipPauseLabel returns the message's components with the pause button's label
// reflecting the new paused state. Components from a received message are
// pointers, so mutating in place is safe here.
func flipPauseLabel(components []discordgo.MessageComponent, paused bool) []discordgo.MessageComponent {
	label := "Pause ⏸️"
	if paused {
		label = "Resume ▶️"
	}
	for _, c := range components {
		row, ok := c.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, rc := range row.Components {
			if btn, ok := rc.(*discordgo.Button); ok && btn.CustomID == pauseButtonID {
				btn.Label = label
			}
		}
	}
	return components
}

// queueText renders the guild's queue, shared by /queue and the Queue button.
func queueText(guildID string) string {
	tracks := LavalinkQueues.Get(guildID).List()
	if len(tracks) == 0 {
		return "Queue is empty"
	}
	out := "Current queue:\n"
	for idx, track := range tracks {
		uri := ""
		if track.Info.URI != nil {
			uri = *track.Info.URI
		}
		out += fmt.Sprintf("%d: [%s](%s)\n", idx+1, track.Info.Title, uri)
	}
	return out
}

// pickSession holds one deep-search's results while the requester chooses.
type pickSession struct {
	tracks         []lavalink.Track
	voiceChannelID string
}

var searchPicks sync.Map // token(string) -> pickSession

// deepSearch handles /play with deep:true — instead of auto-playing the first
// hit, it shows the requester an ephemeral dropdown of the top-10 results.
// Searches the raw query (no "песня lyrics" bias): the user picks manually.
func (b *Bot) deepSearch(s *discordgo.Session, i *discordgo.InteractionCreate, query, voiceChannelID string) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		log.Println("error deferring deep search:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var results []lavalink.Track
	loadErr := ""
	LavalinkClient.BestNode().LoadTracksHandler(ctx, lavalink.SearchTypeYouTube.Apply(query), disgolink.NewResultHandler(
		func(track lavalink.Track) { results = []lavalink.Track{track} },
		func(playlist lavalink.Playlist) { results = playlist.Tracks },
		func(tracks []lavalink.Track) { results = tracks },
		func() {},
		func(err error) { loadErr = err.Error() },
	))
	if loadErr != "" {
		editInteraction(s, i, "Error while looking up query: `"+loadErr+"`")
		return
	}
	if len(results) == 0 {
		editInteraction(s, i, "Nothing found for: `"+query+"`")
		return
	}
	if len(results) > 10 {
		results = results[:10]
	}

	tok := strconv.FormatUint(repeatCounter.Add(1), 36)
	searchPicks.Store(tok, pickSession{tracks: results, voiceChannelID: voiceChannelID})

	opts := make([]discordgo.SelectMenuOption, len(results))
	for idx, t := range results {
		opts[idx] = discordgo.SelectMenuOption{
			Label:       truncate(fmt.Sprintf("%d. %s", idx+1, t.Info.Title), 100),
			Value:       strconv.Itoa(idx),
			Description: truncate(t.Info.Author+" · "+fmtDuration(t.Info.Length), 100),
		}
	}
	content := fmt.Sprintf("🔎 Топ-%d по запросу `%s`:", len(results), query)
	comps := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.SelectMenu{
			MenuType:    discordgo.StringSelectMenu,
			CustomID:    pickMenuID + ":" + tok,
			Placeholder: "Выбери трек",
			Options:     opts,
		},
	}}}
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    &content,
		Components: &comps,
	}); err != nil {
		log.Println("error sending deep-search menu:", err)
	}
}

// onPickMenu handles a deep-search dropdown selection: replaces the picker with
// a confirmation and plays or queues the chosen track.
func (b *Bot) onPickMenu(s *discordgo.Session, i *discordgo.InteractionCreate, token string) {
	expired := func() {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Выбор устарел — запусти `/play` с deep ещё раз.",
				Components: []discordgo.MessageComponent{},
			},
		})
	}

	v, found := searchPicks.LoadAndDelete(token)
	values := i.MessageComponentData().Values
	if !found || len(values) == 0 {
		expired()
		return
	}
	ps := v.(pickSession)
	idx, err := strconv.Atoi(values[0])
	if err != nil || idx < 0 || idx >= len(ps.tracks) {
		expired()
		return
	}
	track := ps.tracks[idx]

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "✅ Выбрано: `" + track.Info.Title + "`",
			Components: []discordgo.MessageComponent{},
		},
	})

	announceID := ""
	if ch, ok := announceChannels.Load(i.GuildID); ok {
		announceID, _ = ch.(string)
	}
	report := func(m string) {
		if announceID != "" {
			_, _ = b.s.ChannelMessageSend(announceID, m)
		}
	}
	go b.startOrQueue(i.GuildID, ps.voiceChannelID, track, report)
}

// startOrQueue plays an already-resolved track immediately (joining voice) when
// nothing is playing, or appends it to the queue otherwise.
func (b *Bot) startOrQueue(guildID, voiceChannelID string, track lavalink.Track, report func(string)) {
	player := GetOrCreatePlayer(guildID)
	if player == nil {
		report("Music is not available right now.")
		return
	}
	if player.Track() != nil {
		LavalinkQueues.Get(guildID).Add(track)
		report("Добавил в очередь: `" + track.Info.Title + "`")
		return
	}
	if err := b.s.ChannelVoiceJoinManual(guildID, voiceChannelID, false, true); err != nil {
		report("Error joining voice channel: " + err.Error())
		return
	}
	if err := player.Update(context.TODO(), lavalink.WithTrack(track)); err != nil {
		report("Error playing track: " + err.Error())
		return
	}
	b.sendNowPlaying(guildID, track)
}

// truncate caps a string at max runes (Discord limits labels to 100).
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// fmtDuration renders a track length as m:ss.
func fmtDuration(d lavalink.Duration) string {
	return fmt.Sprintf("%d:%02d", d.Minutes(), d.SecondsPart())
}

// currentTrackURI returns the URI of the track the guild's player is currently
// playing, or "" if nothing is playing or the track has no URI.
func (b *Bot) currentTrackURI(guildID string) string {
	p := LavalinkClient.ExistingPlayer(snowflake.MustParse(guildID))
	if p == nil {
		return ""
	}
	t := p.Track()
	if t == nil {
		return ""
	}
	return trackURI(*t)
}

// ephemeralFollowup sends a follow-up message visible only to the user who
// clicked the button.
func (b *Bot) ephemeralFollowup(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: msg,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

// ToggleRepeat flips single-track repeat for a guild and returns whether repeat
// is now on.
func (b *Bot) ToggleRepeat(guildID string) bool {
	queue := LavalinkQueues.Get(guildID)
	if queue.Type == QueueTypeRepeatTrack {
		queue.Type = QueueTypeNormal
		return false
	}
	queue.Type = QueueTypeRepeatTrack
	return true
}

// onTrackEnd advances the queue when a track finishes naturally.
func (b *Bot) onTrackEnd(player disgolink.Player, event lavalink.TrackEndEvent) {
	// Only a clean finish advances the queue. A loadFailed used to skip to the
	// next track (MayStartNext is true for it), which cascades through the whole
	// queue when YouTube gates everything — don't treat "couldn't play" as a skip.
	if event.Reason != lavalink.TrackEndReasonFinished {
		return
	}

	guildID := event.GuildID().String()
	queue := LavalinkQueues.Get(guildID)

	// Single-track repeat: replay the same track (don't re-announce a loop).
	if queue.Type == QueueTypeRepeatTrack {
		if err := player.Update(context.TODO(), lavalink.WithTrack(event.Track)); err != nil {
			log.Println("failed to repeat track:", err)
		}
		return
	}

	if queue.Type == QueueTypeRepeatQueue {
		queue.Add(event.Track)
	}
	next, ok := queue.Next()
	if !ok {
		return
	}
	if err := player.Update(context.TODO(), lavalink.WithTrack(next)); err != nil {
		log.Println("failed to play next track:", err)
		return
	}
	b.sendNowPlaying(guildID, next)
}

func (b *Bot) onTrackException(player disgolink.Player, event lavalink.TrackExceptionEvent) {
	log.Printf("track exception in guild %s: %+v", event.GuildID(), event)
	// A failure here is often a stale/missing poToken (e.g. Lavalink restarted
	// without one). Kick a rate-limited refresh so playback self-heals.
	b.refreshPoTokenOnDemand()
}

func (b *Bot) onTrackStuck(player disgolink.Player, event lavalink.TrackStuckEvent) {
	log.Printf("track stuck in guild %s: %+v", event.GuildID(), event)
}

// GetOrCreatePlayer returns the Lavalink player for a guild, creating one if it
// does not exist yet. Returns nil when Lavalink is disabled.
func GetOrCreatePlayer(guildID string) disgolink.Player {
	if LavalinkClient == nil {
		return nil
	}
	return LavalinkClient.Player(snowflake.MustParse(guildID))
}

// StopLavalink clears the queue, stops playback and disconnects from voice.
func (b *Bot) StopLavalink(guildID string) error {
	LavalinkQueues.Get(guildID).Clear()
	if player := LavalinkClient.ExistingPlayer(snowflake.MustParse(guildID)); player != nil {
		if err := player.Update(context.TODO(), lavalink.WithNullTrack()); err != nil {
			return err
		}
	}
	// Leaving the channel ("" channel ID) tears down the Lavalink voice link.
	return b.s.ChannelVoiceJoinManual(guildID, "", false, false)
}

// PauseLavalink pauses or resumes the guild's player.
func (b *Bot) PauseLavalink(guildID string, paused bool) error {
	player := LavalinkClient.ExistingPlayer(snowflake.MustParse(guildID))
	if player == nil {
		return errors.New("no active player")
	}
	return player.Update(context.TODO(), lavalink.WithPaused(paused))
}

// SkipLavalink jumps to the next queued track, or stops if the queue is empty.
func (b *Bot) SkipLavalink(guildID string) error {
	player := LavalinkClient.ExistingPlayer(snowflake.MustParse(guildID))
	if player == nil {
		return errors.New("no active player")
	}
	next, ok := LavalinkQueues.Get(guildID).Next()
	if !ok {
		return player.Update(context.TODO(), lavalink.WithNullTrack())
	}
	if err := player.Update(context.TODO(), lavalink.WithTrack(next)); err != nil {
		return err
	}
	b.sendNowPlaying(guildID, next)
	return nil
}

// onPlay handles the /play command: it loads the requested
// track/playlist/search through the node and plays (or queues) it.
func (b *Bot) onPlay(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if LavalinkClient == nil {
		respond(s, i, "Music is not available right now.")
		return
	}

	announceChannels.Store(i.GuildID, i.ChannelID)

	identifier, displayName, rawQuery, deep, ok := b.resolveIdentifier(s, i)
	if !ok {
		return
	}

	// The user has to be in a voice channel for us to join.
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		respond(s, i, "You need to be in a voice channel!")
		return
	}

	// deep:true on a text query → let the user pick from the top-10 instead of
	// auto-playing the first hit.
	if deep && rawQuery != "" {
		b.deepSearch(s, i, rawQuery, voiceState.ChannelID)
		return
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Println("error deferring interaction:", err)
		return
	}

	b.playWithFallback(i.GuildID, voiceState.ChannelID, identifier, displayName, rawQuery, func(m string) {
		editInteraction(s, i, m)
	})
}

// playWithFallback plays identifier and, when a Cyrillic free-text query found
// nothing, retries once with the query transliterated to Latin — spoken English
// titles come out of the Russian STT as Cyrillic phonetics.
func (b *Bot) playWithFallback(guildID, voiceChannelID, identifier, displayName, rawQuery string, report func(string)) {
	if !b.loadAndPlay(guildID, voiceChannelID, identifier, displayName, report) {
		return
	}
	if rawQuery == "" || !hasCyrillic(rawQuery) {
		return
	}
	latin := anglicize(rawQuery)
	report("🔎 Пробую латиницей: `" + latin + "`")
	id, err := queryToIdentifier(latin)
	if err != nil {
		return
	}
	b.loadAndPlay(guildID, voiceChannelID, id, displayName, report)
}

// loadAndPlay resolves an identifier on the Lavalink node and either starts it
// (if nothing is playing) or queues it. Status goes through the report callback
// so the slash command (interaction edit) and the voice command (channel
// message) share this core. Caller must have checked LavalinkClient != nil.
func (b *Bot) loadAndPlay(guildID, voiceChannelID, identifier, displayName string, report func(string)) (notFound bool) {
	player := GetOrCreatePlayer(guildID)
	queue := LavalinkQueues.Get(guildID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var toPlay *lavalink.Track
	LavalinkClient.BestNode().LoadTracksHandler(ctx, identifier, disgolink.NewResultHandler(
		func(track lavalink.Track) {
			if displayName != "" {
				track.Info.Title = displayName // uploaded files: show the filename
			}
			report("Loaded: `" + track.Info.Title + "`")
			if player.Track() == nil {
				toPlay = &track
			} else {
				queue.Add(track)
			}
		},
		func(playlist lavalink.Playlist) {
			report(fmt.Sprintf("Loaded playlist `%s` with %d tracks", playlist.Info.Name, len(playlist.Tracks)))
			if player.Track() == nil && len(playlist.Tracks) > 0 {
				toPlay = &playlist.Tracks[0]
				queue.Add(playlist.Tracks[1:]...)
			} else {
				queue.Add(playlist.Tracks...)
			}
		},
		func(tracks []lavalink.Track) {
			report("Loaded: `" + tracks[0].Info.Title + "`")
			if player.Track() == nil {
				toPlay = &tracks[0]
			} else {
				queue.Add(tracks[0])
			}
		},
		func() {
			report("Nothing found for: `" + identifier + "`")
			notFound = true
		},
		func(err error) {
			report("Error while looking up query: `" + err.Error() + "`")
		},
	))

	if toPlay == nil {
		// Nothing found, or the track was queued behind the current one.
		return
	}

	if err := b.s.ChannelVoiceJoinManual(guildID, voiceChannelID, false, true); err != nil {
		report("Error joining voice channel: " + err.Error())
		return
	}

	if err := player.Update(context.TODO(), lavalink.WithTrack(*toPlay)); err != nil {
		report("Error playing track: " + err.Error())
		return
	}

	b.sendNowPlaying(guildID, *toPlay)
	return
}

// playQuery is the non-interaction entry point used by voice commands: resolve
// the spoken query and play it in voiceChannelID (the channel the ears bot is
// listening in). Status messages go to announceChannelID (may be empty).
func (b *Bot) playQuery(guildID, voiceChannelID, query, announceChannelID string) {
	report := func(m string) {
		if announceChannelID != "" {
			_, _ = b.s.ChannelMessageSend(announceChannelID, m)
		}
	}

	if LavalinkClient == nil {
		report("Music is not available right now.")
		return
	}
	if voiceChannelID == "" {
		report("You need to be in a voice channel!")
		return
	}

	identifier, err := queryToIdentifier(query)
	if err != nil {
		report("Something went wrong: " + err.Error())
		return
	}

	b.playWithFallback(guildID, voiceChannelID, identifier, "", query, report)
}

// resolveIdentifier turns the /play options (a query string or an uploaded
// file) into a Lavalink identifier. It reports false (after responding to the
// user) when no usable input was given.
func (b *Bot) resolveIdentifier(s *discordgo.Session, i *discordgo.InteractionCreate) (identifier, displayName, rawQuery string, deep, ok bool) {
	data := i.ApplicationCommandData()

	var query string
	var file *discordgo.MessageAttachment
	for _, opt := range data.Options {
		switch opt.Name {
		case "query":
			query = opt.StringValue()
		case "deep":
			deep = opt.BoolValue()
		case "file":
			if id, ok := opt.Value.(string); ok {
				if f, ok := data.Resolved.Attachments[id]; ok {
					file = f
				}
			}
		}
	}

	switch {
	case query != "":
		id, err := queryToIdentifier(query)
		if err != nil {
			respond(s, i, "Something went wrong: "+err.Error())
			return "", "", "", false, false
		}
		return id, "", query, deep, true
	case file != nil:
		if file.Size > maxFileBytes {
			respond(s, i, fmt.Sprintf("Файл слишком большой: %d МБ (максимум %d МБ).", file.Size>>20, maxFileBytes>>20))
			return "", "", "", false, false
		}
		return file.URL, file.Filename, "", false, true
	default:
		respond(s, i, "No music data given")
		return "", "", "", false, false
	}
}

// searchSuffix is appended to free-text searches to bias YouTube toward the
// actual track (a lyrics / official-audio upload) over covers, live takes,
// reactions, etc.
const searchSuffix = " lyrics"

// queryToIdentifier turns a raw query (typed or spoken) into a Lavalink
// identifier: URLs (YouTube, Spotify, SoundCloud, Deezer, ...) and explicit
// "xxsearch:" prefixes pass straight through — Lavalink resolves them via the
// youtube / LavaSrc / native SoundCloud sources — while plain text becomes a
// YouTube search.
func queryToIdentifier(query string) (string, error) {
	if urlPattern.MatchString(query) || searchPattern.MatchString(query) {
		return query, nil
	}
	// Cyrillic queries get the Russian-flavoured suffix; pure-Latin ones (typed
	// English, or an anglicized retry) get a plain " lyrics".
	suffix := searchSuffix
	if !hasCyrillic(query) {
		suffix = " lyrics"
	}
	return lavalink.SearchTypeYouTube.Apply(query + suffix), nil
}
