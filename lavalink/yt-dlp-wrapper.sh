#!/bin/sh
# yt-dlp wrapper that LavaSrc calls (installed as /usr/local/bin/yt-dlp).
#
# Self-update: YouTube breaks a stale yt-dlp (and its yt-dlp-ejs JS challenge
# solver) within weeks — googlevideo 403s, missing formats, weak-client fallback.
# The image only installs yt-dlp at build time and Docker caches that layer, so
# without this it silently rots. At most once a day, upgrade in the background;
# each later call picks up the new version (no Lavalink restart needed). flock
# keeps two concurrent calls from running pip into the same venv at once.
# ponytail: an upgrade landing mid-import can fail that one call (the bot skips
# the track); acceptable for a once-a-day window.
if [ -z "$(find /tmp/ytdlp-updated -mmin -1440 2>/dev/null)" ]; then
	touch /tmp/ytdlp-updated
	flock -n /tmp/ytdlp-update.lock \
		/opt/ytdlp/bin/pip install --no-cache-dir -U --pre "yt-dlp[default]" \
		</dev/null >/tmp/ytdlp-update.log 2>&1 &
fi

# Cookies (optional): YouTube refuses streams (403 on every URL) to unauthenticated
# sessions from a flagged IP. If a Netscape cookies.txt from a THROWAWAY Google
# account is mounted at /opt/Lavalink/cookies/cookies.txt, hand yt-dlp a private
# per-call copy: yt-dlp rewrites its cookie jar on exit, and LavaSrc's parallel
# calls (load + playback) writing one shared file could corrupt it. No file, no-op.
if [ -f /opt/Lavalink/cookies/cookies.txt ] && jar=$(mktemp); then
	cp /opt/Lavalink/cookies/cookies.txt "$jar"
	trap 'rm -f "$jar"' EXIT
	set -- --cookies "$jar" "$@"
fi

# stdout carries LavaSrc's JSON, so stderr must stay off it — but keep the last
# call's warnings/errors in a file instead of discarding them, so a breakage is
# diagnosable: docker compose exec lavalink cat /tmp/ytdlp-last.err
# stdout goes through ytdlp-fixnull.py to null-fill fields LavaSrc would NPE on.
/opt/ytdlp/bin/yt-dlp "$@" 2>/tmp/ytdlp-last.err | /opt/ytdlp/bin/python /usr/local/bin/ytdlp-fixnull.py
