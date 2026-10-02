//go:build windows

package store

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAFileThatCannotBeReadIsLeftOut(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "proxies/aaaa.json", `{"schema":1,"name":"Held","kind":"http","server":"192.0.2.1","port":8080}`)
	write(t, dir, "proxies/bbbb.json", `{"schema":1,"name":"Fine","kind":"http","server":"192.0.2.2","port":8080}`)

	// Another program holds aaaa.json open without sharing, as a backup or
	// antivirus scanner might.
	path, err := windows.UTF16PtrFromString(filepath.Join(dir, "proxies", "aaaa.json"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(path, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)

	d, problems := open(t, dir)
	if got := problemCodes(problems); !mapsEqual(got, map[string]string{"proxies/aaaa.json": ProblemReadFailed}) {
		t.Fatalf("problems %v", got)
	}
	if _, ok := d.Proxy("bbbb"); !ok {
		t.Fatal("the readable proxy did not load")
	}
	if _, ok := d.Proxy("aaaa"); ok {
		t.Fatal("the held file loaded")
	}
}
