package notices_test

import (
	. "github.com/liubz102/RDP-over-proxy/tools/notices"

	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// write creates files under dir, by slash-separated path.
func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// goList is what `go list -deps -json` writes for pkgs.
func goList(t *testing.T, pkgs ...GoPackage) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	for _, p := range pkgs {
		data, err := json.MarshalIndent(p, "", "\t")
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteString("\n")
	}
	return &b
}

func paths(files []File) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path+" "+f.Kind)
	}
	return out
}

// A module's files are those at its root and in the folders of the packages
// the build uses, up to the root; a folder of packages it does not use is
// left out, and so are source files named after licenses.
func TestGoModules(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "example.com", "lib@v1.2.0")
	write(t, mod, map[string]string{
		"LICENSE":             mit,
		"PATENTS":             "Additional IP Rights Grant (Patents)",
		"license.go":          "package lib",
		"sub/LICENSE.txt":     bsd2,
		"sub/pkg/LICENSE":     bsd3,
		"sub/pkg/code.go":     "package pkg",
		"unused/LICENSE":      gplNotice,
		"unused/code/code.go": "package code",
	})
	upper := filepath.Join(root, "github.com", "!example", "!tool@v0.1.0")
	write(t, upper, map[string]string{"LICENSE": mit})
	fork := filepath.Join(root, "example.com", "fork@v2.0.0")
	write(t, fork, map[string]string{"COPYING": bsd3})

	list := goList(t,
		GoPackage{ImportPath: "example.com/app", Dir: filepath.Join(root, "app"), Module: &GoModule{Path: "example.com/app", Main: true}},
		GoPackage{ImportPath: "fmt", Dir: filepath.Join(root, "goroot", "src", "fmt"), Standard: true},
		GoPackage{ImportPath: "example.com/lib/sub/pkg", Dir: filepath.Join(mod, "sub", "pkg"),
			Module: &GoModule{Path: "example.com/lib", Version: "v1.2.0", Dir: mod}},
		GoPackage{ImportPath: "example.com/lib", Dir: mod,
			Module: &GoModule{Path: "example.com/lib", Version: "v1.2.0", Dir: mod}},
		GoPackage{ImportPath: "github.com/Example/Tool", Dir: upper,
			Module: &GoModule{Path: "github.com/Example/Tool", Version: "v0.1.0", Dir: upper}},
		GoPackage{ImportPath: "example.com/original", Dir: fork,
			Module: &GoModule{Path: "example.com/original", Version: "v1.0.0",
				Replace: &GoModule{Path: "example.com/fork", Version: "v2.0.0", Dir: fork}}},
	)
	pkgs, err := ReadGoList(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 6 {
		t.Fatalf("ReadGoList read %d packages, want 6", len(pkgs))
	}
	got, err := GoModules(pkgs)
	if err != nil {
		t.Fatal(err)
	}
	type summary struct {
		Name, Version, Source string
		Files                 []string
	}
	var sums []summary
	for _, c := range got {
		sums = append(sums, summary{c.Name, c.Version, c.Source, paths(c.Files)})
	}
	want := []summary{
		{"example.com/fork", "v2.0.0", "https://proxy.golang.org/example.com/fork/@v/v2.0.0.zip", []string{"COPYING license"}},
		{"example.com/lib", "v1.2.0", "https://proxy.golang.org/example.com/lib/@v/v1.2.0.zip",
			[]string{"LICENSE license", "PATENTS notice", "sub/LICENSE.txt license", "sub/pkg/LICENSE license"}},
		{"github.com/Example/Tool", "v0.1.0", "https://proxy.golang.org/github.com/!example/!tool/@v/v0.1.0.zip", []string{"LICENSE license"}},
	}
	if !reflect.DeepEqual(sums, want) {
		t.Errorf("GoModules =\n%+v\nwant\n%+v", sums, want)
	}
	if got[1].Files[0].Text != mit {
		t.Errorf("the text of LICENSE is %q", got[1].Files[0].Text)
	}
}

// A module that is not in the module cache has no folder to read.
func TestGoModulesNotDownloaded(t *testing.T) {
	_, err := GoModules([]GoPackage{{ImportPath: "example.com/lib", Module: &GoModule{Path: "example.com/lib", Version: "v1.0.0"}}})
	if err == nil || !strings.Contains(err.Error(), "go mod download") {
		t.Fatalf("GoModules = %v, want an error that says to download it", err)
	}
}

