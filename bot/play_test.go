package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgolink/v3/lavalink"
)

// TestQueueTiming covers the play-duration tracking that lets onTrackEnd tell a
// real load failure from a track that played through and only faulted at its end
// (the Discord-CDN MP3 repeat bug).
func TestQueueTiming(t *testing.T) {
	q := &LavalinkQueue{}

	if q.playedAtLeast(0) {
		t.Fatal("no track started: playedAtLeast should be false")
	}

	q.markStarted()
	if !q.playedAtLeast(0) {
		t.Fatal("just started: playedAtLeast(0) should be true")
	}
	if q.playedAtLeast(time.Hour) {
		t.Fatal("just started: playedAtLeast(1h) should be false")
	}

	// A track that played well past the success window counts as long enough,
	// so a tail-end fault is treated as success (repeat loops it).
	q.startedAt = time.Now().Add(-minPlayForSuccess - time.Second)
	if !q.playedAtLeast(minPlayForSuccess) {
		t.Fatal("played past the window: should count as long enough")
	}

	// After a track ends, its start time must not leak into the next one — a
	// track that never starts would otherwise inherit this elapsed time.
	q.clearStarted()
	if q.playedAtLeast(0) {
		t.Fatal("cleared: playedAtLeast should be false again")
	}
}

// TestAdviceForException checks the exception → advice mapping: specific causes
// win over the severity fallback, and each severity has a distinct message.
func TestAdviceForException(t *testing.T) {
	cases := []struct {
		cause    string
		severity lavalink.Severity
		want     string // expected substring
	}{
		{"Video unavailable", lavalink.SeverityCommon, "недоступно"},
		{"This video requires payment; 403 Forbidden", lavalink.SeverityFault, "403"},
		{"Read timed out", lavalink.SeverityFault, "таймаут"},
		{"Please sign in to confirm your age", lavalink.SeverityCommon, "ограничение"},
		{"", lavalink.SeverityCommon, "источника"},   // fallback: common
		{"", lavalink.SeveritySuspicious, "странно"}, // fallback: suspicious
		{"", lavalink.SeverityFault, "Внутренняя"},   // fallback: fault
	}
	for _, tc := range cases {
		got := adviceForException(lavalink.Exception{Cause: tc.cause, Severity: tc.severity})
		if !strings.Contains(got, tc.want) {
			t.Errorf("cause %q sev %q: got %q, want substring %q", tc.cause, tc.severity, got, tc.want)
		}
	}
}
