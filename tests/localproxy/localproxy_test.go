package localproxy_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/localproxy"

	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func listen(addr string, pid uint32) Listener {
	return Listener{Addr: netip.MustParseAddrPort(addr), PID: pid}
}

// at is a process start time, minutes after an arbitrary sign-in.
func at(minutes int) time.Time {
	return time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
}

// What a computer with v2rayN 3 shows: v2rayN runs the windowless v2ray,
// which listens on every address (its SOCKS5 port) and on 127.0.0.1 (its
// statistics API).
var v2rayN = Facts{
	Processes: []Process{
		{PID: 4, ParentPID: 0, Exe: "System"}, // no start time to be had
		{PID: 50, ParentPID: 40, Exe: "explorer.exe", Created: at(0)},
		{PID: 100, ParentPID: 50, Exe: "v2rayN.exe", Created: at(1)},
		{PID: 200, ParentPID: 100, Exe: "wv2ray.exe", Created: at(2)},
		{PID: 900, ParentPID: 4, Exe: "svchost.exe", Created: at(0)},
	},
	Listeners: []Listener{
		listen("0.0.0.0:135", 900),
		listen("[::]:10808", 200),
		listen("127.0.0.1:2023", 200),
		listen("[::]:135", 900),
	},
}

func TestCandidatesOfAnApp(t *testing.T) {
	got := Candidates(v2rayN)
	want := []Candidate{
		{Name: "v2rayN", Hosts: []string{"127.0.0.1"}, Port: 2023, Source: SourceProgram},
		// Listening on every IPv6 address may take IPv4 connections too.
		{Name: "v2rayN", Hosts: []string{"127.0.0.1", "::1"}, Port: 10808, Source: SourceProgram},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Candidates =\n%+v\nwant\n%+v", got, want)
	}
}

func TestCandidatesNamesAndAddresses(t *testing.T) {
	f := Facts{
		Processes: []Process{
			{PID: 300, ParentPID: 50, Exe: "Clash Verge.exe", Created: at(1)},
			{PID: 301, ParentPID: 300, Exe: "verge-mihomo.exe", Created: at(2)},
			{PID: 400, ParentPID: 50, Exe: "XRAY.EXE", Created: at(3)}, // run on its own; names in any case
			{PID: 500, ParentPID: 50, Exe: "nginx.exe", Created: at(3)},
			{PID: 600, ParentPID: 50, Exe: "sing-box.exe", Created: at(3)},
			// A parent's ID may have been reused, even by a process it started.
			{PID: 700, ParentPID: 701, Exe: "hysteria-windows-amd64.exe", Created: at(4)},
			{PID: 701, ParentPID: 700, Exe: "launcher.exe", Created: at(5)},
		},
		Listeners: []Listener{
			listen("127.0.0.1:7897", 301),
			listen("[::1]:7897", 301),
			listen("127.0.0.1:9097", 301),
			listen("0.0.0.0:10808", 400),
			listen("127.0.0.1:8080", 500),  // not a proxy program
			listen("192.0.2.10:2080", 600), // not reached through loopback
			listen("127.0.0.1:1080", 999),  // its process is gone
			listen("[::ffff:127.0.0.2]:1081", 700),
		},
	}
	want := []Candidate{
		{Name: "Clash Verge", Hosts: []string{"127.0.0.1", "::1"}, Port: 7897, Source: SourceProgram},
		{Name: "Clash Verge", Hosts: []string{"127.0.0.1"}, Port: 9097, Source: SourceProgram},
		{Name: "hysteria-windows-amd64", Hosts: []string{"127.0.0.2"}, Port: 1081, Source: SourceProgram},
		{Name: "XRAY", Hosts: []string{"127.0.0.1"}, Port: 10808, Source: SourceProgram},
	}
	if got := Candidates(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("Candidates =\n%+v\nwant\n%+v", got, want)
	}
}

