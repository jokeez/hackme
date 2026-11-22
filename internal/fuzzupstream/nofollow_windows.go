//go:build windows

package fuzzupstream

// Windows has no O_NOFOLLOW. Cache writes still unlink a planted symlink before create.
const oNoFollow = 0
