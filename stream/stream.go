package stream

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/erik-petrov/dj_sanya_go/dca"
)

var (
	ErrVoiceConnClosed = errors.New("voice connection closed")
	ErrPaused          = errors.New("paused by user")
	ErrStopped         = errors.New("stopped by user")
	ErrAlreadyRunning  = errors.New("a stream is already running")
	ErrStreamIsDone    = errors.New("stream is done")
	ErrSkipped         = errors.New("skipped by user")
)

// StreamingSession provides an easy way to directly transmit opus audio
// to discord from an encode session.
type StreamingSession struct {
	sync.Mutex

	// If this channel is not nil, an error will be sen when finished (or nil if no error)
	done chan error

	source dca.OpusReader
	vc     *discordgo.VoiceConnection

	repeat     bool
	skipped    bool
	end        bool //lol, i hate this but let it be
	paused     bool
	framesSent int

	finished bool
	running  bool
	err      error // If an error occured and we had to stop
}

// Creates a new stream from an Opusreader.
// source   : The source of the opus frames to be sent, either from an encoder or decoder.
// vc       : The voice connecion to stream to.
// done     : If not nil, an error will be sent on it when completed.
func NewStream(source dca.OpusReader, vc *discordgo.VoiceConnection, done chan error) *StreamingSession {
	session := &StreamingSession{
		source: source,
		vc:     vc,
		done:   done,
	}
	go session.stream()
	return session
}

func (s *StreamingSession) stream() error {
	// Check if we are already running and if so stop
	s.Lock()
	if s.running {
		s.Unlock()
		return ErrAlreadyRunning
	}
	s.running = true

	s.Unlock()
	defer func() {
		s.Lock()
		s.running = false
		s.Unlock()
	}()

	for {
		s.Lock()
		if s.paused {
			s.Unlock()
			return ErrPaused
		}

		if s.skipped {
			s.skipped = false
			s.running = false
			s.repeat = false
			s.done <- ErrSkipped
		}

		if s.end {
			s.end = false
			s.running = false
			s.repeat = false
			s.done <- ErrStopped
		}

		s.Unlock()
		err := s.readNext()
		s.Lock()

		if err != nil {
			s.finished = true
			if !errors.Is(err, io.EOF) {
				s.err = err
			} else {
				err = nil
			}

			if s.done != nil {
				s.done <- err
			}
			s.Unlock()
			break
		}
		s.Unlock()
	}
	if s.done != nil {
		s.done <- ErrStreamIsDone
	}
	return nil
}

func (s *StreamingSession) readNext() error {
	s.Lock()
	defer s.Unlock()

	opus, err := s.source.OpusFrame()
	if err != nil {
		return err
	}

	select {
	case <-time.After(time.Second):
		return ErrVoiceConnClosed
	case s.vc.OpusSend <- opus:
	}

	s.framesSent++
	return nil
}

// Paused returns wether the sream is paused or not
func (s *StreamingSession) Paused() bool {
	s.Lock()
	defer s.Unlock()
	return s.paused
}

// SetPaused provides pause/unpause functionality
func (s *StreamingSession) SetPaused(paused bool) {
	s.Lock()
	defer s.Unlock()
	if s.finished {
		s.Unlock()
		return
	}

	// Already running
	if !paused && s.running {
		if s.paused {
			// Was set to stop running after next frame so undo this
			s.paused = false
		}

		s.Unlock()
		return
	}

	// Already stopped
	if paused && !s.running {
		// Not running, but starting up..
		if !s.paused {
			s.paused = true
		}

		s.Unlock()
		return
	}

	// Time to start it up again
	if !s.running && s.paused && !paused {
		go s.stream()
	}

	s.paused = paused
}

// PlaybackPosition returns the the duration of content we have transmitted so far
func (s *StreamingSession) PlaybackPosition() time.Duration {
	s.Lock()
	defer s.Unlock()
	return time.Duration(s.framesSent) * s.source.FrameDuration()
}

func (s *StreamingSession) SetPlaybackPosition(time time.Duration) {
	s.Lock()
	frame := int(time * s.source.FrameDuration())
	s.framesSent = frame
	s.Unlock()
}

// Finished returns wether the stream finished or not, and any error that caused it to stop
func (s *StreamingSession) Finished() (bool, error) {
	s.Lock()
	s.Unlock()

	return s.finished, s.err
}

func (s *StreamingSession) SetSkipped() {
	s.Lock()

	if s.finished {
		s.Unlock()
		return
	}

	s.skipped = true

	s.Unlock()
}

// Stop current stream from playing more music
func (s *StreamingSession) SetFinished() {
	s.Lock()

	if s.finished {
		s.Unlock()
		return
	}

	s.end = true

	s.Unlock()
}

// checks if the stream is repeating
func (s *StreamingSession) Repeat() bool {
	s.Lock()

	if s.finished || !s.running {
		s.Unlock()
		return false
	}

	repeat := s.repeat

	s.Unlock()
	return repeat
}

func (s *StreamingSession) SetRepeat(repeat bool) {
	s.Lock()

	if s.finished || !s.running {
		s.Unlock()
		return
	}

	s.repeat = repeat

	s.Unlock()
}
