# Sanitize yt-dlp -J output before LavaSrc parses it. LavaSrc 4.8.3's yt-dlp source
# dereferences entry.title/uploader/thumbnail with no null check, so a flat-playlist
# (which never fills the singular thumbnail, and leaves deleted/private videos with
# null title/uploader) makes it throw a NullPointerException and the whole playlist
# load fails. Drop unavailable entries (null title — they'd also stall the queue) and
# null-fill the rest. Single-video output (playback) passes through unchanged except
# harmless null-fills. Anything that isn't a JSON object is passed through verbatim.
import sys
import json

data = sys.stdin.read()
try:
    obj = json.loads(data)
except Exception:
    sys.stdout.write(data)
    sys.exit(0)

if not isinstance(obj, dict):
    sys.stdout.write(data)
    sys.exit(0)


def fill(d):
    for k in ("title", "uploader", "thumbnail"):
        if d.get(k) is None:
            d[k] = ""
    return d


entries = obj.get("entries")
if isinstance(entries, list):
    obj["entries"] = [
        fill(e)
        for e in entries
        if isinstance(e, dict) and e.get("title") and e.get("id")
    ]
fill(obj)

json.dump(obj, sys.stdout)
