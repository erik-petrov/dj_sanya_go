# Lavalink + yt-dlp. LavaSrc shells out to `yt-dlp`, absent from the base image.
#  - yt-dlp_linux (NIGHTLY): standalone build, bundles its own Python (base ships
#    3.10, whose deprecation notice would pollute LavaSrc's JSON parse). Nightly,
#    not stable: stable 2026.07.04 had the youtube stream-403 bug (yt-dlp#17456);
#    nightly's default client (visionos/HLS) downloads fine, no cookies needed.
#  - deno: JS runtime yt-dlp needs to run YouTube's player JS and decipher
#    signatures. Without it only storyboard formats resolve.
#  - bgutil plugin zip: youtube's android_vr client now requires a GVS poToken
#    for non-18 formats (yt-dlp#17348), else the stream 403s. The plugin fetches
#    poTokens from the bgutil service (base_url passed via customPlaybackArgs in
#    application.yml) and bakes pot= into the URL. LavaSrc doesn't forward
#    yt-dlp's http_headers, so the poToken (not a UA match) is what authorizes it.
#  - wrapper: drop yt-dlp stderr so its chatter can't break LavaSrc's stdout JSON.
#
# arch: x86_64/glibc. On arm64 use yt-dlp_linux_aarch64 + deno-aarch64-...
# Update: docker compose build --pull --no-cache lavalink
FROM ghcr.io/lavalink-devs/lavalink:4
USER root
ADD https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp_linux /usr/local/bin/yt-dlp.real
ADD https://github.com/denoland/deno/releases/latest/download/deno-x86_64-unknown-linux-gnu.zip /tmp/deno.zip
ADD https://github.com/Brainicism/bgutil-ytdlp-pot-provider/releases/latest/download/bgutil-ytdlp-pot-provider.zip /etc/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip
RUN apt-get update && apt-get install -y --no-install-recommends unzip \
 && unzip -o /tmp/deno.zip -d /usr/local/bin && rm /tmp/deno.zip \
 && chmod a+rx /usr/local/bin/yt-dlp.real /usr/local/bin/deno \
 && chmod a+r /etc/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip \
 && printf '#!/bin/sh\nexec /usr/local/bin/yt-dlp.real "$@" 2>/dev/null\n' > /usr/local/bin/yt-dlp \
 && chmod a+rx /usr/local/bin/yt-dlp \
 && rm -rf /var/lib/apt/lists/*
USER lavalink
