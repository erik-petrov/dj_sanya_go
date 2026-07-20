#!/bin/sh
# Sync gitignored secret/config files to the deployment server. Invoked by the
# pre-push hook (hooks/pre-push) so these files reach the server on every push,
# since git itself never carries them (they're in .gitignore).
#
# Never blocks the push: on any problem it warns and exits 0.
#
# Configure via deploy.env (gitignored) at the repo root — see deploy.env.example.
set -u

root=$(git rev-parse --show-toplevel) || exit 0
cfg="$root/deploy.env"

if [ ! -f "$cfg" ]; then
  echo "[sync-secrets] no deploy.env - skipping (copy deploy.env.example to set it up)" >&2
  exit 0
fi
# shellcheck source=/dev/null
. "$cfg"

if [ -z "${DEPLOY_HOST:-}" ] || [ -z "${DEPLOY_PATH:-}" ]; then
  echo "[sync-secrets] DEPLOY_HOST / DEPLOY_PATH unset in deploy.env - skipping" >&2
  exit 0
fi

# Gitignored files to carry to the server (paths relative to the repo root).
# NOTE: .env is intentionally NOT synced — the server runs a different bot
# account, so its tokens must stay independent from this machine's.
files="lavalink/application.yml cookies.txt"

failed=0
for f in $files; do
  if [ ! -f "$root/$f" ]; then
    echo "[sync-secrets] - $f absent locally, skip"
    continue
  fi
  # Ensure the target subdirectory exists, then copy the file over.
  if ssh -n "$DEPLOY_HOST" "mkdir -p \"$DEPLOY_PATH/$(dirname "$f")\"" \
     && scp -q "$root/$f" "$DEPLOY_HOST:$DEPLOY_PATH/$f"; then
    echo "[sync-secrets] + $f"
  else
    echo "[sync-secrets] ! failed to sync $f" >&2
    failed=1
  fi
done

[ "$failed" -eq 0 ] || echo "[sync-secrets] some files did not sync (push continues anyway)" >&2
exit 0
