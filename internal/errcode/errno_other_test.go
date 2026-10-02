//go:build !windows

package errcode

import "syscall"

var refusedErrno = syscall.ECONNREFUSED
