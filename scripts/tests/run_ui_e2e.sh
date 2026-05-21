#!/usr/bin/env bash
# Install Playwright (first run) and execute dashboard E2E against e2e_stack.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
E2E_DIR="$ROOT_DIR/tests/e2e"

export E2E_BASE_URL="${E2E_BASE_URL:-http://127.0.0.1:19080}"
export E2E_ADMIN_TOKEN="${E2E_ADMIN_TOKEN:-e2e-admin-token-test}"

cd "$E2E_DIR"
if [[ ! -d node_modules/@playwright/test ]]; then
  npm install --no-audit --no-fund
  npx playwright install chromium
fi

npx playwright test "$@"
EXIT=$?

bash "$ROOT_DIR/scripts/tests/e2e_stack_stop.sh" 2>/dev/null || true
exit "$EXIT"
