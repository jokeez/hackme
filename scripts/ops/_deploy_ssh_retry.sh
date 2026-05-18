# shellcheck shell=bash
# Source from deploy_*.sh:  . "$(dirname "${BASH_SOURCE[0]}")/_deploy_ssh_retry.sh"
#
# Flaky VPS SSH (Connection refused) — retry rsync/scp/ssh until success or limit.
#
#   HACKME_DEPLOY_SSH_RETRIES            default 30
#   HACKME_DEPLOY_SSH_RETRY_SLEEP_SEC   default 5
#
deploy_ssh_retry_run() {
  local max="${HACKME_DEPLOY_SSH_RETRIES:-30}"
  local delay="${HACKME_DEPLOY_SSH_RETRY_SLEEP_SEC:-5}"
  local n=1
  while (( n <= max )); do
    if "$@"; then
      return 0
    fi
    if (( n < max )); then
      echo "[deploy-ssh-retry] ${n}/${max} failed, sleep ${delay}s" >&2
      sleep "$delay"
    fi
    n=$((n + 1))
  done
  echo "[deploy-ssh-retry] giving up after ${max} attempts" >&2
  return 1
}