// A module path sorts before longer paths it begins with.
func TestGoModulesOrder(t *testing.T) {
	root := t.TempDir()
	var pkgs []GoPackage
	for _, path := range []string{"example.com/sing-shadowsocks", "example.com/sing"} {
		dir := filepath.Join(root, filepath.FromSlash(path))
		write(t, dir, map[string]string{"LICENSE": mit})
		pkgs = append(pkgs, GoPackage{ImportPath: path, Dir: dir, Module: &GoModule{Path: path, Version: "v1.0.0", Dir: dir}})
	}
	got, err := GoModules(pkgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "example.com/sing" {
		t.Fatalf("GoModules order: %v", got)
	}
}

func TestGoToolchain(t *testing.T) {
	goroot := t.TempDir()
	write(t, goroot, map[string]string{
		"LICENSE":   bsd3,
		"PATENTS":   "Additional IP Rights Grant (Patents)",
		"README.md": "# The Go Programming Language",
		"VERSION":   "go1.27.0",
	})
	c, err := GoToolchain(goroot, "go1.27.0 X:nocoverageredesign")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Go" || c.Version != "1.27.0" || c.Source != "https://go.dev/dl/go1.27.0.src.tar.gz" {
		t.Errorf("GoToolchain = %s %s %s", c.Name, c.Version, c.Source)
	}
	if got := paths(c.Files); !reflect.DeepEqual(got, []string{"LICENSE license", "PATENTS notice"}) {
		t.Errorf("Go's files: %q", got)
	}
}

// An npm package is read from its package.json and its folder: scoped and
// nested packages, the old {"type": …} license, an author given as an
// object, and a package with no license file at all.
func TestNpmPackages(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"node_modules/react/package.json":                    `{"name": "react", "version": "19.3.0", "license": "MIT"}`,
		"node_modules/react/LICENSE":                         mit,
		"node_modules/react/index.js":                        "module.exports = {}",
		"node_modules/@scope/runtime/package.json":           `{"name": "@scope/runtime", "version": "3.0.0-beta.1", "license": "MIT", "author": {"name": "The Example Team", "email": "team@example.com"}}`,
		"node_modules/old/package.json":                      `{"name": "old", "version": "1.0.0", "license": {"type": "ISC"}, "author": "Someone <someone@example.com>"}`,
		"node_modules/old/LICENSE.md":                        "ISC License\n\nPermission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted, provided that the above copyright notice and this permission notice appear in all copies.",
		"node_modules/a/node_modules/b/package.json":         `{"name": "b", "version": "2.0.0", "license": "MIT"}`,
		"node_modules/a/node_modules/b/LICENSE":              mit,
		"node_modules/a/node_modules/b/NOTICE":               "Notice of b",
		"node_modules/a/node_modules/b/lib/license.js":       "export default 1",
		"node_modules/a/node_modules/b/node_modules/.keep":   "",
		"node_modules/@scope/runtime/dist/runtime.js":        "export {}",
		"node_modules/@scope/runtime/plugins/LICENSE-NOTICE": "only the folder's own files count",
	})
	got, err := NpmPackages(root, []string{"node_modules/react", "node_modules/@scope/runtime", "node_modules/old", "node_modules/a/node_modules/b"})
	if err != nil {
		t.Fatal(err)
	}
	type summary struct {
		Name, Version, Source, Declared, Author string
		Files                                   []string
	}
	var sums []summary
	for _, c := range got {
		sums = append(sums, summary{c.Name, c.Version, c.Source, c.Declared, c.Author, paths(c.Files)})
	}
	want := []summary{
		{"@scope/runtime", "3.0.0-beta.1", "https://registry.npmjs.org/@scope/runtime/-/runtime-3.0.0-beta.1.tgz", "MIT", "The Example Team", nil},
		{"b", "2.0.0", "https://registry.npmjs.org/b/-/b-2.0.0.tgz", "MIT", "", []string{"LICENSE license", "NOTICE notice"}},
		{"old", "1.0.0", "https://registry.npmjs.org/old/-/old-1.0.0.tgz", "ISC", "Someone <someone@example.com>", []string{"LICENSE.md license"}},
		{"react", "19.3.0", "https://registry.npmjs.org/react/-/react-19.3.0.tgz", "MIT", "", []string{"LICENSE license"}},
	}
	if !reflect.DeepEqual(sums, want) {
		t.Errorf("NpmPackages =\n%+v\nwant\n%+v", sums, want)
	}

	if _, err := NpmPackages(root, []string{"node_modules/missing"}); err == nil {
		t.Error("a folder without package.json was read")
	}
}

