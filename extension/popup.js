const $ = (id) => document.getElementById(id);
const setStatus = (msg, cls) => {
  const s = $("status");
  s.textContent = msg;
  s.className = cls || "";
};

// Load saved settings; open the settings panel until everything is filled in.
chrome.storage.sync.get(["webhook", "password", "userid"], (cfg) => {
  $("webhook").value = cfg.webhook || "";
  $("password").value = cfg.password || "";
  $("userid").value = cfg.userid || "";
  if (!cfg.webhook || !cfg.password || !cfg.userid) $("settings").open = true;
});

$("save").addEventListener("click", () => {
  chrome.storage.sync.set(
    {
      webhook: $("webhook").value.trim(),
      password: $("password").value,
      userid: $("userid").value.trim(),
    },
    () => setStatus("Saved.", "ok"),
  );
});

// Reduce a YouTube URL to a clean watch link, dropping playlist/timestamp params
// so the bot loads just the one video (and not a whole playlist).
function cleanYouTubeURL(raw) {
  let u;
  try {
    u = new URL(raw);
  } catch {
    return null;
  }
  const host = u.hostname.replace(/^www\./, "");
  if (host === "youtu.be") {
    const id = u.pathname.slice(1);
    return id ? "https://www.youtube.com/watch?v=" + id : null;
  }
  if (host.endsWith("youtube.com")) {
    const v = u.searchParams.get("v");
    if (v) return "https://www.youtube.com/watch?v=" + v;
    const shorts = u.pathname.match(/^\/shorts\/([^/?]+)/);
    if (shorts) return "https://www.youtube.com/watch?v=" + shorts[1];
  }
  return null;
}

$("play").addEventListener("click", async () => {
  const cfg = await chrome.storage.sync.get(["webhook", "password", "userid"]);
  if (!cfg.webhook || !cfg.password || !cfg.userid) {
    setStatus("Fill in settings first.", "err");
    $("settings").open = true;
    return;
  }

  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const url = cleanYouTubeURL(tab && tab.url);
  if (!url) {
    setStatus("Not a YouTube video tab.", "err");
    return;
  }

  setStatus("Sending…");
  try {
    const res = await fetch(cfg.webhook, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content: `${cfg.password} ${cfg.userid} ${url}` }),
    });
    if (res.ok) {
      setStatus("Sent ▶ — make sure you're in a voice channel.", "ok");
    } else {
      setStatus(`Webhook error: ${res.status}`, "err");
    }
  } catch (e) {
    setStatus("Request failed: " + e.message, "err");
  }
});
