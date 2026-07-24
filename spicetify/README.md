# Sanya Play — Spicetify extension

Sends a Spotify track to the Sanya Discord bot from the Spotify **desktop** app.
It POSTs the track's `open.spotify.com` URL to the same Discord webhook the
bot watches; the bot resolves it via LavaSrc (Spotify → mirrored to YouTube) and
plays it in whatever voice channel you're currently in.

Requires [Spicetify](https://spicetify.app) and the bot's LavaSrc/Spotify setup
(`SPOTIFY_ID` / `SPOTIFY_SECRET`) to be configured.

## Install

1. Find your Spicetify config dir:
   ```
   spicetify config-dir
   ```
2. Copy `sanya-play.js` into the `Extensions/` folder inside it.
3. Enable and apply:
   ```
   spicetify config extensions sanya-play.js
   spicetify apply
   ```

## Configure

In Spotify: click your profile (top-right) → **Sanya Play settings**, then fill in:

- **Discord webhook URL** — from the bot's control channel (Edit channel →
  Integrations → Webhooks → New Webhook → Copy URL).
- **Password** — the `HOOK_PASSWORD` from the bot's `.env`.
- **Your Discord user ID** — Discord → Settings → Advanced → Developer Mode,
  then right-click yourself → Copy User ID.

## Use

- **Topbar button** (play icon) — sends the **currently playing** track.
- **Right-click** a track / album / playlist → **Play on Sanya** — sends that one.

You must be in a voice channel when you trigger it.

## Notes

- Albums/playlists send the whole collection (the bot queues them).
- The **webhook URL + password together** are the credential — keep them private.
- Podcast episodes may not resolve (LavaSrc mirrors music tracks).
