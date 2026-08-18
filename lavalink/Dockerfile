# Lavalink + yt-dlp. LavaSrc's yt-dlp source shells out to the `yt-dlp` binary,
# which the base image doesn't ship. Add Python + yt-dlp + the bgutil poToken
# CLIENT plugin (yt-dlp auto-discovers it via the yt_dlp_plugins namespace), so
# yt-dlp fetches tokens from the bgutil service and can stream SABR videos that
# youtube-source can't.
#
# yt-dlp moves fast; rebuild to update:  docker compose build --pull lavalink
FROM ghcr.io/lavalink-devs/lavalink:4
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 python3-pip \
 && pip3 install --break-system-packages --no-cache-dir -U yt-dlp bgutil-ytdlp-pot-provider \
 && rm -rf /var/lib/apt/lists/*
USER lavalink
