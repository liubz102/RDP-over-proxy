package release

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Files is what Pack puts into a release.
type Files struct {
	// Version names the zip ("0.1.0", "0.2.0-rc.1").
	Version string
	// Exe is the built program; License and Notices go next to it.
	Exe, License, Notices string
}

// Folder is the folder the zip holds. The program keeps its data next to
// itself, so the user puts this folder where they can write.
const Folder = "RDP-over-proxy"

// ZipName is the name of a version's zip.
func ZipName(version string) string {
	return "RDP-over-proxy-" + version + "-windows-amd64.zip"
}

// Pack writes a release into dir: the zip (Folder with the exe,
// LICENSE.txt and THIRD_PARTY_NOTICES.txt), the notices on their own, and
// SHA256SUMS.txt for both, in the format sha256sum -c reads. It returns the
// paths it wrote. Every entry of the zip carries the exe's time, so the same
// files give the same zip.
func Pack(f Files, dir string) ([]string, error) {
	if f.Version == "" {
		return nil, fmt.Errorf("no version")
	}
	exe, err := os.Stat(f.Exe)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	zipPath := filepath.Join(dir, ZipName(f.Version))
	if err := writeZip(zipPath, exe.ModTime(), []entry{
		{f.Exe, "RDP-over-proxy.exe"},
		{f.License, "LICENSE.txt"},
		{f.Notices, "THIRD_PARTY_NOTICES.txt"},
	}); err != nil {
		return nil, err
	}
	notices := filepath.Join(dir, "THIRD_PARTY_NOTICES.txt")
	if err := copyFile(f.Notices, notices); err != nil {
		return nil, err
	}

	var sums strings.Builder
	files := []string{zipPath, notices}
	sort.Slice(files, func(i, j int) bool { return filepath.Base(files[i]) < filepath.Base(files[j]) })
	for _, p := range files {
		sum, err := sha256File(p)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&sums, "%s  %s\n", sum, filepath.Base(p))
	}
	sumsPath := filepath.Join(dir, "SHA256SUMS.txt")
	if err := os.WriteFile(sumsPath, []byte(sums.String()), 0o644); err != nil {
		return nil, err
	}
	return append(files, sumsPath), nil
}

type entry struct{ from, name string }

// writeZip writes entries into Folder in a new zip at path.
func writeZip(path string, modified time.Time, entries []entry) (err error) {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	zw := zip.NewWriter(out)
	for _, e := range entries {
		in, err := os.Open(e.from)
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: Folder + "/" + e.name, Method: zip.Deflate, Modified: modified})
		if err == nil {
			_, err = io.Copy(w, in)
		}
		in.Close()
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o644)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
