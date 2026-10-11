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

// TestStreamRetry covers the stream-403 retry bookkeeping: retries are capped per
// track, each queued retry is consumed exactly once (with its resume position), a
// different track starts fresh, and resetRetry drops everything.
func TestStreamRetry(t *testing.T) {
	q := &LavalinkQueue{}
	if _, ok := q.pendingRetry(); ok {
		t.Fatal("nothing queued yet")
	}
	for i := 1; i <= maxStreamRetries; i++ {
		want := lavalink.Duration(1000 * i)
		if !q.takeRetry("a", want) {
			t.Fatalf("retry %d of %d should be allowed", i, maxStreamRetries)
		}
		if pos, ok := q.pendingRetry(); !ok || pos != want {
			t.Fatalf("retry %d: pendingRetry = %v, %v; want %v, true", i, pos, ok, want)
		}
		if _, ok := q.pendingRetry(); ok {
			t.Fatal("a queued retry must be consumed only once")
		}
	}
	if q.takeRetry("a", 0) {
		t.Fatal("retries past the cap must be refused")
	}
	if !q.takeRetry("b", 0) {
		t.Fatal("a different track starts with fresh retries")
	}
	q.resetRetry()
	if _, ok := q.pendingRetry(); ok {
		t.Fatal("resetRetry must drop a queued retry")
	}
	if !q.takeRetry("b", 0) {
		t.Fatal("after a reset the same track can retry again")
	}
}
