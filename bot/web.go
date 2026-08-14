package bot

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
)

// Web control panel: a Discord-OAuth-gated browser UI over the bot's existing
// controls. Enabled only when WEB_ADDR + WEB_BASE_URL + DISCORD_CLIENT_ID/SECRET
// are set. Sessions are in-memory (lost on restart → re-login). Sits behind an
// HTTPS reverse proxy.
//
// ponytail: in-memory sessions, no DB; polling frontend, no websocket.

//go:embed web/index.html
var webIndex []byte

var (
	webBaseURL      string
	webClientID     string
	webClientSecret string
	webSessions     sync.Map // sessionID -> *webSession
)

type webSession struct {
	userID   string
	username string
	guilds   map[string]bool // guild IDs the user belongs to (from OAuth)
}

func (b *Bot) setupWeb() {
	addr := os.Getenv("WEB_ADDR")
	webBaseURL = strings.TrimRight(os.Getenv("WEB_BASE_URL"), "/")
	webClientID = os.Getenv("DISCORD_CLIENT_ID")
	webClientSecret = os.Getenv("DISCORD_CLIENT_SECRET")
	if addr == "" || webBaseURL == "" || webClientID == "" || webClientSecret == "" {
		log.Println("web panel disabled (need WEB_ADDR, WEB_BASE_URL, DISCORD_CLIENT_ID, DISCORD_CLIENT_SECRET)")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", b.webIndex)
	mux.HandleFunc("/login", b.webLogin)
	mux.HandleFunc("/callback", b.webCallback)
	mux.HandleFunc("/logout", b.webLogout)
	mux.HandleFunc("/api/me", b.webAuth(b.apiMe))
	mux.HandleFunc("/api/state", b.webAuth(b.apiState))
	mux.HandleFunc("/api/play", b.webAuth(b.apiPlay))
	mux.HandleFunc("/api/search", b.webAuth(b.apiSearch))
	mux.HandleFunc("/api/skip", b.webAuth(b.apiSkip))
	mux.HandleFunc("/api/stop", b.webAuth(b.apiStop))
	mux.HandleFunc("/api/pause", b.webAuth(b.apiPause))
	mux.HandleFunc("/api/repeat", b.webAuth(b.apiRepeat))
	mux.HandleFunc("/api/seek", b.webAuth(b.apiSeek))

	go func() {
		log.Println("web panel listening on", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Println("web panel stopped:", err)
		}
	}()
}

// ---- helpers ----

func randID() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (b *Bot) sessionOf(r *http.Request) *webSession {
	c, err := r.Cookie("sanya_session")
	if err != nil {
		return nil
	}
	if v, ok := webSessions.Load(c.Value); ok {
		return v.(*webSession)
	}
	return nil
}

// webAuth gates an API handler: 401 if not logged in, 403 if banned.
func (b *Bot) webAuth(h func(http.ResponseWriter, *http.Request, *webSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := b.sessionOf(r)
		if sess == nil {
			http.Error(w, "not logged in", http.StatusUnauthorized)
			return
		}
		if isBanned(sess.userID) {
			http.Error(w, "banned", http.StatusForbidden)
			return
		}
		h(w, r, sess)
	}
}

// guildParam returns the ?guild= value, but only if the user is actually in it.
func guildParam(w http.ResponseWriter, r *http.Request, sess *webSession) (string, bool) {
	g := r.URL.Query().Get("guild")
	if g == "" || !sess.guilds[g] {
		http.Error(w, "not your guild", http.StatusForbidden)
		return "", false
	}
	return g, true
}

// ---- pages ----

func (b *Bot) webIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(webIndex)
}

func (b *Bot) webLogin(w http.ResponseWriter, r *http.Request) {
	state := randID()
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, Path: "/", MaxAge: 300, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	q := url.Values{
		"client_id":     {webClientID},
		"redirect_uri":  {webBaseURL + "/callback"},
		"response_type": {"code"},
		"scope":         {"identify guilds"},
		"state":         {state},
	}
	http.Redirect(w, r, "https://discord.com/oauth2/authorize?"+q.Encode(), http.StatusFound)
}

