//go:build windows

package winx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	winhttp                                   = windows.NewLazySystemDLL("winhttp.dll")
	procWinHttpGetIEProxyConfigForCurrentUser = winhttp.NewProc("WinHttpGetIEProxyConfigForCurrentUser")
	procWinHttpOpen                           = winhttp.NewProc("WinHttpOpen")
	procWinHttpCreateProxyResolver            = winhttp.NewProc("WinHttpCreateProxyResolver")
	procWinHttpSetStatusCallback              = winhttp.NewProc("WinHttpSetStatusCallback")
	procWinHttpGetProxyForUrlEx               = winhttp.NewProc("WinHttpGetProxyForUrlEx")
	procWinHttpGetProxyResult                 = winhttp.NewProc("WinHttpGetProxyResult")
	procWinHttpFreeProxyResult                = winhttp.NewProc("WinHttpFreeProxyResult")
	procWinHttpCloseHandle                    = winhttp.NewProc("WinHttpCloseHandle")
	procGlobalFree                            = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")
)

// IEProxy is the current user's Internet proxy settings: what the Settings
// app's proxy page sets, and what Windows' own programs follow.
type IEProxy struct {
	// AutoDetect: "Automatically detect settings" (WPAD).
	AutoDetect bool
	// Script is the setup script's address ("Use setup script"), or "".
	Script string
	// Proxy is the manual proxy server while it is on, as Windows keeps it:
	// "host:port" for every protocol, or "http=host:port;https=…"; else "".
	Proxy string
	// Bypass lists what the manual proxy is not used for:
	// "localhost;127.*;*.example.com;<local>".
	Bypass string
}

// WINHTTP_CURRENT_USER_IE_PROXY_CONFIG.
type ieProxyConfig struct {
	autoDetect    int32 // BOOL
	autoConfigURL *uint16
	proxy         *uint16
	proxyBypass   *uint16
}

// IEProxyConfig reads the current user's Internet proxy settings
// (WinHttpGetIEProxyConfigForCurrentUser).
func IEProxyConfig() (IEProxy, error) {
	var c ieProxyConfig
	if r, _, err := procWinHttpGetIEProxyConfigForCurrentUser.Call(uintptr(unsafe.Pointer(&c))); r == 0 {
		return IEProxy{}, winhttpError(err)
	}
	defer globalFree(c.autoConfigURL, c.proxy, c.proxyBypass)
	return IEProxy{
		AutoDetect: c.autoDetect != 0,
		Script:     windows.UTF16PtrToString(c.autoConfigURL),
		Proxy:      windows.UTF16PtrToString(c.proxy),
		Bypass:     windows.UTF16PtrToString(c.proxyBypass),
	}, nil
}

// WINHTTP_AUTOPROXY_OPTIONS.
type autoProxyOptions struct {
	flags                 uint32
	autoDetectFlags       uint32
	autoConfigURL         *uint16
	reserved              uintptr
	reserved2             uint32
	autoLogonIfChallenged int32 // BOOL
}

// WINHTTP_PROXY_RESULT.
type proxyResult struct {
	count   uint32
	entries *proxyResultEntry
}

// WINHTTP_PROXY_RESULT_ENTRY.
type proxyResultEntry struct {
	proxy  int32  // BOOL
	bypass int32  // BOOL
	scheme uint32 // INTERNET_SCHEME
	host   *uint16
	port   uint16
}

// WINHTTP_ASYNC_RESULT.
type asyncResult struct {
	api uintptr // the call that failed
	err uint32
}

const (
	winhttpAccessTypeNoProxy                    = 1
	winhttpFlagAsync                            = 0x10000000
	winhttpAutoproxyAutoDetect                  = 0x1
	winhttpAutoproxyConfigURL                   = 0x2
	winhttpAutoDetectTypeDHCP                   = 0x1
	winhttpAutoDetectTypeDNSA                   = 0x2
	winhttpStatusRequestError                   = 0x00200000
	winhttpStatusGetProxyForURLComplete         = 0x01000000
	winhttpAPIGetProxyForURL                    = 6
	winhttpInvalidStatusCallback        uintptr = ^uintptr(0)
)

// ProxyEntry is one of the ways the automatic proxy configuration names
// for a URL.
type ProxyEntry struct {
	// Direct: connect directly (DIRECT).
	Direct bool
	// Scheme is how the proxy is spoken to: "http" (PROXY), "https" (over
	// TLS) or "socks"; WinHTTP's number for a scheme it has no name for.
	Scheme string
	Host   string
	Port   int
}

