# Lavalink + yt-dlp. LavaSrc's yt-dlp source shells out to the `yt-dlp` binary,
# which the base image doesn't ship. The base ships Python 3.10, and yt-dlp emits
# a "Python 3.10 deprecated" notice that pollutes the JSON LavaSrc parses — so
# pull Python 3.11 from deadsnakes and build an isolated venv on it. Install
# yt-dlp + the bgutil poToken CLIENT plugin (auto-discovered via the
# yt_dlp_plugins namespace) so yt-dlp can mint poTokens and stream SABR videos.
#
# yt-dlp moves fast; rebuild to update:  docker compose build --pull lavalink
FROM ghcr.io/lavalink-devs/lavalink:4
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends software-properties-common \
 && add-apt-repository -y ppa:deadsnakes/ppa \
 && apt-get update \
 && apt-get install -y --no-install-recommends python3.11 python3.11-venv \
 && python3.11 -m venv /opt/ytdlp \
 && /opt/ytdlp/bin/pip install --no-cache-dir -U pip yt-dlp bgutil-ytdlp-pot-provider \
 && ln -s /opt/ytdlp/bin/yt-dlp /usr/local/bin/yt-dlp \
 && rm -rf /var/lib/apt/lists/*
USER lavalink
