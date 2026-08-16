package bot

import (
	"testing"
	"time"
)

func TestParseKickDelay(t *testing.T) {
	for in, want := range map[string]time.Duration{"1h": time.Hour, "1m": time.Minute, "90m": 90 * time.Minute} {
		if d, ok := parseKickDelay(in); !ok || d != want {
			t.Errorf("%q: got %v,%v want %v,true", in, d, ok, want)
		}
	}
	for _, in := range []string{"", "0m", "-1h", "25h", "abc", "1"} {
		if _, ok := parseKickDelay(in); ok {
			t.Errorf("%q: want invalid", in)
		}
	}
}
