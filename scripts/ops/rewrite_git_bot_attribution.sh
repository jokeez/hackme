#!/usr/bin/env bash
# Rewrite main history: normalize bot-agent author/committer + strip co-author trailers.
#
#   bash scripts/ops/rewrite_git_bot_attribution.sh
#   git push --force-with-lease origin main
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

NAME="${GIT_AUTHOR_REWRITE_NAME:-jokeez}"
EMAIL="${GIT_AUTHOR_REWRITE_EMAIL:-dney777666@gmail.com}"
BOT_EMAIL="${GIT_BOT_EMAIL:-cursoragent@cursor.com}"

if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  echo "[rewrite-attribution] stash or commit working tree first" >&2
  git status -sb
  exit 1
fi

export NAME EMAIL BOT_EMAIL
FILTER_BRANCH_SQUELCH_WARNING=1 git filter-branch -f \
  --env-filter '
bot_author() {
  case "$1" in
    *'"$BOT_EMAIL"'*|*"Cursor Agent"*|*"Cursor <"*) return 0 ;;
  esac
  return 1
}
if bot_author "$GIT_AUTHOR_NAME" || bot_author "$GIT_AUTHOR_EMAIL"; then
  export GIT_AUTHOR_NAME="$NAME"
  export GIT_AUTHOR_EMAIL="$EMAIL"
fi
if bot_author "$GIT_COMMITTER_NAME" || bot_author "$GIT_COMMITTER_EMAIL"; then
  export GIT_COMMITTER_NAME="$NAME"
  export GIT_COMMITTER_EMAIL="$EMAIL"
fi
' \
  --msg-filter 'grep -viE "^Co-authored-by:.*cursor|^Co-authored-by:.*cursoragent|^Made-with:.*cursor" || true' \
  -- main

git for-each-ref --format='%(refname)' refs/original/ | xargs -r -n1 git update-ref -d 2>/dev/null || true

left=$(git log main --format='%an <%ae>' | grep -ci cursoragent || true)
echo "[rewrite-attribution] bot-agent authors left on main: ${left:-0}"
if [[ "${left:-0}" -gt 0 ]]; then
  git log main --format='%h %an <%ae>' | grep -i cursoragent | head -5
  exit 1
fi
echo "[rewrite-attribution] OK — push: git push --force-with-lease origin main"
