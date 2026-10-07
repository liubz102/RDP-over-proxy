package release_test

import (
	. "github.com/liubz102/RDP-over-proxy/tools/release"

	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const repo = "../.."

// The version is written in build/config.yml and nowhere else: the files
// that used to repeat it carry none, so changing it is a one-line edit.
func TestOnePlace(t *testing.T) {
	v, err := Version(repo)
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, name := range []string{"build/windows/info.json", "build/windows/wails.exe.manifest", "frontend/package.json", "main.go"} {
		if strings.Contains(read(name), `"`+v+`"`) || strings.Contains(read(name), `"`+v+`.0"`) {
			t.Errorf("%s carries the version %s; build/config.yml should be the only place", name, v)
		}
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(read("frontend/package.json")), &pkg); err != nil {
		t.Fatal(err)
	}
	if _, ok := pkg["version"]; ok {
		t.Error("frontend/package.json has a version")
	}
	var lock struct {
		Version  *string
		Packages map[string]struct{ Version *string }
	}
	if err := json.Unmarshal([]byte(read("frontend/package-lock.json")), &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Version != nil || lock.Packages[""].Version != nil {
		t.Error("frontend/package-lock.json has the package's version")
	}
}

// config is a repository with build/config.yml saying version.
func config(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "build", "config.yml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "version: '3'\n\ninfo:\n  productName: \"Example\"\n  version: \"" + version + "\" # decided by the owner\n"
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestVersion(t *testing.T) {
	if v, err := Version(config(t, "1.2.3")); v != "1.2.3" || err != nil {
		t.Errorf("Version = %q, %v", v, err)
	}
	// Windows' file version holds numbers only; a pre-release goes on the tag.
	for _, bad := range []string{"1.2", "1.2.3-beta.1", "v1.2.3", ""} {
		if _, err := Version(config(t, bad)); err == nil {
			t.Errorf("Version accepted %q", bad)
		}
	}
	if _, err := Version(t.TempDir()); err == nil {
		t.Error("Version without build/config.yml")
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		tag, version string
		prerelease   bool
		problem      string
	}{
		{"v1.2.3", "1.2.3", false, ""},
		{"v1.2.3-beta.1", "1.2.3-beta.1", true, ""},
		{"v1.2.4", "", false, "is not the version in build/config.yml, 1.2.3"},
		{"1.2.3", "", false, "is not v<major>.<minor>.<patch>"},
		{"v1.2", "", false, "is not v<major>.<minor>.<patch>"},
		{"v1.2.3-", "", false, "is not v<major>.<minor>.<patch>"},
	}
	for _, c := range cases {
		version, prerelease, err := Check(c.tag, "1.2.3")
		switch {
		case c.problem == "" && (err != nil || version != c.version || prerelease != c.prerelease):
			t.Errorf("Check(%q) = %q, %v, %v; want %q, %v", c.tag, version, prerelease, err, c.version, c.prerelease)
		case c.problem != "" && (err == nil || !strings.Contains(err.Error(), c.problem)):
			t.Errorf("Check(%q) = %v, want an error saying %q", c.tag, err, c.problem)
		}
	}
}

// Stamp gives the repository's own info.json and manifest the version: the
// fixed file and product versions, every string table's, and the app's
// assembly (not the Common Controls one it depends on).
func TestStamp(t *testing.T) {
	out := t.TempDir()
	if err := Stamp(repo, out, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "info.json"))
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Fixed struct{ File_version, Product_version string }
		Info  map[string]map[string]string
	}
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	if info.Fixed.File_version != "1.2.3" || info.Fixed.Product_version != "1.2.3" || len(info.Info) == 0 {
		t.Errorf("stamped info.json: %s", data)
	}
	for lang, table := range info.Info {
		if table["FileVersion"] != "1.2.3" || table["ProductVersion"] != "1.2.3" || table["ProductName"] != "RDP over Proxy" {
			t.Errorf("string table %s: %v", lang, table)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(out, "wails.exe.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`<assemblyIdentity[^>]*>`).FindAllString(string(manifest), -1)
	if len(ids) != 2 || !strings.Contains(ids[0], `name="io.github.liubz102.rdp-over-proxy" version="1.2.3.0"`) || !strings.Contains(ids[1], `version="6.0.0.0"`) {
		t.Errorf("stamped manifest identities: %q", ids)
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
