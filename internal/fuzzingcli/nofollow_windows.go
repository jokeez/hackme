//go:build windows

package fuzzingcli

// Windows has no O_NOFOLLOW; readRegularNoFollow still rejects non-regular files
// via Stat on the opened handle. No Hunt/Dig cage runs on Windows.
const oNoFollow = 0
