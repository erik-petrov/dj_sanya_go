package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRefreshPoToken checks the money path: we cold-start the provider with an
// empty body, and push contentBinding→visitorData to Lavalink's /youtube with the
// password as the Authorization header.
func TestRefreshPoToken(t *testing.T) {
	var providerBody string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		providerBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"poToken":"TOK","contentBinding":"VISITOR","expiresAt":"2099-01-02T15:04:05Z"}`)
	}))
	defer provider.Close()

	var pushed map[string]string
	var gotAuth, gotPath string
	lava := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&pushed)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer lava.Close()

	t.Setenv("LAVALINK_ADDRESS", strings.TrimPrefix(lava.URL, "http://"))
	t.Setenv("LAVALINK_SECURE", "false")
	t.Setenv("LAVALINK_PASSWORD", "pw")

	exp, err := refreshPoToken(provider.URL)
	if err != nil {
		t.Fatal(err)
	}
	if providerBody != "{}" {
		t.Errorf("provider body = %q, want {}", providerBody)
	}
	if gotPath != "/youtube" {
		t.Errorf("push path = %q, want /youtube", gotPath)
	}
	if pushed["poToken"] != "TOK" || pushed["visitorData"] != "VISITOR" {
		t.Errorf("pushed = %v, want poToken=TOK visitorData=VISITOR", pushed)
	}
	if gotAuth != "pw" {
		t.Errorf("auth = %q, want pw", gotAuth)
	}
	if want := time.Date(2099, 1, 2, 15, 4, 5, 0, time.UTC); !exp.Equal(want) {
		t.Errorf("expiry = %v, want %v", exp, want)
	}
}

// A non-2xx from the provider must surface as an error, not a silent junk push.
func TestRefreshPoTokenProviderError(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer provider.Close()
	if _, err := refreshPoToken(provider.URL); err == nil {
		t.Error("want error on provider 400, got nil")
	}
}
