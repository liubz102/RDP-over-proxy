package notices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// GoPackage is what `go list -json` says about a package: the fields the
// notices use.
type GoPackage struct {
	ImportPath string
	Dir        string
	Standard   bool
	Module     *GoModule
}

// GoModule is a package's module as `go list -json` reports it.
type GoModule struct {
	Path    string
	Version string
	Dir     string
	Main    bool
	Replace *GoModule
}

// ReadGoList reads the stream of JSON objects `go list -json` writes.
func ReadGoList(r io.Reader) ([]GoPackage, error) {
	dec := json.NewDecoder(r)
	var pkgs []GoPackage
	for {
		var p GoPackage
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return pkgs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read go list: %w", err)
		}
		pkgs = append(pkgs, p)
	}
}

// GoModules returns the modules of pkgs as components, but the main module
// and the standard library (see GoToolchain). A module's files are the
// license and notice files at its root and those in the folders of its
// packages in pkgs and the folders between them and the root: code a module
// took from elsewhere keeps its own license there. Folders of packages the
// build does not use are left out.
func GoModules(pkgs []GoPackage) ([]Component, error) {
	type module struct {
		path, version, dir string
		dirs               map[string]bool
	}
	mods := map[string]*module{}
	for _, p := range pkgs {
		if p.Standard || p.Module == nil || p.Module.Main {
			continue
		}
		m := p.Module
		if m.Replace != nil {
			m = m.Replace
		}
		if m.Dir == "" {
			return nil, fmt.Errorf("%s %s: not in the module cache (go mod download)", m.Path, m.Version)
		}
		key := m.Path + " " + m.Version // sorts a path before longer ones it begins
		if mods[key] == nil {
			mods[key] = &module{path: m.Path, version: m.Version, dir: m.Dir, dirs: map[string]bool{}}
		}
		mods[key].dirs[p.Dir] = true
	}

	var out []Component
	for _, key := range sortedKeys(mods) {
		m := mods[key]
		dirs := map[string]bool{m.dir: true}
		for d := range m.dirs {
			for _, up := range between(m.dir, d) {
				dirs[up] = true
			}
		}
		var files []File
		for _, d := range sortedKeys(dirs) {
			fs, err := filesIn(m.dir, d)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", m.path, m.version, err)
			}
			files = append(files, fs...)
		}
		c := Component{Name: m.path, Version: m.version, Files: files}
		if m.version != "" {
			c.Source = "https://proxy.golang.org/" + escapePath(m.path) + "/@v/" + escapePath(m.version) + ".zip"
		}
		out = append(out, c)
	}
	return out, nil
}

// between returns dir and the folders above it up to root, root excluded;
// nothing when dir is not inside root.
func between(root, dir string) []string {
	var out []string
	for {
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return out
		}
		out = append(out, dir)
		up := filepath.Dir(dir)
		if up == dir {
			return out
		}
		dir = up
	}
}

// escapePath writes a module path or version the way the module proxy
// expects: capital letters as "!" and the lower-case letter.
func escapePath(s string) string {
	var b strings.Builder
	for _, r := range s {
		if 'A' <= r && r <= 'Z' {
			b.WriteByte('!')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// GoToolchain is Go itself, whose runtime and standard library are part of
// every Go program: the license and patent grant in goroot. version is what
// `go env GOVERSION` says ("go1.27.0").
func GoToolchain(goroot, version string) (Component, error) {
	files, err := filesIn(goroot, goroot)
	if err != nil {
		return Component{}, fmt.Errorf("Go: %w", err)
	}
	if f := strings.Fields(version); len(f) > 0 {
		version = f[0]
	}
	return Component{
		Name:    "Go",
		Version: strings.TrimPrefix(version, "go"),
		Source:  "https://go.dev/dl/" + version + ".src.tar.gz",
		Files:   files,
	}, nil
}

// NpmPackages returns the packages in dirs as components. dirs are the
// packages' folders relative to root, with forward slashes, as the
// frontend's bundler reports them ("node_modules/react"). A package's files
// are the license and notice files in its folder; its name, version,
// declared license and author come from its package.json.
func NpmPackages(root string, dirs []string) ([]Component, error) {
	var out []Component
	for _, dir := range dirs {
		abs := filepath.Join(root, filepath.FromSlash(dir))
		data, err := os.ReadFile(filepath.Join(abs, "package.json"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		var pj packageJSON
		if err := json.Unmarshal(data, &pj); err != nil {
			return nil, fmt.Errorf("%s/package.json: %w", dir, err)
		}
		if pj.Name == "" || pj.Version == "" {
			return nil, fmt.Errorf("%s/package.json: no name or version", dir)
		}
		files, err := filesIn(abs, abs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		out = append(out, Component{
			Name:     pj.Name,
			Version:  pj.Version,
			Source:   "https://registry.npmjs.org/" + pj.Name + "/-/" + path.Base(pj.Name) + "-" + pj.Version + ".tgz",
			Files:    files,
			Declared: pj.License.Type,
			Author:   pj.Author.Name,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type packageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License named  `json:"license"`
	Author  person `json:"author"`
}

// named is package.json's "license": "MIT", or the old {"type": "MIT"}.
type named struct{ Type string }

func (n *named) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		var v struct{ Type string }
		err := json.Unmarshal(b, &v)
		n.Type = v.Type
		return err
	}
	return json.Unmarshal(b, &n.Type)
}

// person is package.json's "author": "Name <mail> (url)" or {"name": …}.
type person struct{ Name string }

func (p *person) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		var v struct{ Name string }
		err := json.Unmarshal(b, &v)
		p.Name = v.Name
		return err
	}
	return json.Unmarshal(b, &p.Name)
}

// filesIn reads the license and notice files directly in dir, with paths
// relative to root.
func filesIn(root, dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, e := range entries {
		kind, ok := FileKind(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: filepath.ToSlash(rel), Kind: kind, Text: text(data)})
	}
	return files, nil
}

// text is a file's content as text: without a UTF-8 byte order mark, with
// LF line ends and anything that is not UTF-8 replaced.
func text(data []byte) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, string(utf8.RuneError))
	}
	return s
}