// ProxyForURL runs Windows' automatic proxy configuration for url:
// detection (WPAD) when autoDetect, then the setup script at script when it
// is set (WinHttpGetProxyForUrlEx). It returns the ways the configuration
// names for url, in its order.
//
// Detection and the script are Windows' to run: they take as long as the
// network does to answer, and Windows keeps what it found for a while.
// Cancelling ctx ends the wait; ProxyForURL then returns ctx's error.
func ProxyForURL(ctx context.Context, url string, autoDetect bool, script string) ([]ProxyEntry, error) {
	if !autoDetect && script == "" {
		return nil, errors.New("no automatic configuration is set")
	}
	if err := procWinHttpGetProxyForUrlEx.Find(); err != nil {
		return nil, err // before Windows 8
	}
	// WinHTTP reads the address and the options while it works, so they are
	// kept where the garbage collector neither frees nor moves them: in the
	// call, which proxyCalls holds until ProxyForURL returns.
	call := &proxyCall{opts: autoProxyOptions{autoLogonIfChallenged: 1}, done: make(chan uint32, 1)}
	var err error
	if call.url, err = windows.UTF16PtrFromString(url); err != nil {
		return nil, err
	}
	if autoDetect {
		call.opts.flags |= winhttpAutoproxyAutoDetect
		call.opts.autoDetectFlags = winhttpAutoDetectTypeDHCP | winhttpAutoDetectTypeDNSA
	}
	if script != "" {
		if call.opts.autoConfigURL, err = windows.UTF16PtrFromString(script); err != nil {
			return nil, err
		}
		call.opts.flags |= winhttpAutoproxyConfigURL
	}
	agent, err := windows.UTF16PtrFromString("RDP-over-proxy")
	if err != nil {
		return nil, err
	}
	session, _, err := procWinHttpOpen.Call(uintptr(unsafe.Pointer(agent)), winhttpAccessTypeNoProxy, 0, 0, winhttpFlagAsync)
	if session == 0 {
		return nil, winhttpError(err)
	}
	defer procWinHttpCloseHandle.Call(session)
	var resolver uintptr
	if r, _, _ := procWinHttpCreateProxyResolver.Call(session, uintptr(unsafe.Pointer(&resolver))); r != 0 {
		return nil, winhttpError(windows.Errno(r))
	}
	// Closing the resolver also cancels what it is doing.
	open := true
	closeResolver := func() {
		if open {
			open = false
			procWinHttpCloseHandle.Call(resolver)
		}
	}
	defer closeResolver()
	if r, _, err := procWinHttpSetStatusCallback.Call(resolver, proxyCallback,
		winhttpStatusRequestError|winhttpStatusGetProxyForURLComplete, 0); r == winhttpInvalidStatusCallback {
		return nil, winhttpError(err)
	}
	id := proxyCalls.add(call)
	defer proxyCalls.remove(id)
	if r, _, _ := procWinHttpGetProxyForUrlEx.Call(resolver, uintptr(unsafe.Pointer(call.url)),
		uintptr(unsafe.Pointer(&call.opts)), id); r != uintptr(windows.ERROR_IO_PENDING) {
		return nil, winhttpError(windows.Errno(r))
	}
	select {
	case code := <-call.done:
		if code != 0 {
			return nil, winhttpError(windows.Errno(code))
		}
		return proxyEntries(resolver)
	case <-ctx.Done():
		// The answer to a cancelled call, if any, finds no one waiting.
		closeResolver()
		return nil, ctx.Err()
	}
}

// proxyEntries reads the answer the resolver got.
func proxyEntries(resolver uintptr) ([]ProxyEntry, error) {
	var res proxyResult
	if r, _, _ := procWinHttpGetProxyResult.Call(resolver, uintptr(unsafe.Pointer(&res))); r != 0 {
		return nil, winhttpError(windows.Errno(r))
	}
	defer procWinHttpFreeProxyResult.Call(uintptr(unsafe.Pointer(&res)))
	out := make([]ProxyEntry, 0, res.count)
	for _, e := range unsafe.Slice(res.entries, res.count) {
		if e.proxy == 0 || e.bypass != 0 {
			out = append(out, ProxyEntry{Direct: true})
			continue
		}
		out = append(out, ProxyEntry{Scheme: schemeName(e.scheme), Host: windows.UTF16PtrToString(e.host), Port: int(e.port)})
	}
	return out, nil
}

// schemeName names an INTERNET_SCHEME.
func schemeName(s uint32) string {
	switch s {
	case 1:
		return "http"
	case 2:
		return "https"
	case 4:
		return "socks"
	}
	return strconv.FormatUint(uint64(s), 10)
}

