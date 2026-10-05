//go:build windows

package winx

import (
	"fmt"
	"runtime"
	"slices"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procSetWinEventHook    = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent     = user32.NewProc("UnhookWinEvent")
)

const (
	eventObjectShow       = 0x8002
	eventObjectNameChange = 0x800C
	wineventOutOfContext  = 0x0000
	objidWindow           = 0
	childidSelf           = 0
	pmNoRemove            = 0x0000
	wmQuit                = 0x0012
)

// msg is Win32's MSG, for a message the watch thread waits for and never
// looks at.
type msg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	private uint32
}

// An out-of-context window event hook delivers its events on the thread
// that set it, while that thread waits for messages. One callback serves
// every watch (see enumCallback for why); watches finds the watch a hook
// belongs to.
var (
	watchMu sync.Mutex
	watches = map[uintptr]*watch{}

	winEventCallback = windows.NewCallback(func(hook, _, hwnd, object, child, _, _ uintptr) uintptr {
		if hwnd == 0 || int32(object) != objidWindow || int32(child) != childidSelf {
			return 0 // about a part of a window, not the window itself
		}
		watchMu.Lock()
		w := watches[hook]
		watchMu.Unlock()
		if w != nil && topLevel(windows.HWND(hwnd)) {
			w.notify(windows.HWND(hwnd))
		}
		return 0
	})
)

// WatchWindows calls changed with each top-level window of the process pid
// that is shown or gets a new title, and first with each window the process
// already has, hidden ones included; between them nothing is missed, however
// early the window appeared. changed runs on a thread of the watch's own, one
// call at a time, taking the windows in the order their changes came (a
// window already waiting its turn waits once); it may get a window it has
// seen before, and one that is gone by then. stop ends the watch, waiting for
// a call in progress to return.
func WatchWindows(pid int, changed func(windows.HWND)) (stop func(), err error) {
	type start struct {
		thread uint32
		err    error
	}
	started := make(chan start, 1)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The thread stays locked, so it ends with the goroutine and takes
		// its message queue along.
		runtime.LockOSThread()
		var m msg
		// Give the thread its message queue before stop can post to it.
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmNoRemove)
		w := &watch{changed: changed}
		hooks, err := hookWindowEvents(pid, w)
		if err != nil {
			started <- start{err: err}
			return
		}
		defer unhook(hooks)
		started <- start{thread: windows.GetCurrentThreadId()}
		w.notify(processWindows(pid)...)
		for {
			// The events arrive inside GetMessage. Nothing is posted to this
			// thread but stop's WM_QUIT, and it has no windows to dispatch to.
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			switch int32(r) {
			case 0:
				return // WM_QUIT
			case -1:
				// It cannot fail with these arguments. Should it all the same,
				// the thread waits until stop has posted to it: a thread ID is
				// reused once its thread has ended, and the post must not reach
				// another thread.
				<-quit
				return
			}
		}
	}()
	s := <-started
	if s.err != nil {
		<-done
		return nil, s.err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			procPostThreadMessageW.Call(uintptr(s.thread), wmQuit, 0, 0)
			close(quit)
			<-done
		})
	}, nil
}

// watch is one WatchWindows. Its fields belong to its thread.
type watch struct {
	changed func(windows.HWND)
	// busy is set while changed runs. changed may send a message to another
	// program's window (SetTitle does), and while the thread waits for the
	// answer it can be handed the next event. That event's window waits in
	// pending, so changed never runs inside itself and the windows are taken
	// in order.
	busy    bool
	pending []windows.HWND
}

func (w *watch) notify(hwnds ...windows.HWND) {
	for _, h := range hwnds {
		if !slices.Contains(w.pending, h) { // one look at its current state covers both
			w.pending = append(w.pending, h)
		}
	}
	if w.busy {
		return
	}
	w.busy = true
	for len(w.pending) > 0 {
		next := w.pending[0]
		w.pending = w.pending[1:]
		w.changed(next)
	}
	w.busy = false
}

// hookWindowEvents hooks the events of the process's windows that matter to
// a watch, for delivery to w on this thread.
func hookWindowEvents(pid int, w *watch) ([]uintptr, error) {
	var hooks []uintptr
	for _, event := range []uintptr{eventObjectShow, eventObjectNameChange} {
		h, _, err := procSetWinEventHook.Call(event, event, 0, winEventCallback, uintptr(pid), 0, wineventOutOfContext)
		if h == 0 {
			unhook(hooks)
			return nil, fmt.Errorf("SetWinEventHook: %w", err)
		}
		watchMu.Lock()
		watches[h] = w
		watchMu.Unlock()
		hooks = append(hooks, h)
	}
	return hooks, nil
}

func unhook(hooks []uintptr) {
	for _, h := range hooks {
		// Forgotten first: once unhooked, its handle can go to another watch.
		watchMu.Lock()
		delete(watches, h)
		watchMu.Unlock()
		procUnhookWinEvent.Call(h)
	}
}
