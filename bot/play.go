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

	// Retry the initial connection so we tolerate Lavalink still booting (e.g.
	// when both containers start together under docker-compose). Only fall back
	// to the legacy player if it never comes up.
	connected := false
	for attempt := 1; attempt <= 20; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		log.Printf("Lavalink connection attempt %d/20 failed: %v", attempt, err)
		client.RemoveNode("main")
		time.Sleep(2 * time.Second)
	}
	if !connected {
		log.Println("could not connect to Lavalink node; using legacy player")
		return
	}

	// Forward Discord voice gateway updates to Lavalink so it can establish the
	// voice connection on our behalf.
	b.s.AddHandler(b.onVoiceStateUpdate)
	b.s.AddHandler(b.onVoiceServerUpdate)

	LavalinkClient = client
	log.Println("Lavalink connected at", address)
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

// Custom IDs of the buttons on now-playing messages.
const (
	skipButtonID   = "np_skip"
	repeatButtonID = "np_repeat"
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
	repeatID := repeatButtonID
	if uri := trackURI(t); uri != "" {
		if id := repeatButtonID + ":" + uri; len(id) <= 100 {
			repeatID = id // short URI (YouTube etc.): embed directly, stateless
		} else {
			// Long URI (e.g. an uploaded file's signed CDN URL): stash it under a
			// short token so the button stays within Discord's 100-char cap.
			tok := strconv.FormatUint(repeatCounter.Add(1), 36)
			repeatStore.Store(tok, repeatTrack{identifier: uri, displayName: t.Info.Title})
			repeatID = repeatButtonID + ":#" + tok
		}
	}
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Skip ⏭️",
				Style:    discordgo.SecondaryButton,
				CustomID: skipButtonID,
			},
			discordgo.Button{
				Label:    "Repeat 🔁",
				Style:    discordgo.SecondaryButton,
				CustomID: repeatID,
			},
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

// onComponent handles message-component interactions: the now-playing Skip and
// Repeat buttons.
func (b *Bot) onComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Acknowledge the click without changing the clicked message.
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if LavalinkClient == nil {
		return
	}
	customID := i.MessageComponentData().CustomID
	switch {
	case customID == skipButtonID:
		if err := b.SkipLavalink(i.GuildID); err != nil {
			b.ephemeralFollowup(s, i, "Сейчас нечего пропускать.")
		}
	case strings.HasPrefix(customID, repeatButtonID):
		b.onRepeatButton(s, i, customID)
	}
}

// onRepeatButton handles the now-playing Repeat button. If the song this button
// belongs to is the one currently playing, it toggles single-track repeat.
// Otherwise (a different song is playing, or playback has finished) it plays the
// button's song now — or queues it behind whatever is currently playing.
func (b *Bot) onRepeatButton(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	payload, ok := strings.CutPrefix(customID, repeatButtonID+":")
	if !ok {
		payload = ""
	}
	// A "#"-prefixed payload is a token into repeatStore (long/file URIs);
	// anything else is the track's URI embedded directly.
	identifier, displayName := payload, ""
	if tok, isToken := strings.CutPrefix(payload, "#"); isToken {
		identifier = ""
		if v, found := repeatStore.Load(tok); found {
			rt := v.(repeatTrack)
			identifier, displayName = rt.identifier, rt.displayName
		}
	}

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
	if !event.Reason.MayStartNext() {
		return
	}

	guildID := event.GuildID().String()
	queue := LavalinkQueues.Get(guildID)

	var (
		next lavalink.Track
		ok   bool
	)
	switch queue.Type {
	case QueueTypeRepeatTrack:
		next, ok = event.Track, true
	case QueueTypeRepeatQueue:
		queue.Add(event.Track)
		next, ok = queue.Next()
	default: // QueueTypeNormal
		next, ok = queue.Next()
	}
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

	identifier, displayName, ok := b.resolveIdentifier(s, i)
	if !ok {
		return
	}

	// The user has to be in a voice channel for us to join.
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		respond(s, i, "You need to be in a voice channel!")
		return
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Println("error deferring interaction:", err)
		return
	}

	b.loadAndPlay(i.GuildID, voiceState.ChannelID, identifier, displayName, func(m string) {
		editInteraction(s, i, m)
	})
}

// loadAndPlay resolves an identifier on the Lavalink node and either starts it
// (if nothing is playing) or queues it. Status goes through the report callback
// so the slash command (interaction edit) and the voice command (channel
// message) share this core. Caller must have checked LavalinkClient != nil.
func (b *Bot) loadAndPlay(guildID, voiceChannelID, identifier, displayName string, report func(string)) {
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

	b.loadAndPlay(guildID, voiceChannelID, identifier, "", report)
}

// resolveIdentifier turns the /play options (a query string or an uploaded
// file) into a Lavalink identifier. It reports false (after responding to the
// user) when no usable input was given.
func (b *Bot) resolveIdentifier(s *discordgo.Session, i *discordgo.InteractionCreate) (identifier, displayName string, ok bool) {
	data := i.ApplicationCommandData()

	var query string
	var file *discordgo.MessageAttachment
	for _, opt := range data.Options {
		switch opt.Name {
		case "query":
			query = opt.StringValue()
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
			return "", "", false
		}
		return id, "", true
	case file != nil:
		if file.Size > maxFileBytes {
			respond(s, i, fmt.Sprintf("Файл слишком большой: %d МБ (максимум %d МБ).", file.Size>>20, maxFileBytes>>20))
			return "", "", false
		}
		return file.URL, file.Filename, true
	default:
		respond(s, i, "No music data given")
		return "", "", false
	}
}

// searchSuffix is appended to free-text searches to bias YouTube toward the
// actual track (a lyrics / official-audio upload) over covers, live takes,
// reactions, etc.
const searchSuffix = " lyrics"

// queryToIdentifier turns a raw query (typed or spoken) into a Lavalink
// identifier: Spotify links become a YouTube search, URLs and "xxsearch:"
// queries pass through, everything else becomes a YouTube search.
func queryToIdentifier(query string) (string, error) {
	if checkSubstrings(query, "open.spotify", "spotify.com") {
		song, err := getSpotifyLinkName(query)
		if err != nil {
			return "", err
		}
		search := song.Name
		if len(song.Artist) > 0 {
			search += " " + song.Artist[0].Name + " lyrics"
		}
		return lavalink.SearchTypeYouTube.Apply(search), nil
	}
	if urlPattern.MatchString(query) || searchPattern.MatchString(query) {
		return query, nil
	}
	return lavalink.SearchTypeYouTube.Apply(query + searchSuffix), nil
}
