package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// poToken automation. youtube-source's WEB/WEBEMBEDDED clients need a fresh
// poToken+visitorData pair to pass YouTube's bot check, and it expires every few
// hours. Rather than paste one into application.yml by hand, pull a pair from a
// bgutil pot-provider and push it to Lavalink's runtime POST /youtube route,
// which takes effect without a restart. Gated on POT_PROVIDER_URL; no-op if unset.

// potFallbackInterval is the refresh cadence when the provider returns no usable
// expiry. bgutil poTokens live ~6h, so an hour keeps a wide margin.
// ponytail: fixed cadence; the loop honors expiresAt when the provider gives one.
const potFallbackInterval = time.Hour

// potMinRefreshGap floors the time between on-demand (playback-failure) refreshes
// so a burst of exceptions can't hammer BotGuard.
const potMinRefreshGap = 5 * time.Minute

var potLastRefresh atomic.Int64 // unix nanos of the last refresh attempt

// setupPoToken starts the background poToken refresher when POT_PROVIDER_URL is set.
func (b *Bot) setupPoToken() {
	provider := os.Getenv("POT_PROVIDER_URL")
	if provider == "" {
		return
	}
	go func() {
		time.Sleep(5 * time.Second) // let Lavalink's REST route come up
		for {
			wait := potFallbackInterval
			if exp, err := refreshPoToken(provider); err != nil {
				log.Println("potoken: refresh failed:", err)
				wait = 5 * time.Minute // back off, then retry
			} else if !exp.IsZero() {
				if d := time.Until(exp) - 5*time.Minute; d > 0 {
					wait = d // refresh just before it actually expires
				}
			}
			time.Sleep(wait)
		}
	}()
}

// potResponse is bgutil's /get_pot reply. contentBinding is the visitorData the
// poToken is bound to (bgutil cold-starts one when we send no content_binding).
type potResponse struct {
	PoToken        string `json:"poToken"`
	ContentBinding string `json:"contentBinding"`
	ExpiresAt      string `json:"expiresAt"`
}

// refreshPoToken fetches a fresh poToken/visitorData pair from the bgutil
// provider and pushes it to Lavalink. Returns the token's expiry (zero when the
// provider gives no parseable one).
func refreshPoToken(provider string) (time.Time, error) {
	potLastRefresh.Store(time.Now().UnixNano())

	// Empty body → bgutil cold-starts a session and returns the visitorData it
	// generated as contentBinding.
	raw, err := httpPostJSON(provider+"/get_pot", nil, "")
	if err != nil {
		return time.Time{}, fmt.Errorf("get_pot: %w", err)
	}
	var pr potResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return time.Time{}, fmt.Errorf("get_pot decode: %w", err)
	}
	if pr.PoToken == "" || pr.ContentBinding == "" {
		return time.Time{}, fmt.Errorf("get_pot returned empty token/binding: %s", raw)
	}

	body, _ := json.Marshal(map[string]string{"poToken": pr.PoToken, "visitorData": pr.ContentBinding})
	if _, err := httpPostJSON(lavalinkHTTPBase()+"/youtube", body, lavalinkPassword()); err != nil {
		return time.Time{}, fmt.Errorf("push to lavalink: %w", err)
	}

	exp, _ := time.Parse(time.RFC3339, pr.ExpiresAt)
	log.Printf("potoken: pushed fresh token (visitorData %.12s…, expires %s)", pr.ContentBinding, pr.ExpiresAt)
	return exp, nil
}

// refreshPoTokenOnDemand triggers a refresh after a playback failure, but no more
// than once per potMinRefreshGap. Best-effort: it logs and moves on.
func (b *Bot) refreshPoTokenOnDemand() {
	provider := os.Getenv("POT_PROVIDER_URL")
	if provider == "" {
		return
	}
	if last := potLastRefresh.Load(); last != 0 && time.Since(time.Unix(0, last)) < potMinRefreshGap {
		return
	}
	go func() {
		if _, err := refreshPoToken(provider); err != nil {
			log.Println("potoken: on-demand refresh failed:", err)
		}
	}()
}

// lavalinkPassword mirrors the default used in setupLavalink.
func lavalinkPassword() string {
	if p := os.Getenv("LAVALINK_PASSWORD"); p != "" {
		return p
	}
	return "youshallnotpass"
}

// lavalinkHTTPBase builds the http(s) base URL of the Lavalink REST API from the
// same env the disgolink node uses.
func lavalinkHTTPBase() string {
	scheme := "http"
	if secure, _ := strconv.ParseBool(os.Getenv("LAVALINK_SECURE")); secure {
		scheme = "https"
	}
	return scheme + "://" + os.Getenv("LAVALINK_ADDRESS")
}

// httpPostJSON POSTs body (nil → "{}") to url with an optional Authorization
// header and returns the response body, erroring on any non-2xx status.
func httpPostJSON(url string, body []byte, auth string) ([]byte, error) {
	if body == nil {
		body = []byte("{}")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s: %s", url, resp.Status, data)
	}
	return data, nil
}
