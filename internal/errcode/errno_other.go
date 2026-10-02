//go:build !windows

package errcode

import "syscall"

// The app only runs on Windows; this keeps the pure packages that use
// errcode building and testable elsewhere.
func fromErrno(err error) string {
	errno, ok := err.(syscall.Errno)
	if !ok {
		return ""
	}
	switch errno {
	case syscall.ECONNREFUSED:
		return NetRefused
	case syscall.ETIMEDOUT:
		return NetTimeout
	case syscall.ECONNRESET, syscall.ECONNABORTED:
		return NetReset
	case syscall.EADDRINUSE:
		return NetAddressInUse
	}
	return ""
}

// SystemMessage returns nothing outside Windows.
func SystemMessage(string) []string { return nil }
