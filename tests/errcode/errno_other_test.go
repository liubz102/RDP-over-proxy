//go:build !windows

package errcode_test

import (
	"syscall"

	. "github.com/liubz102/RDP-over-proxy/internal/errcode"
)

var refusedErrno = syscall.ECONNREFUSED