func (b *Bot) webCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("oauth_state")
	if err != nil || c.Value == "" || c.Value != r.URL.Query().Get("state") {
		http.Error(w, "bad oauth state", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "no code", http.StatusBadRequest)
		return
	}
	token, err := discordTokenExchange(code)
	if err != nil {
		log.Println("oauth token exchange:", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	user, guilds, err := discordUserAndGuilds(token)
	if err != nil {
		log.Println("oauth fetch user:", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	sid := randID()
	webSessions.Store(sid, &webSession{userID: user.ID, username: user.Username, guilds: guilds})
	http.SetCookie(w, &http.Cookie{Name: "sanya_session", Value: sid, Path: "/", MaxAge: 7 * 24 * 3600, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (b *Bot) webLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("sanya_session"); err == nil {
		webSessions.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "sanya_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}

// ---- Discord OAuth (hand-rolled) ----

type discordUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func discordTokenExchange(code string) (string, error) {
	resp, err := http.PostForm("https://discord.com/api/oauth2/token", url.Values{
		"client_id":     {webClientID},
		"client_secret": {webClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {webBaseURL + "/callback"},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token status %d: %s", resp.StatusCode, body)
	}
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

func discordGet(token, path string, out any) error {
	req, _ := http.NewRequest(http.MethodGet, "https://discord.com/api"+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func discordUserAndGuilds(token string) (discordUser, map[string]bool, error) {
	var u discordUser
	if err := discordGet(token, "/users/@me", &u); err != nil {
		return u, nil, err
	}
	var gs []struct {
		ID string `json:"id"`
	}
	if err := discordGet(token, "/users/@me/guilds", &gs); err != nil {
		return u, nil, err
	}
	m := make(map[string]bool, len(gs))
	for _, g := range gs {
		m[g.ID] = true
	}
	return u, m, nil
}

// ---- API ----

func (b *Bot) apiMe(w http.ResponseWriter, r *http.Request, sess *webSession) {
	type guildOut struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	guilds := []guildOut{}
	for _, g := range b.s.State.Guilds { // only servers the user shares with the bot
		if sess.guilds[g.ID] {
			guilds = append(guilds, guildOut{g.ID, g.Name})
		}
	}
	writeJSON(w, map[string]any{"user": sess.username, "guilds": guilds})
}

func (b *Bot) apiState(w http.ResponseWriter, r *http.Request, sess *webSession) {
	guildID, ok := guildParam(w, r, sess)
	if !ok {
		return
	}
	out := map[string]any{"queue": []any{}, "position": 0, "length": 0, "paused": false, "repeat": false, "title": ""}
	if LavalinkClient != nil {
		if p := LavalinkClient.ExistingPlayer(snowflake.MustParse(guildID)); p != nil {
			if t := p.Track(); t != nil {
				out["title"] = t.Info.Title
				out["uri"] = trackURI(*t)
				out["length"] = int64(t.Info.Length)
				out["position"] = int64(p.Position())
				out["paused"] = p.Paused()
			}
		}
		out["repeat"] = LavalinkQueues.Get(guildID).Type == QueueTypeRepeatTrack
		q := []map[string]any{}
		for _, t := range LavalinkQueues.Get(guildID).List() {
			q = append(q, map[string]any{"title": t.Info.Title, "uri": trackURI(t)})
		}
		out["queue"] = q
	}
	writeJSON(w, out)
}

func (b *Bot) apiPlay(w http.ResponseWriter, r *http.Request, sess *webSession) {
	guildID, ok := guildParam(w, r, sess)
	if !ok {
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	query := strings.TrimSpace(body.Query)
	if query == "" {
		http.Error(w, "empty query", http.StatusBadRequest)
		return
	}
	vs, err := b.s.State.VoiceState(guildID, sess.userID)
	if err != nil || vs.ChannelID == "" {
		http.Error(w, "join a voice channel first", http.StatusConflict)
		return
	}
	go b.playQuery(guildID, vs.ChannelID, query, "")
	w.WriteHeader(http.StatusNoContent)
}

func (b *Bot) apiSearch(w http.ResponseWriter, r *http.Request, sess *webSession) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || LavalinkClient == nil {
		writeJSON(w, []any{})
		return
	}
	node := LavalinkClient.BestNode()
	if node == nil {
		writeJSON(w, []any{})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var results []lavalink.Track
	node.LoadTracksHandler(ctx, lavalink.SearchTypeYouTube.Apply(q), disgolink.NewResultHandler(
		func(t lavalink.Track) { results = []lavalink.Track{t} },
		func(pl lavalink.Playlist) { results = pl.Tracks },
		func(ts []lavalink.Track) { results = ts },
		func() {},
		func(error) {},
	))
	if len(results) > 10 {
		results = results[:10]
	}
	out := make([]map[string]any, 0, len(results))
	for _, t := range results {
		out = append(out, map[string]any{"title": t.Info.Title, "uri": trackURI(t), "author": t.Info.Author, "length": int64(t.Info.Length)})
	}
	writeJSON(w, out)
}

func (b *Bot) apiSkip(w http.ResponseWriter, r *http.Request, sess *webSession) {
	if guildID, ok := guildParam(w, r, sess); ok {
		_ = b.SkipLavalink(guildID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (b *Bot) apiStop(w http.ResponseWriter, r *http.Request, sess *webSession) {
	if guildID, ok := guildParam(w, r, sess); ok {
		_ = b.StopLavalink(guildID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (b *Bot) apiRepeat(w http.ResponseWriter, r *http.Request, sess *webSession) {
	if guildID, ok := guildParam(w, r, sess); ok {
		writeJSON(w, map[string]any{"repeat": b.ToggleRepeat(guildID)})
	}
}

func (b *Bot) apiPause(w http.ResponseWriter, r *http.Request, sess *webSession) {
	guildID, ok := guildParam(w, r, sess)
	if !ok {
		return
	}
	var body struct {
		Paused bool `json:"paused"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	_ = b.PauseLavalink(guildID, body.Paused)
	w.WriteHeader(http.StatusNoContent)
}

func (b *Bot) apiSeek(w http.ResponseWriter, r *http.Request, sess *webSession) {
	guildID, ok := guildParam(w, r, sess)
	if !ok {
		return
	}
	var body struct {
		Ms int64 `json:"ms"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	p := LavalinkClient.ExistingPlayer(snowflake.MustParse(guildID))
	if p == nil || p.Track() == nil {
		http.Error(w, "nothing playing", http.StatusConflict)
		return
	}
	ms := body.Ms
	if length := int64(p.Track().Info.Length); length > 0 && ms > length {
		ms = length
	}
	if ms < 0 {
		ms = 0
	}
	_ = p.Update(context.TODO(), lavalink.WithPosition(lavalink.Duration(ms)))
	w.WriteHeader(http.StatusNoContent)
}