// File text loses a UTF-8 byte order mark and CRLF line ends.
func TestFileText(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"node_modules/x/package.json": `{"name": "x", "version": "1.0.0"}`,
		"node_modules/x/LICENSE":      string([]byte{0xEF, 0xBB, 0xBF}) + "MIT License\r\n\r\nPermission is hereby granted\r\n",
	})
	got, err := NpmPackages(root, []string{"node_modules/x"})
	if err != nil {
		t.Fatal(err)
	}
	if text := got[0].Files[0].Text; text != "MIT License\n\nPermission is hereby granted\n" {
		t.Errorf("text = %q", text)
	}
}

var policy = Policy{
	Allowed:  []string{"Apache-2.0", "BSD-3-Clause", "MIT"},
	Reviewed: map[string][]string{"example.com/gpl": {"GPL-3.0-or-later"}},
}

func lic(text string) []File { return []File{{Path: "LICENSE", Kind: KindLicense, Text: text}} }

func TestBuild(t *testing.T) {
	n, err := Build("Example", []List{
		{Heading: "Go", Components: []Component{
			{Name: "example.com/a", Version: "v1.0.0", Files: lic(mit + "\n\n" + apache)},
			{Name: "example.com/gpl", Version: "v1.0.0", Files: lic(gplNotice)},
		}},
		{Heading: "npm", Components: []Component{
			{Name: "runtime", Version: "1.0.0", Declared: "MIT"},
			{Name: "both", Version: "1.0.0", Declared: "ISC", Files: lic(mit)},
		}},
	}, policy)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range n.Sections {
		for _, e := range s.Entries {
			got = append(got, s.Heading+" "+e.Name+" "+strings.Join(e.Licenses, "+")+map[bool]string{true: " (standard)"}[e.UsesStandard])
		}
	}
	want := []string{
		"Go example.com/a Apache-2.0+MIT",
		"Go example.com/gpl GPL-3.0-or-later",
		"npm runtime MIT (standard)",
		// A license file counts over what package.json says.
		"npm both MIT",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Build =\n%q\nwant\n%q", got, want)
	}
}

// Build lists every problem at once.
func TestBuildRefuses(t *testing.T) {
	_, err := Build("Example", []List{{Heading: "Go", Components: []Component{
		{Name: "example.com/copyleft", Version: "v1.0.0", Files: lic(gplNotice)},
		{Name: "example.com/unknown", Version: "v1.0.0", Files: lic("All rights reserved.")},
		{Name: "example.com/none", Version: "v1.0.0"},
		{Name: "example.com/declared", Version: "v1.0.0", Declared: "BSD-3-Clause"},
		{Name: "example.com/restricted", Version: "v1.0.0", Files: lic(mit + "\nThe Commons Clause applies.")},
	}}}, policy)
	if err == nil {
		t.Fatal("Build accepted all of them")
	}
	for _, want := range []string{
		"example.com/copyleft v1.0.0: GPL-3.0-or-later is not allowed",
		"example.com/unknown v1.0.0: LICENSE: no license recognised",
		"example.com/none v1.0.0: no license file and no declared license",
		`example.com/declared v1.0.0: no license file, and no standard text of its declared license "BSD-3-Clause"`,
		"example.com/restricted v1.0.0: Commons-Clause is not allowed",
		// Reviewed, but the build no longer has it.
		"example.com/gpl: reviewed, but no longer in the build with that license",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say %q:\n%v", want, err)
		}
	}
}

func notices(t *testing.T, lists ...List) string {
	t.Helper()
	p := Policy{Allowed: policy.Allowed, Reviewed: map[string][]string{}}
	for _, l := range lists {
		for _, c := range l.Components {
			if c.Name == "example.com/gpl" {
				p.Reviewed = policy.Reviewed
			}
		}
	}
	n, err := Build("Example", lists, p)
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := n.Write(&a); err != nil {
		t.Fatal(err)
	}
	if err := n.Write(&b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("the same notices were written differently")
	}
	return a.String()
}

