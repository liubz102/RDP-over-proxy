//go:build windows

package winx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Listener is a TCP socket that listens for connections, and the process
// that owns it.
type Listener struct {
	Addr netip.AddrPort
	PID  uint32
}

var procGetExtendedTcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// tcpTableOwnerPIDListener is TCP_TABLE_OWNER_PID_LISTENER: the listening
// sockets, with the process that owns each.
const tcpTableOwnerPIDListener = 3

// Listeners lists this computer's TCP sockets that listen for connections,
// IPv4 and IPv6, with the process that owns each. It needs no administrator
// rights. A family that cannot be read is left out; it fails only when
// neither can be.
func Listeners() ([]Listener, error) {
	v4, err4 := listeners(windows.AF_INET)
	v6, err6 := listeners(windows.AF_INET6)
	if err4 != nil && err6 != nil {
		return nil, fmt.Errorf("list the listening sockets: %w", errors.Join(err4, err6))
	}
	return append(v4, v6...), nil
}

// The rows of MIB_TCPTABLE_OWNER_PID and MIB_TCP6TABLE_OWNER_PID, after the
// table's DWORD count of rows.
const (
	tcpRowSize  = 24 // MIB_TCPROW_OWNER_PID
	tcp6RowSize = 56 // MIB_TCP6ROW_OWNER_PID
)

// statusUnsuccessful is STATUS_UNSUCCESSFUL, which GetExtendedTcpTable
// also answers when the table changed between the size query and the read
// (seen by psutil, giampaolo/psutil#1294).
const statusUnsuccessful = 0xC0000001

// tcpTable asks for the table into buf, or for its size with an empty buf.
func tcpTable(buf []byte, size *uint32, family uint32) uintptr {
	var table unsafe.Pointer
	if len(buf) > 0 {
		table = unsafe.Pointer(&buf[0])
	}
	r, _, _ := procGetExtendedTcpTable.Call(uintptr(table), uintptr(unsafe.Pointer(size)), 0,
		uintptr(family), tcpTableOwnerPIDListener, 0)
	return r
}

func listeners(family uint32) ([]Listener, error) {
	var buf []byte
	var size uint32
	for {
		r := tcpTable(buf, &size, family)
		if r == 0 {
			break
		}
		switch {
		case windows.Errno(r) == windows.ERROR_INSUFFICIENT_BUFFER:
			// Sockets that open between the calls make the table bigger than
			// the size given before; each refusal says what it needs now.
			buf = make([]byte, size)
		case r == statusUnsuccessful && len(buf) > 0:
			// The table may have changed under the read. Ask for its size
			// again: one that did change is read again; one that did not
			// failed for another reason.
			had := len(buf)
			size = 0
			if windows.Errno(tcpTable(nil, &size, family)) != windows.ERROR_INSUFFICIENT_BUFFER || int(size) == had {
				return nil, windows.NTStatus(r)
			}
			buf = make([]byte, size)
		default:
			return nil, windows.Errno(r)
		}
	}
	if int(size) < len(buf) {
		buf = buf[:size]
	}
	if len(buf) < 4 {
		return nil, nil
	}
	n := int(binary.LittleEndian.Uint32(buf))
	rowSize := tcpRowSize
	if family == windows.AF_INET6 {
		rowSize = tcp6RowSize
	}
	if 4+n*rowSize > len(buf) {
		return nil, errors.New("the TCP table is shorter than its count of rows")
	}
	out := make([]Listener, 0, n)
	for i := range n {
		row := buf[4+i*rowSize : 4+(i+1)*rowSize]
		var addr netip.Addr
		var port uint16
		var pid uint32
		if family == windows.AF_INET {
			// dwLocalAddr holds the address in network byte order, and the
			// low 16 bits of dwLocalPort the port in network byte order.
			addr = netip.AddrFrom4([4]byte(row[4:8]))
			port = binary.BigEndian.Uint16(row[8:10])
			pid = binary.LittleEndian.Uint32(row[20:24])
		} else {
			addr = netip.AddrFrom16([16]byte(row[0:16]))
			port = binary.BigEndian.Uint16(row[20:22])
			pid = binary.LittleEndian.Uint32(row[52:56])
		}
		out = append(out, Listener{Addr: netip.AddrPortFrom(addr, port), PID: pid})
	}
	return out, nil
}

// Process is a running program: its process ID, the ID of the process that
// started it (which may have exited since, and its ID been reused), the
// file name of its executable, such as "xray.exe", and when it started
// (zero when Windows does not tell, as for the system's own processes).
type Process struct {
	PID       uint32
	ParentPID uint32
	Exe       string
	Created   time.Time
}

// Processes lists the running processes, other users' too. It needs no
// administrator rights.
func Processes() ([]Process, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []Process
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out = append(out, Process{PID: e.ProcessID, ParentPID: e.ParentProcessID, Exe: windows.UTF16ToString(e.ExeFile[:]),
			Created: created(e.ProcessID)})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return out, nil
}

// created is when a process started, or zero when it cannot be asked.
func created(pid uint32) time.Time {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return time.Time{}
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &creation, &exit, &kernel, &user) != nil {
		return time.Time{}
	}
	return time.Unix(0, creation.Nanoseconds())
}
