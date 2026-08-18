# Lavalink + yt-dlp. LavaSrc's yt-dlp source shells out to the `yt-dlp` binary,
# which the base image doesn't ship. Install yt-dlp + the bgutil poToken CLIENT
# plugin (yt-dlp auto-discovers it via the yt_dlp_plugins namespace) into an
# isolated venv, then symlink yt-dlp onto PATH. The venv avoids PEP-668 and old
# system-pip flag issues on the Debian/Ubuntu base. (If the base is Alpine, swap
# the apt-get line for `apk add --no-cache python3`.)
#
# yt-dlp moves fast; rebuild to update:  docker compose build --pull lavalink
FROM ghcr.io/lavalink-devs/lavalink:4
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 python3-venv \
 && python3 -m venv /opt/ytdlp \
 && /opt/ytdlp/bin/pip install --no-cache-dir -U pip yt-dlp bgutil-ytdlp-pot-provider \
 && ln -s /opt/ytdlp/bin/yt-dlp /usr/local/bin/yt-dlp \
 && rm -rf /var/lib/apt/lists/*
USER lavalink