// Components with the same files share an entry. A first line that only
// names the package does not make files differ.
func TestWriteGroups(t *testing.T) {
	out := notices(t, List{Heading: "npm packages", Components: []Component{
		{Name: "@ui/button", Version: "9.1.0", Source: "https://example.com/button.tgz", Files: lic("@ui/button\n\n" + mit)},
		{Name: "@ui/menu", Version: "9.2.0", Source: "https://example.com/menu.tgz", Files: lic("@ui/menu\n\n" + mit)},
		{Name: "@ui/noticed", Version: "9.3.0", Files: append(lic(mit), File{Path: "NOTICE", Kind: KindNotice, Text: "Notice of noticed"})},
	}})
	if !strings.Contains(out, "npm packages\n\n  @ui/button 9.1.0   MIT\n  @ui/menu 9.2.0     MIT\n  @ui/noticed 9.3.0  MIT\n") {
		t.Errorf("the list is not as expected:\n%s", out)
	}
	entry := "\n@ui/button 9.1.0\n    https://example.com/button.tgz\n@ui/menu 9.2.0\n    https://example.com/menu.tgz\nLicense: MIT\n"
	if !strings.Contains(out, entry) {
		t.Errorf("the two packages with one license do not share an entry:\n%s", out)
	}
	if strings.Count(out, "Permission is hereby granted") != 2 || strings.Contains(out, "@ui/button\n\nMIT License") {
		t.Errorf("the shared license is not written once without the name line:\n%s", out)
	}
	if !strings.Contains(out, "\nNOTICE:\n\nNotice of noticed\n") {
		t.Errorf("the notice is missing:\n%s", out)
	}
	if strings.Contains(out, "GNU General Public License, version 3\n") || strings.Contains(out, "(standard text)") {
		t.Errorf("an appendix nobody refers to was written:\n%s", out)
	}
}

// The program's own license comes first; under the GNU GPL, with the notice
// the GPL asks for and its text at the end, whatever the components are.
func TestWriteOwnLicense(t *testing.T) {
	n, err := Build("Example", []List{{Heading: "npm", Components: []Component{
		{Name: "react", Version: "19.3.0", Files: lic(mit)},
	}}}, Policy{Allowed: []string{"MIT"}})
	if err != nil {
		t.Fatal(err)
	}
	write := func() string {
		var b bytes.Buffer
		if err := n.Write(&b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}

	n.License, n.Copyright = "GPL-3.0-or-later", "Copyright (C) 2026 Example Authors"
	out := write()
	if !strings.Contains(out, "\nExample Copyright (C) 2026 Example Authors\n\nThis program is free software: you can redistribute it and/or modify it\n") ||
		!strings.Contains(out, "either version 3 of the License, or (at your option) any\nlater version.") ||
		!strings.Contains(out, "本程序是自由软件") {
		t.Errorf("the program's license is not stated as the GPL asks:\n%s", out[:1500])
	}
	if !strings.Contains(out, "\nGNU General Public License, version 3\n") {
		t.Error("the GPL's text is missing although the program is under it")
	}
	if strings.Contains(out, "Some of the components are licensed under the GNU") {
		t.Error("says some components are under the GNU licenses; none is")
	}

	n.License, n.Copyright = "MIT", ""
	out = write()
	if !strings.Contains(out, "\nLicense / 许可证: MIT\n") || strings.Contains(out, "GNU General Public License, version 3") {
		t.Errorf("an MIT program's notices:\n%s", out[:800])
	}
}

// The GPL's text goes at the end when a component is under the GPL or the
// LGPL, as the FSF publishes it; the standard text of a declared license
// when a package has no license file.
func TestWriteAppendices(t *testing.T) {
	gpl, err := os.ReadFile(filepath.Join("..", "..", "tools", "notices", "GPL-3.0.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// https://www.gnu.org/licenses/gpl-3.0.txt
	if sum := sha256.Sum256(gpl); hex.EncodeToString(sum[:]) != "3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986" {
		t.Fatalf("GPL-3.0.txt is not the text the FSF publishes")
	}
	out := notices(t,
		List{Heading: "Go", Components: []Component{{Name: "example.com/gpl", Version: "v1.0.0", Files: lic(gplNotice)}}},
		List{Heading: "npm", Components: []Component{{Name: "runtime", Version: "1.0.0", Declared: "MIT", Author: "The Example Team"}}},
	)
	if !strings.Contains(out, "\nGNU General Public License, version 3\n") || !strings.Contains(out, string(gpl)) {
		t.Errorf("the GPL's text is missing")
	}
	if !strings.Contains(out, "The full text of the\nGNU GPL, version 3, is at the end of this file.") {
		t.Errorf("the introduction does not point to the GPL's text:\n%s", out[:1000])
	}
	if !strings.Contains(out, "This package has no license file. Its package.json declares the MIT\n") ||
		!strings.Contains(out, "Author named in package.json: The Example Team\n") ||
		!strings.Contains(out, "\nThe MIT license (standard text)\n") {
		t.Errorf("the standard MIT text is not referred to and written:\n%s", out)
	}
}
