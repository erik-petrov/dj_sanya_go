# Lavalink + yt-dlp. LavaSrc shells out to `yt-dlp`, absent from the base image.
#  - yt-dlp_linux: standalone build, bundles its own Python (base ships 3.10,
#    whose deprecation notice would pollute LavaSrc's JSON parse).
#  - deno: yt-dlp needs a JS runtime to run YouTube's player JS and decipher
#    signatures. Without it, only storyboard formats resolve and streams 403.
#  - wrapper: drop yt-dlp stderr so its chatter can't break LavaSrc's stdout JSON.
#
# arch: x86_64/glibc. On arm64 use yt-dlp_linux_aarch64 + deno-aarch64-...
# Update: docker compose build --pull --no-cache lavalink
# ponytail: no bgutil poToken plugin yet — add to /etc/yt-dlp/plugins/ only if a
# video still fails after deno (poToken/SABR error).
FROM ghcr.io/lavalink-devs/lavalink:4
USER root
ADD https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux /usr/local/bin/yt-dlp.real
ADD https://github.com/denoland/deno/releases/latest/download/deno-x86_64-unknown-linux-gnu.zip /tmp/deno.zip
RUN apt-get update && apt-get install -y --no-install-recommends unzip \
 && unzip -o /tmp/deno.zip -d /usr/local/bin && rm /tmp/deno.zip \
 && chmod a+rx /usr/local/bin/yt-dlp.real /usr/local/bin/deno \
 && printf '#!/bin/sh\nexec /usr/local/bin/yt-dlp.real "$@" 2>/dev/null\n' > /usr/local/bin/yt-dlp \
 && chmod a+rx /usr/local/bin/yt-dlp \
 && rm -rf /var/lib/apt/lists/*
USER lavalink
