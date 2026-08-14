package bot

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Security gate: no session -> 401, valid -> runs, banned -> 403, and a foreign
// guild is rejected.
func TestWebAuthGate(t *testing.T) {
	b := &Bot{}
	called := false
	h := b.webAuth(func(w http.ResponseWriter, r *http.Request, s *webSession) { called = true; w.WriteHeader(200) })

	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("GET", "/api/x", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no session: want 401 got %d", rr.Code)
	}

	webSessions.Store("sid1", &webSession{userID: "u1", guilds: map[string]bool{"g1": true}})
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: "sanya_session", Value: "sid1"})
	rr = httptest.NewRecorder()
	h(rr, req)
	if !called || rr.Code != 200 {
		t.Fatalf("valid session: called=%v code=%d", called, rr.Code)
	}

	banUser("u1")
	defer unbanUser("u1")
	called = false
	rr = httptest.NewRecorder()
	h(rr, req)
	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("banned: called=%v code=%d", called, rr.Code)
	}

	rr = httptest.NewRecorder()
	if _, ok := guildParam(rr, httptest.NewRequest("GET", "/api/x?guild=other", nil), &webSession{guilds: map[string]bool{"g1": true}}); ok || rr.Code != http.StatusForbidden {
		t.Fatalf("foreign guild: ok=%v code=%d", ok, rr.Code)
	}
}
