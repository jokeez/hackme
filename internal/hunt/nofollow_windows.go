//go:build windows

package hunt

// Windows has no O_NOFOLLOW; callers still Stat the opened handle. No Hunt cage runs on Windows.
const oNoFollow = 0
