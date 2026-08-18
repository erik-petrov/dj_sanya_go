# Lavalink + yt-dlp. LavaSrc's yt-dlp source shells out to the `yt-dlp` binary,
# which the base image doesn't ship. Install yt-dlp + the bgutil poToken CLIENT
# plugin (auto-discovered via the yt_dlp_plugins namespace) into an isolated venv.
#
# LavaSrc merges yt-dlp's stderr into the stdout it parses as JSON, so ANY stderr
# line (e.g. yt-dlp's "Python 3.10 deprecated" notice) breaks parsing. Wrap the
# binary to drop stderr — LavaSrc then always gets clean JSON. For debugging, call
# the real binary directly: /opt/ytdlp/bin/yt-dlp ... 2>&1
#
# yt-dlp moves fast; rebuild to update:  docker compose build --pull lavalink
# ponytail: builds on the base's Python (3.10). If a future yt-dlp drops 3.10,
# add Python 3.11 (deadsnakes) here; the wrapper already hides the deprecation.
FROM ghcr.io/lavalink-devs/lavalink:4
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 python3-venv \
 && python3 -m venv /opt/ytdlp \
 && /opt/ytdlp/bin/pip install --no-cache-dir -U pip yt-dlp bgutil-ytdlp-pot-provider \
 && printf '#!/bin/sh\nexec /opt/ytdlp/bin/yt-dlp "$@" 2>/dev/null\n' > /usr/local/bin/yt-dlp \
 && chmod a+rx /usr/local/bin/yt-dlp \
 && rm -rf /var/lib/apt/lists/*
USER lavalink
