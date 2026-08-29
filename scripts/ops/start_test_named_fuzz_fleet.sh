#!/usr/bin/env bash
# DEPRECATED — use start_test_named_fleet.sh (PoH+fuzz same worker_id, no *-fuzz sybil rows).
# Kept as forwarder for old docs/cron references.
exec "$(dirname "$0")/start_test_named_fleet.sh" "$@"