// proxyCall is one ProxyForURL waiting for WinHTTP's answer.
type proxyCall struct {
	url  *uint16
	opts autoProxyOptions
	// done gets WinHTTP's error code, or 0 when the answer is ready. Only
	// the first counts.
	done chan uint32
}

// proxyCalls are the calls WinHTTP may still answer, by the number it hands
// back to proxyCallback with the answer.
var proxyCalls = callTable{calls: map[uintptr]*proxyCall{}}

type callTable struct {
	mu    sync.Mutex
	next  uintptr
	calls map[uintptr]*proxyCall
}

func (t *callTable) add(c *proxyCall) uintptr {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	t.calls[t.next] = c
	return t.next
}

func (t *callTable) remove(id uintptr) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.calls, id)
}

// answer passes WinHTTP's answer to the call id, when it still waits.
func (t *callTable) answer(id uintptr, code uint32) {
	t.mu.Lock()
	c := t.calls[id]
	t.mu.Unlock()
	if c != nil {
		select {
		case c.done <- code:
		default:
		}
	}
}

// proxyCallback receives WinHTTP's answers for ProxyForURL, on threads of
// WinHTTP's own, and passes them on without waiting. It is made once:
// callbacks cannot be freed.
var proxyCallback = windows.NewCallback(func(_, id, status uintptr, info *asyncResult, _ uintptr) uintptr {
	switch status {
	case winhttpStatusGetProxyForURLComplete:
		proxyCalls.answer(id, 0)
	case winhttpStatusRequestError:
		if info.api == winhttpAPIGetProxyForURL {
			proxyCalls.answer(id, info.err)
		}
	}
	return 0
})

// globalFree frees strings WinHTTP allocated.
func globalFree(ps ...*uint16) {
	for _, p := range ps {
		if p != nil {
			procGlobalFree.Call(uintptr(unsafe.Pointer(p)))
		}
	}
}

// The WinHTTP errors automatic proxy configuration reports. Their texts are
// in winhttp.dll, which Go's error text does not look in.
var winhttpErrors = map[windows.Errno]string{
	12166: "the setup script failed",
	12167: "the setup script could not be downloaded",
	12178: "the automatic proxy service failed",
	12180: "no automatic configuration was found on the network",
}

func winhttpError(err error) error {
	var errno windows.Errno
	if errors.As(err, &errno) {
		if text, ok := winhttpErrors[errno]; ok {
			return &winhttpErr{text: text, errno: errno}
		}
	}
	return err
}

// winhttpErr is a WinHTTP error with its text; errors.Is still finds the
// code.
type winhttpErr struct {
	text  string
	errno windows.Errno
}

func (e *winhttpErr) Error() string {
	return fmt.Sprintf("%s (WinHTTP error %d)", e.text, uint32(e.errno))
}
func (e *winhttpErr) Unwrap() error { return e.errno }

// WatchKey calls changed whenever a value of the key, or of a key below it,
// is set or removed, or a key below it made or removed
// (RegNotifyChangeKeyValue), until stop is called. The watch is armed again
// before changed runs, so a change made meanwhile is not missed. stop
// returns once changed is no longer running.
func WatchKey(root registry.Key, path string, changed func()) (stop func(), err error) {
	k, err := registry.OpenKey(root, path, registry.NOTIFY)
	if err != nil {
		return nil, err
	}
	notify, err := windows.CreateEvent(nil, 0, 0, nil) // resets itself when a wait sees it
	if err != nil {
		k.Close()
		return nil, err
	}
	quit, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(notify)
		k.Close()
		return nil, err
	}
	closeAll := func() {
		windows.CloseHandle(quit)
		windows.CloseHandle(notify)
		k.Close()
	}
	// Not tied to the thread that asks: goroutines move between threads.
	arm := func() error {
		return windows.RegNotifyChangeKeyValue(windows.Handle(k), true,
			windows.REG_NOTIFY_CHANGE_NAME|windows.REG_NOTIFY_CHANGE_LAST_SET|windows.REG_NOTIFY_THREAD_AGNOSTIC,
			notify, true)
	}
	if err := arm(); err != nil {
		closeAll()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			r, err := windows.WaitForMultipleObjects([]windows.Handle{notify, quit}, false, windows.INFINITE)
			if err != nil || r != windows.WAIT_OBJECT_0 {
				return
			}
			if arm() != nil {
				return
			}
			changed()
		}
	}()
	return func() {
		windows.SetEvent(quit)
		<-done
		closeAll()
	}, nil
}
