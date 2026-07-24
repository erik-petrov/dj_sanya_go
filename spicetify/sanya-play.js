// Sanya Play — Spicetify extension
//
// Sends a Spotify track to the Sanya Discord bot (via a Discord webhook), which
// resolves it through LavaSrc and plays it in your current voice channel.
//
// Install:
//   1. copy this file into the Spicetify Extensions folder (`spicetify config-dir`)
//   2. spicetify config extensions sanya-play.js
//   3. spicetify apply
// Then set the webhook / password / your Discord ID via the profile menu ->
// "Sanya Play settings".

(function SanyaPlay() {
  const S = window.Spicetify;
  // Wait until the APIs we need are ready.
  if (
    !S ||
    !S.Player ||
    !S.Topbar ||
    !S.ContextMenu ||
    !S.LocalStorage ||
    !S.PopupModal ||
    !S.showNotification
  ) {
    setTimeout(SanyaPlay, 300);
    return;
  }

  const LS = S.LocalStorage;
  const KEYS = {
    webhook: "sanya:webhook",
    password: "sanya:password",
    userid: "sanya:userid",
  };
  const cfg = () => ({
    webhook: LS.get(KEYS.webhook) || "",
    password: LS.get(KEYS.password) || "",
    userid: LS.get(KEYS.userid) || "",
  });

  const notify = (msg, isErr) => S.showNotification(msg, !!isErr);

  // spotify:track:ID -> https://open.spotify.com/track/ID (also album/playlist/episode).
  function uriToUrl(uri) {
    const m = /^spotify:(track|album|playlist|episode):([a-zA-Z0-9]+)/.exec(uri || "");
    return m ? `https://open.spotify.com/${m[1]}/${m[2]}` : null;
  }

  function currentUri() {
    const d = S.Player.data || {};
    const item = d.item || d.track; // newer / older Spicetify
    return item && item.uri;
  }

  async function send(uri) {
    const c = cfg();
    if (!c.webhook || !c.password || !c.userid) {
      notify("Sanya Play: fill in settings first", true);
      openSettings();
      return;
    }
    const url = uriToUrl(uri);
    if (!url) {
      notify("Sanya Play: unsupported item", true);
      return;
    }
    try {
      const res = await fetch(c.webhook, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content: `${c.password} ${c.userid} ${url}` }),
      });
      if (res.ok) notify("Sent to Sanya ▶");
      else notify(`Sanya Play: webhook error ${res.status}`, true);
    } catch (e) {
      notify("Sanya Play: " + e.message, true);
    }
  }

  // ---- settings modal ----
  function openSettings() {
    const c = cfg();
    const wrap = document.createElement("div");
    wrap.style.cssText = "display:flex;flex-direction:column;gap:10px;";

    const field = (labelText, value, type) => {
      const label = document.createElement("label");
      label.style.cssText = "display:flex;flex-direction:column;gap:4px;font-size:13px;";
      label.append(labelText);
      const input = document.createElement("input");
      input.type = type || "text";
      input.value = value || "";
      input.style.cssText =
        "padding:8px;border-radius:4px;border:1px solid #555;background:#121212;color:#fff;";
      label.appendChild(input);
      wrap.appendChild(label);
      return input;
    };

    const wh = field("Discord webhook URL", c.webhook, "url");
    const pw = field("Password", c.password, "password");
    const uid = field("Your Discord user ID", c.userid, "text");

    const save = document.createElement("button");
    save.textContent = "Save";
    save.style.cssText =
      "margin-top:6px;padding:8px;border:0;border-radius:20px;background:#1db954;color:#000;font-weight:700;cursor:pointer;";
    save.onclick = () => {
      LS.set(KEYS.webhook, wh.value.trim());
      LS.set(KEYS.password, pw.value);
      LS.set(KEYS.userid, uid.value.trim());
      notify("Sanya Play: saved");
      S.PopupModal.hide();
    };
    wrap.appendChild(save);

    S.PopupModal.display({ title: "Sanya Play settings", content: wrap });
  }

  // ---- topbar button: play what's currently playing ----
  new S.Topbar.Button("Play current on Sanya", "play", () => {
    const uri = currentUri();
    if (!uri) {
      notify("Sanya Play: nothing is playing", true);
      return;
    }
    send(uri);
  });

  // ---- right-click a track/album/playlist -> Play on Sanya ----
  new S.ContextMenu.Item(
    "Play on Sanya",
    (uris) => send(uris && uris[0]),
    (uris) =>
      uris &&
      uris.length === 1 &&
      /^spotify:(track|album|playlist|episode):/.test(uris[0]),
    "play",
  ).register();

  // ---- settings entry in the profile menu ----
  if (S.Menu && S.Menu.Item) {
    new S.Menu.Item("Sanya Play settings", false, openSettings).register();
  }

  console.log("Sanya Play loaded");
})();
