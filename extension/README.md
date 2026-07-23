# Sanya Play — browser extension

Sends the current YouTube video to the Sanya Discord bot, which plays it in
whatever voice channel you're currently in. It works by POSTing to a Discord
**webhook**; the bot watches that channel and plays what it receives. No server
ports are exposed.

## How it fits together

```
YouTube tab ──(click)──▶ extension ──POST──▶ Discord webhook ──▶ control channel
                                                                       │
                                                     bot sees the webhook message,
                                                     checks the password, finds your
                                                     voice channel, and plays.
```

The message the extension sends is:

```
<password> <your discord user id> <clean youtube url>
```

## Install (unpacked)

1. Open `chrome://extensions` (or `edge://extensions`).
2. Enable **Developer mode** (top right).
3. **Load unpacked** → select this `extension/` folder.
4. Pin the extension so its icon is visible.

## Configure

Click the icon → **Settings**, and fill in:

- **Discord webhook URL** — create it in the bot's control channel:
  Channel → Edit → Integrations → Webhooks → New Webhook → Copy Webhook URL.
- **Password** — the `HOOK_PASSWORD` you set in the bot's `.env`.
- **Your Discord user ID** — enable Developer Mode in Discord
  (Settings → Advanced), then right-click yourself → Copy User ID.

Click **Save settings**.

## Use

1. Join a voice channel in Discord.
2. Open any YouTube video, `youtu.be` link, or Short.
3. Click the extension → **Play this video**.

## Notes

- The **webhook URL + password together** are the credential — anyone with both
  can queue songs for any user ID that's currently in voice. Keep them private.
- Make the control channel **private** and give the bot **Manage Messages** there
  so it can delete each trigger message (the password rides in the message text).
- Playlist/timestamp params are stripped automatically — only the single video is
  sent, never the whole playlist.
