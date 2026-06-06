package bot

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// earsListeners tracks an active listening session per guild: the ears bot's
// voice connection plus its receive goroutine.
var earsListeners sync.Map // guildID(string) -> *earsListener

type earsListener struct {
	guildID   string
	channelID string // the voice channel the ears bot is listening in
	vc        *discordgo.VoiceConnection
	stop      chan struct{}
	ssrcUser  sync.Map // uint32 SSRC -> string userID (best-effort; may be empty)
}

// setupEars opens the second ("ears") gateway session, used only to receive
// voice. If EARS_BOT_TOKEN is unset the feature stays off (b.ears stays nil) and
// the /listen command reports it as disabled. Must run after the main session
// is open (it needs nothing from it, but Start() owns the ordering).
func (b *Bot) setupEars() {
	if b.earsToken == "" {
		log.Println("EARS_BOT_TOKEN not set; voice listening disabled")
		return
	}
	s, err := discordgo.New("Bot " + b.earsToken)
	if err != nil {
		log.Println("could not create ears session; voice listening disabled:", err)
		return
	}
	// discordgo's default intents already include guild voice states; we just
	// need the State to cache voice so the connection works smoothly.
	s.State.TrackVoice = true
	if err := s.Open(); err != nil {
		log.Println("could not open ears session; voice listening disabled:", err)
		return
	}
	sttURL = os.Getenv("STT_URL")
	if sttURL == "" {
		sttURL = "http://localhost:8002"
	}
	b.ears = s
	log.Printf("ears session connected as %s; STT sidecar at %s", s.State.User.Username, sttURL)
}

// startListening makes the ears bot join a voice channel and begins logging who
// speaks and for how long. Phase 0: no transcription yet — this just proves the
// second session receives audio while Lavalink plays through the main one.
func (b *Bot) startListening(guildID, channelID string) error {
	if b.ears == nil {
		return errors.New("voice listening is disabled")
	}
	// Leave any existing session first (handles a channel change).
	_ = b.stopListening(guildID)

	vc, err := b.ears.ChannelVoiceJoin(guildID, channelID, true, false) // muted, NOT deaf
	if err != nil {
		return err
	}

	l := &earsListener{guildID: guildID, channelID: channelID, vc: vc, stop: make(chan struct{})}
	// Map each speaker's SSRC to their user ID as Discord announces it.
	vc.AddHandler(func(_ *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
		l.ssrcUser.Store(uint32(vs.SSRC), vs.UserID)
	})
	earsListeners.Store(guildID, l)

	go b.receiveLoop(l)
	return nil
}

// stopListening disconnects the ears bot from a guild's voice channel and stops
// its receive goroutine.
func (b *Bot) stopListening(guildID string) error {
	v, ok := earsListeners.LoadAndDelete(guildID)
	if !ok {
		return nil
	}
	l := v.(*earsListener)
	close(l.stop)
	return l.vc.Disconnect()
}

// receiveLoop reads Opus packets, groups them per speaker, and on a silence gap
// logs the utterance length and who spoke.
func (b *Bot) receiveLoop(l *earsListener) {
	const (
		silenceGap         = 800 * time.Millisecond // gap that ends an utterance
		minUtteranceFrames = 25                     // ignore blips shorter than ~0.5s
	)

	// OpusRecv is created during the voice handshake; wait briefly for it.
	for i := 0; i < 50 && l.vc.OpusRecv == nil; i++ {
		select {
		case <-l.stop:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	recv := l.vc.OpusRecv
	if recv == nil {
		log.Println("[ears] OpusRecv never initialized; receive loop aborting")
		return
	}

	type utterance struct {
		frames [][]byte
		last   time.Time
	}
	active := map[uint32]*utterance{}

	flush := func(ssrc uint32, u *utterance) {
		frames := u.frames
		delete(active, ssrc)
		if len(frames) < minUtteranceFrames {
			return
		}
		uid, _ := l.ssrcUser.Load(ssrc)
		userID, _ := uid.(string)
		go b.handleUtterance(l.guildID, l.channelID, userID, ssrc, frames)
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-l.stop:
			return
		case p, ok := <-recv:
			if !ok {
				return
			}
			if len(p.Opus) == 0 {
				continue
			}
			// Copy: the packet's buffer may be reused by discordgo.
			frame := make([]byte, len(p.Opus))
			copy(frame, p.Opus)
			u := active[p.SSRC]
			if u == nil {
				u = &utterance{}
				active[p.SSRC] = u
			}
			u.frames = append(u.frames, frame)
			u.last = time.Now()
		case <-ticker.C:
			now := time.Now()
			for ssrc, u := range active {
				if now.Sub(u.last) >= silenceGap {
					flush(ssrc, u)
				}
			}
		}
	}
}

// handleUtterance sends a finished utterance to the STT sidecar and logs the
// transcript. Phase 1a: log only — command dispatch comes in Phase 1b.
func (b *Bot) handleUtterance(guildID, voiceChannelID, userID string, ssrc uint32, frames [][]byte) {
	// Only one STT request at a time; if the sidecar is busy, drop this
	// utterance instead of queuing it (avoids a backlog of timing-out requests).
	select {
	case sttSem <- struct{}{}:
		defer func() { <-sttSem }()
	default:
		return
	}

	text, err := transcribe(frames)
	if err != nil {
		log.Println("[stt] transcription error:", err)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	who := userID
	if who == "" {
		who = fmt.Sprintf("ssrc %d", ssrc)
	}
	log.Printf("[stt] %s said: %q", who, text)

	// userID is best-effort (the speaking-event mapping is unreliable with the
	// DAVE fork), so we don't require it — playback targets voiceChannelID, the
	// channel the ears bot is already listening in.
	b.handleVoiceCommand(guildID, voiceChannelID, userID, text)
}

// onListen handles /listen: the ears bot joins the caller's voice channel.
func (b *Bot) onListen(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if b.ears == nil {
		respond(s, i, "Voice listening is disabled (no ears bot configured).")
		return
	}
	vs, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || vs.ChannelID == "" {
		respond(s, i, "You need to be in a voice channel!")
		return
	}
	// Ack within Discord's 3s window first: the voice + DAVE handshake can take
	// a moment, so defer and edit the reply once the join completes (otherwise
	// the late reply fails with "Unknown interaction" / 10062).
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Println("error deferring /listen:", err)
		return
	}
	msg := "🎤 Слушаю команды в этом канале."
	if err := b.startListening(i.GuildID, vs.ChannelID); err != nil {
		msg = "Couldn't start listening: " + err.Error()
	} else {
		// Route voice-command feedback (and now-playing notices) to this channel.
		announceChannels.Store(i.GuildID, i.ChannelID)
	}
	editInteraction(s, i, msg)
}

// onUnlisten handles /unlisten: the ears bot leaves voice.
func (b *Bot) onUnlisten(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if b.ears == nil {
		respond(s, i, "Voice listening is disabled.")
		return
	}
	if err := b.stopListening(i.GuildID); err != nil {
		respond(s, i, "Couldn't stop listening: "+err.Error())
		return
	}
	respond(s, i, "Stopped listening.")
}
