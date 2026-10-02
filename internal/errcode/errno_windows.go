//go:build windows

package errcode

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// Winsock errors and the codes they map to. Go reports socket failures as
// these values (inside *net.OpError and *os.SyscallError), so they can be
// recognised whatever language Windows shows its messages in.
var errnoCodes = map[syscall.Errno]string{
	windows.WSAECONNREFUSED:  NetRefused,
	windows.WSAETIMEDOUT:     NetTimeout,
	windows.WSAEHOSTUNREACH:  NetUnreachable,
	windows.WSAENETUNREACH:   NetUnreachable,
	windows.WSAECONNRESET:    NetReset,
	windows.WSAECONNABORTED:  NetReset,
	windows.WSAEADDRINUSE:    NetAddressInUse,
	windows.WSAEACCES:        NetAccessDenied,
	windows.WSAEADDRNOTAVAIL: NetAddressNotAvail,
	// ERROR_CONNECTION_REFUSED and friends come from overlapped I/O.
	windows.ERROR_CONNECTION_REFUSED:  NetRefused,
	windows.ERROR_HOST_UNREACHABLE:    NetUnreachable,
	windows.ERROR_NETWORK_UNREACHABLE: NetUnreachable,
	windows.ERROR_SEM_TIMEOUT:         NetTimeout,
}

func fromErrno(err error) string {
	if errno, ok := err.(syscall.Errno); ok {
		return errnoCodes[errno]
	}
	return ""
}

// SystemMessage returns the text Windows gives for code's errors, in the
// language Windows is set to. The Xray engine only passes on the text of
// some socket errors, so it compares against these.
func SystemMessage(code string) []string {
	var out []string
	for errno, c := range errnoCodes {
		if c == code {
			out = append(out, errno.Error())
		}
	}
	return out
}
