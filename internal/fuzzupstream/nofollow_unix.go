//go:build !windows

package fuzzupstream

import "syscall"

const oNoFollow = syscall.O_NOFOLLOW
