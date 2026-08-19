# Lavalink + yt-dlp (fast, pip-installed).
#  - Python 3.12 from python-build-standalone (portable tarball — no deadsnakes
#    PPA, no system-3.10 whose deprecation notice pollutes LavaSrc's JSON). Pip-
#    install nightly yt-dlp into a venv: it starts in ~0.2s vs the standalone
#    binary's ~0.9s decompress-on-every-launch, which LavaSrc paid TWICE per play.
#    The bgutil poToken plugin installs via pip too (the venv auto-discovers it —
#    no plugin zip needed).
#  - deno: JS runtime yt-dlp needs to run YouTube's player JS / decipher sigs.
#  - wrapper: drop yt-dlp stderr so its chatter can't break LavaSrc's stdout JSON.
#    (Debug by calling /opt/ytdlp/bin/yt-dlp directly to see stderr.)
#
# Nightly, not stable: stable 2026.07.04 has the youtube stream-403 bug
# (yt-dlp#17456). arch: x86_64/glibc.
# Update yt-dlp / python: docker compose build --no-cache lavalink
FROM ghcr.io/lavalink-devs/lavalink:4
USER root
ADD https://github.com/astral-sh/python-build-standalone/releases/download/20260814/cpython-3.12.14+20260814-x86_64-unknown-linux-gnu-install_only.tar.gz /tmp/py.tar.gz
RUN tar -xzf /tmp/py.tar.gz -C /opt && rm /tmp/py.tar.gz \
 && /opt/python/bin/python3 -m venv /opt/ytdlp \
 && /opt/ytdlp/bin/pip install --no-cache-dir -U --pre "yt-dlp[default]" bgutil-ytdlp-pot-provider \
 && printf '#!/bin/sh\nexec /opt/ytdlp/bin/yt-dlp "$@" 2>/dev/null\n' > /usr/local/bin/yt-dlp \
 && chmod a+rx /usr/local/bin/yt-dlp
ADD https://github.com/denoland/deno/releases/latest/download/deno-x86_64-unknown-linux-gnu.zip /tmp/deno.zip
RUN apt-get update && apt-get install -y --no-install-recommends unzip \
 && unzip -o /tmp/deno.zip -d /usr/local/bin && rm /tmp/deno.zip \
 && chmod a+rx /usr/local/bin/deno \
 && rm -rf /var/lib/apt/lists/*
USER lavalink
