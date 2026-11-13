#!/usr/bin/env bash
# Create or update the sticky hackme-fuzz-action comment on a PR.
# Never fails the job: fork PRs get a read-only token, so a 403 is expected.
set -euo pipefail

: "${GH_TOKEN:?}" "${HFA_REPO:?}" "${HFA_PR:?}" "${HFA_BODY:?}"
marker='<!-- hackme-fuzz-action -->'

warn() { echo "::warning title=HackMe Dig::$*"; exit 0; }

[[ "$HFA_PR" =~ ^[1-9][0-9]{0,9}$ ]] || warn "not a PR number: skipping comment"
[[ "$HFA_REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || warn "unexpected repository name: skipping comment"
[ -s "$HFA_BODY" ] || warn "no comment body: skipping comment"
head -n1 "$HFA_BODY" | grep -qxF "$marker" || warn "comment body has no marker: skipping comment"

# Only ever edit a comment we wrote ourselves: same author as this token and
# the marker on the first line. A PR author can paste the marker, but not as us.
# GITHUB_TOKEN can't read /user (403), and on errors gh prints the error JSON
# to stdout even with --jq — so only trust a clean exit with a login-shaped value.
me=""
if login="$(gh api user --jq .login 2>/dev/null)" && [[ "$login" =~ ^[A-Za-z0-9-]+(\[bot\])?$ ]]; then
  me="$login"
fi
[ -n "$me" ] || me="github-actions[bot]"

existing="$(
  gh api --paginate "repos/$HFA_REPO/issues/$HFA_PR/comments?per_page=100" \
    --jq '.[] | select(.body | startswith("<!-- hackme-fuzz-action -->")) | [.id, .user.login] | @tsv' \
    2>/dev/null \
  | awk -F '\t' -v me="$me" '$2 == me { id = $1 } END { print id }'
)" || existing=""

if [[ "$existing" =~ ^[0-9]+$ ]]; then
  gh api -X PATCH "repos/$HFA_REPO/issues/comments/$existing" -F "body=@$HFA_BODY" >/dev/null \
    || warn "could not update the PR comment (read-only token on a fork PR?)"
  echo "Updated PR comment $existing"
else
  gh api -X POST "repos/$HFA_REPO/issues/$HFA_PR/comments" -F "body=@$HFA_BODY" >/dev/null \
    || warn "could not post the PR comment (read-only token on a fork PR?)"
  echo "Posted PR comment"
fi
