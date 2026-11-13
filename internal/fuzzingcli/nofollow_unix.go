//go:build !windows

package fuzzingcli

import "syscall"

const oNoFollow = syscall.O_NOFOLLOW
