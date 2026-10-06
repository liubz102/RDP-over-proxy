//go:build windows

package localproxy

import "github.com/liubz102/RDP-over-proxy/internal/winx"

// Gather reads the facts from Windows; it changes nothing. A proxy setting
// that cannot be read is left out: the listening programs say more.
func Gather() (Facts, error) {
	listeners, err := winx.Listeners()
	if err != nil {
		return Facts{}, err
	}
	procs, err := winx.Processes()
	if err != nil {
		return Facts{}, err
	}
	f := Facts{
		Listeners: make([]Listener, 0, len(listeners)),
		Processes: make([]Process, 0, len(procs)),
	}
	for _, l := range listeners {
		f.Listeners = append(f.Listeners, Listener{Addr: l.Addr, PID: l.PID})
	}
	for _, p := range procs {
		f.Processes = append(f.Processes, Process{PID: p.PID, ParentPID: p.ParentPID, Exe: p.Exe, Created: p.Created})
	}
	if c, err := winx.IEProxyConfig(); err == nil {
		f.SystemProxy = c.Proxy
	}
	return f, nil
}
