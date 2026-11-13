//go:build !windows

package hunt

import "syscall"

const oNoFollow = syscall.O_NOFOLLOW
