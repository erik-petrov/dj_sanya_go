package bot

import "testing"

func TestScan(t *testing.T) {
	filterMu.Lock()
	filterCfg = map[string]*filterGuild{"g": {
		Regexes:   []string{"тварь", "казах", `discord\.gg/\S+`},
		Whitelist: []string{"Казахстан"},
		Watched:   map[string]bool{"c": true},
	}}
	recompile("g")
	filterMu.Unlock()

	for _, c := range []struct {
		content string
		want    bool
	}{
		{"ты тварь", true},
		{"ТВАРЬ", true},              // case-insensitive
		{"я из Казахстана", false},   // matches казах but whitelisted by Казахстан
		{"этот казах злой", true},    // matches казах, not whitelisted
		{"зайди discord.gg/abc", true},
		{"обычное сообщение", false},
	} {
		if _, ok := scan("g", c.content); ok != c.want {
			t.Errorf("scan(%q) = %v, want %v", c.content, ok, c.want)
		}
	}
	if _, ok := scan("unconfigured", "тварь"); ok {
		t.Error("scan on a guild with no config should not fire")
	}
}
