#!/usr/bin/env bash
# Deprecated name — forwards to display rig (N cosmetic ids, one wallet).
exec "$(dirname "$0")/start_local_pool_display_rig.sh" "$@"
