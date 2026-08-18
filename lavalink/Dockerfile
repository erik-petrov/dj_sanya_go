# Lavalink + yt-dlp. LavaSrc shells out to the `yt-dlp` binary, absent from the
# base image. Use the standalone build: it bundles its own modern Python, so the
# base image's Python 3.10 (and its deprecation notice that pollutes LavaSrc's
# JSON parse) is irrelevant. Wrapper drops stderr as a belt — LavaSrc merges
# stderr into the stdout it parses, so any chatter would break it.
#
# arch: yt-dlp_linux = x86_64/glibc. On arm64 use yt-dlp_linux_aarch64.
# Update: docker compose build --pull --no-cache lavalink
# ponytail: no bgutil poToken plugin yet — residential IP may not need it. Add to
# /etc/yt-dlp/plugins/ only if a video actually fails with a poToken error.
FROM ghcr.io/lavalink-devs/lavalink:4
USER root
ADD https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux /usr/local/bin/yt-dlp.real
RUN chmod a+rx /usr/local/bin/yt-dlp.real \
 && printf '#!/bin/sh\nexec /usr/local/bin/yt-dlp.real "$@" 2>/dev/null\n' > /usr/local/bin/yt-dlp \
 && chmod a+rx /usr/local/bin/yt-dlp
USER lavalink