// explorer.exe outlives the userinit.exe that started it; a program started
// at sign-in may get that ID. A core started from File Explorer later is not
// that program's.
func TestCandidatesOnlyFollowParentsThatCameFirst(t *testing.T) {
	f := Facts{
		Processes: []Process{
			{PID: 50, ParentPID: 9, Exe: "explorer.exe", Created: at(0)},
			{PID: 9, ParentPID: 1, Exe: "v2rayN.exe", Created: at(1)}, // took userinit.exe's ID
			{PID: 600, ParentPID: 50, Exe: "sing-box.exe", Created: at(30)},
			// Without start times the chain stops too.
			{PID: 800, ParentPID: 801, Exe: "xray.exe"},
			{PID: 801, ParentPID: 50, Exe: "v2rayN.exe"},
		},
		Listeners: []Listener{listen("127.0.0.1:2080", 600), listen("127.0.0.1:10808", 800)},
	}
	want := []Candidate{
		{Name: "sing-box", Hosts: []string{"127.0.0.1"}, Port: 2080, Source: SourceProgram},
		{Name: "xray", Hosts: []string{"127.0.0.1"}, Port: 10808, Source: SourceProgram},
	}
	if got := Candidates(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("Candidates =\n%+v\nwant\n%+v", got, want)
	}
}

func TestCandidatesFromTheSystemProxy(t *testing.T) {
	base := Facts{
		Processes: []Process{
			{PID: 50, ParentPID: 40, Exe: "explorer.exe"},
			{PID: 800, ParentPID: 50, Exe: "Fiddler.exe"},
			{PID: 200, ParentPID: 50, Exe: "xray.exe"},
		},
		Listeners: []Listener{
			listen("127.0.0.1:8888", 800),
			listen("[::]:7890", 800),
			listen("127.0.0.1:10809", 200),
		},
	}
	fiddler := func(host string, port int, kind string) []Candidate {
		return []Candidate{{Name: "Fiddler", Hosts: []string{host}, Port: port, Source: SourceSystem, Kind: kind}}
	}
	const http, socks = model.KindHTTP, model.KindSocks
	xray := Candidate{Name: "xray", Hosts: []string{"127.0.0.1"}, Port: 10809, Source: SourceProgram}
	for _, tc := range []struct {
		setting string
		want    []Candidate // after xray's own port
	}{
		{"", nil},
		{"127.0.0.1:8888", fiddler("127.0.0.1", 8888, http)},
		{"localhost:8888", fiddler("127.0.0.1", 8888, http)},
		{"http://127.0.0.1:8888/", fiddler("127.0.0.1", 8888, http)},
		// The way a tunnel goes, as following the system proxy takes it
		// (sysproxy.Pick): the https entry first.
		{"http=127.0.0.1:8888;https=127.0.0.1:7890", fiddler("127.0.0.1", 7890, http)},
		{"https=127.0.0.1:7890 ftp=127.0.0.1:21", fiddler("127.0.0.1", 7890, http)},
		{"[::1]:7890", fiddler("::1", 7890, http)},
		{"socks=127.0.0.1:8888", fiddler("127.0.0.1", 8888, socks)},
		{"ftp=127.0.0.1:8888", nil},
		{"proxy.example.com:8080", nil},
		{"192.0.2.1:8888", nil},
		{"127.0.0.1", nil},
		{"127.0.0.1:0", nil},
		// Nothing listens there: whatever set it is not running.
		{"127.0.0.1:3128", nil},
		// A known proxy program's port is a candidate of its own already.
		{"127.0.0.1:10809", nil},
	} {
		f := base
		f.SystemProxy = tc.setting
		want := append([]Candidate{xray}, tc.want...)
		if got := Candidates(f); !reflect.DeepEqual(got, want) {
			t.Errorf("SystemProxy %q: Candidates =\n%+v\nwant\n%+v", tc.setting, got, want)
		}
	}
}

func TestCandidatesOfNothing(t *testing.T) {
	if got := Candidates(Facts{}); len(got) != 0 {
		t.Fatalf("Candidates(nothing) = %+v", got)
	}
}
