// Package release checks a release tag against the version the project's
// files carry. The version is the owner's to decide; it is written in several
// files (build/config.yml and the ones Wails generated from it, the
// frontend's package.json, main.go), and a release goes ahead only when they
// all say the same and the tag says it too. Nothing here changes a version.
package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A Place is where a file carries the version, with what it says there.
type Place struct {
	// File is relative to the repository, with forward slashes.
	File string
	// Where says which value in the file it is ("info.version").
	Where   string
	Version string
}

// Places reads the version from every place in the repository at root that
// carries it.
func Places(root string) ([]Place, error) {
	var places []Place
	var problems []string
	read := func(file string) []byte {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			problems = append(problems, err.Error())
		}
		return data
	}
	match := func(file, where string, re *regexp.Regexp) {
		data := read(file)
		if data == nil {
			return
		}
		m := re.FindSubmatch(data)
		if m == nil {
			problems = append(problems, fmt.Sprintf("%s: no %s", file, where))
			return
		}
		places = append(places, Place{file, where, string(m[1])})
	}
	fromJSON := func(file string, paths ...string) {
		data := read(file)
		if data == nil {
			return
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", file, err))
			return
		}
		for _, p := range paths {
			keys := strings.Split(p, "|")
			where := make([]string, len(keys))
			for i, k := range keys {
				where[i] = k
				if k == "" {
					where[i] = `""`
				}
			}
			v, ok := lookup(doc, keys)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: no %s", file, strings.Join(where, ".")))
				continue
			}
			places = append(places, Place{file, strings.Join(where, "."), v})
		}
	}

	match("build/config.yml", "info.version", configVersion)
	fromJSON("build/windows/info.json", "fixed|file_version", "fixed|product_version", "info|0409|FileVersion", "info|0409|ProductVersion")
	match("build/windows/wails.exe.manifest", "assemblyIdentity version", manifestVersion)
	fromJSON("frontend/package.json", "version")
	// The lock file repeats the package's own version, under the key "".
	fromJSON("frontend/package-lock.json", "version", "packages||version")
	match("main.go", "version", mainVersion)
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "\n"))
	}
	return places, nil
}

var (
	// The version under info:, indented; the file's own `version: '3'` is not.
	configVersion   = regexp.MustCompile(`(?m)^[ \t]+version:[ \t]*"([^"]+)"`)
	manifestVersion = regexp.MustCompile(`<assemblyIdentity[^>]*name="io\.github\.liubz102\.rdp-over-proxy"[^>]*version="([^"]+)"`)
	mainVersion     = regexp.MustCompile(`(?m)^var version = "([^"]+)"`)
	tagPattern      = regexp.MustCompile(`^v(\d+\.\d+\.\d+)(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
)

// lookup finds the string at path in a decoded JSON document.
func lookup(doc any, path []string) (string, bool) {
	for _, key := range path {
		m, ok := doc.(map[string]any)
		if !ok {
			return "", false
		}
		if doc, ok = m[key]; !ok {
			return "", false
		}
	}
	s, ok := doc.(string)
	return s, ok
}

// Version is what every place says, when they all agree. A manifest may
// write it with a fourth part of 0 ("1.2.3.0").
func Version(places []Place) (string, error) {
	seen := map[string][]string{}
	var order []string
	for _, p := range places {
		v := p.Version
		if parts := strings.Split(v, "."); len(parts) == 4 && parts[3] == "0" {
			v = strings.Join(parts[:3], ".")
		}
		if seen[v] == nil {
			order = append(order, v)
		}
		seen[v] = append(seen[v], p.File+" ("+p.Where+")")
	}
	switch len(order) {
	case 0:
		return "", errors.New("no version found")
	case 1:
		return order[0], nil
	}
	var b strings.Builder
	b.WriteString("the files do not agree on the version:")
	for _, v := range order {
		fmt.Fprintf(&b, "\n  %s: %s", v, strings.Join(seen[v], ", "))
	}
	return "", errors.New(b.String())
}

// Check compares a release tag with the version the places agree on. The
// tag is that version with a "v" in front ("v1.2.3"), and may add a
// pre-release ("v1.2.3-beta.1"). It returns what the program is to call
// itself (the tag without the "v") and whether it is a pre-release.
func Check(tag string, places []Place) (version string, prerelease bool, err error) {
	files, err := Version(places)
	if err != nil {
		return "", false, err
	}
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return "", false, fmt.Errorf("tag %q is not v<major>.<minor>.<patch>, optionally with -<pre-release>", tag)
	}
	if m[1] != files {
		return "", false, fmt.Errorf("tag %s is not the version the files carry, %s", tag, files)
	}
	return strings.TrimPrefix(tag, "v"), m[2] != "", nil
}
