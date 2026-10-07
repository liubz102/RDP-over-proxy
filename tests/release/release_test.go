package release_test

import (
	. "github.com/liubz102/RDP-over-proxy/tools/release"

	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The repository's own files agree on one version, whatever it is: a change
// of version has to reach every place (the owner changes it, everywhere).
func TestRepositoryAgrees(t *testing.T) {
	places, err := Places(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 10 {
		t.Errorf("found the version in %d places, want 10: %+v", len(places), places)
	}
	if _, err := Version(places); err != nil {
		t.Error(err)
	}
}

// files is a repository with every place that carries the version.
func files(t *testing.T, version, manifest string) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, text string) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v := version
	write("build/config.yml", "version: '3'\n\ninfo:\n  productName: \"Example\"\n  version: \""+v+"\" # decided by the owner\n")
	write("build/windows/info.json", `{"fixed": {"file_version": "`+v+`", "product_version": "`+v+`"}, "info": {"0409": {"ProductVersion": "`+v+`", "FileVersion": "`+v+`"}}}`)
	write("build/windows/wails.exe.manifest", `<assembly><assemblyIdentity type="win32" name="io.github.liubz102.rdp-over-proxy" version="`+manifest+`" processorArchitecture="*"/>
<dependency><assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0"/></dependency></assembly>`)
	write("frontend/package.json", `{"name": "example", "version": "`+v+`"}`)
	write("frontend/package-lock.json", `{"name": "example", "version": "`+v+`", "packages": {"": {"name": "example", "version": "`+v+`"}}}`)
	write("main.go", "package main\n\nvar version = \""+v+"\"\n")
	return root
}

func TestCheck(t *testing.T) {
	places, err := Places(files(t, "1.2.3", "1.2.3.0"))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := Version(places); v != "1.2.3" || err != nil {
		t.Fatalf("Version = %q, %v (a manifest's fourth part of 0 is the same version)", v, err)
	}
	cases := []struct {
		tag, version string
		prerelease   bool
		problem      string
	}{
		{"v1.2.3", "1.2.3", false, ""},
		{"v1.2.3-beta.1", "1.2.3-beta.1", true, ""},
		{"v1.2.4", "", false, "not the version the files carry, 1.2.3"},
		{"1.2.3", "", false, "is not v<major>.<minor>.<patch>"},
		{"v1.2", "", false, "is not v<major>.<minor>.<patch>"},
		{"v1.2.3-", "", false, "is not v<major>.<minor>.<patch>"},
	}
	for _, c := range cases {
		version, prerelease, err := Check(c.tag, places)
		switch {
		case c.problem == "" && (err != nil || version != c.version || prerelease != c.prerelease):
			t.Errorf("Check(%q) = %q, %v, %v; want %q, %v", c.tag, version, prerelease, err, c.version, c.prerelease)
		case c.problem != "" && (err == nil || !strings.Contains(err.Error(), c.problem)):
			t.Errorf("Check(%q) = %v, want an error saying %q", c.tag, err, c.problem)
		}
	}
}

// A version changed in one place only is caught, and the error says where
// each version is written.
func TestFilesDisagree(t *testing.T) {
	root := files(t, "1.2.3", "1.2.3")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nvar version = \"1.3.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	places, err := Places(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Check("v1.3.0", places)
	if err == nil || !strings.Contains(err.Error(), "1.3.0: main.go (version)") || !strings.Contains(err.Error(), "1.2.3: build/config.yml (info.version)") {
		t.Fatalf("Check = %v, want the places of each version", err)
	}
}

func TestPlaceMissing(t *testing.T) {
	root := files(t, "1.2.3", "1.2.3")
	if err := os.WriteFile(filepath.Join(root, "frontend", "package.json"), []byte(`{"name": "example"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Places(root); err == nil || !strings.Contains(err.Error(), "frontend/package.json: no version") {
		t.Fatalf("Places = %v, want it to say the version is missing", err)
	}
}

// Pack writes the zip (the folder with the exe, the license and the notices),
// the notices on their own and their checksums.
func TestPack(t *testing.T) {
	src := t.TempDir()
	files := map[string]string{"app.exe": "MZ the program", "LICENSE": "GNU GENERAL PUBLIC LICENSE", "notices.txt": "Third-party software"}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "0.2.0-rc.1")
	written, err := Pack(Files{
		Version: "0.2.0-rc.1",
		Exe:     filepath.Join(src, "app.exe"),
		License: filepath.Join(src, "LICENSE"),
		Notices: filepath.Join(src, "notices.txt"),
	}, out)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range written {
		names = append(names, filepath.Base(p))
	}
	if strings.Join(names, " ") != "RDP-over-proxy-0.2.0-rc.1-windows-amd64.zip THIRD_PARTY_NOTICES.txt SHA256SUMS.txt" {
		t.Fatalf("Pack wrote %q", names)
	}

	zr, err := zip.OpenReader(filepath.Join(out, "RDP-over-proxy-0.2.0-rc.1-windows-amd64.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := map[string]string{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		r.Close()
		got[f.Name] = string(data)
	}
	want := map[string]string{
		"RDP-over-proxy/RDP-over-proxy.exe":      "MZ the program",
		"RDP-over-proxy/LICENSE.txt":             "GNU GENERAL PUBLIC LICENSE",
		"RDP-over-proxy/THIRD_PARTY_NOTICES.txt": "Third-party software",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the zip holds %q", got)
	}

	sums, err := os.ReadFile(filepath.Join(out, "SHA256SUMS.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"RDP-over-proxy-0.2.0-rc.1-windows-amd64.zip", "THIRD_PARTY_NOTICES.txt"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if line := hex.EncodeToString(sum[:]) + "  " + name + "\n"; !strings.Contains(string(sums), line) {
			t.Errorf("SHA256SUMS.txt lacks %q:\n%s", line, sums)
		}
	}
}
