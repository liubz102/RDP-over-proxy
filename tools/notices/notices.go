// Package notices builds the third-party notices of the app's build: every
// Go module linked into the exe and every npm package whose code is in the
// window's page, with the licenses and notices that come with them, and
// where each one's source code can be downloaded.
//
// It refuses what it cannot vouch for: a component without a license file
// (or, for an npm package, a declared license whose standard text it has),
// a license text it does not recognise, and a license the policy does not
// allow for that component. The build runs it (tools/notices/main.go), so
// a dependency that brings any of these stops the build.
package notices

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// A Component is one module or package in the build.
type Component struct {
	// Name is the module path ("github.com/xtls/xray-core") or the package
	// name ("react").
	Name    string
	Version string
	// Source is where the component's source code at this version can be
	// downloaded.
	Source string
	// Files are the license and notice files that come with it.
	Files []File
	// Declared is the license the package's own metadata names (npm's
	// "license"). It counts only when there is no license file.
	Declared string
	// Author is who the package's metadata names as its author, shown with
	// a declared license.
	Author string
}

// A File is a license or notice file of a component.
type File struct {
	// Path is relative to the component's root, with forward slashes.
	Path string
	Kind string // KindLicense or KindNotice
	Text string
}

// Policy says which licenses a build may contain.
type Policy struct {
	// Allowed are the licenses any component may have.
	Allowed []string
	// Reviewed allows licenses beyond those to the components named: the
	// copyleft ones someone has looked at and accepted for this app.
	Reviewed map[string][]string
}

// A List is components under a heading that says what they are ("Go
// modules, linked into the program").
type List struct {
	Heading    string
	Components []Component
}

// Notices are the components of a build with their licenses, checked
// against a policy. Write turns them into the notices file.
type Notices struct {
	// Title names the program at the top of the file.
	Title string
	// License is the program's own license (SPDX), which the file states at
	// the top; for the GNU GPL, with the notice the GPL asks for and the
	// license's text at the end. Copyright goes with the title
	// ("Copyright (C) 2026 Someone").
	License   string
	Copyright string
	Sections  []Section
}

// A Section is a list's components with what was found about them.
type Section struct {
	Heading string
	Entries []Entry
}

// An Entry is a component with what was found about it.
type Entry struct {
	Component
	// Licenses are the licenses of its license files, or its declared
	// license when it has none.
	Licenses []string
	// UsesStandard is set when the standard text of the declared license
	// stands in for a license file the package does not have.
	UsesStandard bool
}

// Build identifies the licenses of every component in lists and checks
// them against p. It reports every problem at once.
func Build(title string, lists []List, p Policy) (*Notices, error) {
	allowed := map[string]bool{}
	for _, id := range p.Allowed {
		allowed[id] = true
	}
	var problems []string
	used := map[string]bool{}
	n := &Notices{Title: title}
	for _, l := range lists {
		out := Section{Heading: l.Heading}
		for _, c := range l.Components {
			entry := Entry{Component: c}
			licenses, missing := licensesOf(c)
			for _, path := range missing {
				problems = append(problems, fmt.Sprintf("%s: %s: no license recognised", label(c), path))
			}
			if len(licenses) == 0 && len(missing) == 0 {
				switch {
				case c.Declared == "":
					problems = append(problems, fmt.Sprintf("%s: no license file and no declared license", label(c)))
				case standardTexts[c.Declared] == "":
					problems = append(problems, fmt.Sprintf("%s: no license file, and no standard text of its declared license %q", label(c), c.Declared))
				default:
					licenses = []string{c.Declared}
					entry.UsesStandard = true
				}
			}
			entry.Licenses = licenses
			for _, id := range licenses {
				if allowed[id] {
					continue
				}
				if slices.Contains(p.Reviewed[c.Name], id) {
					used[c.Name] = true
					continue
				}
				problems = append(problems, fmt.Sprintf("%s: %s is not allowed", label(c), id))
			}
			out.Entries = append(out.Entries, entry)
		}
		n.Sections = append(n.Sections, out)
	}
	// A review the build no longer needs would allow its license to
	// whatever comes next under that name.
	for _, name := range sortedKeys(p.Reviewed) {
		if !used[name] {
			problems = append(problems, fmt.Sprintf("%s: reviewed, but no longer in the build with that license (remove it from the policy)", name))
		}
	}
	if len(problems) > 0 {
		return nil, errors.New("third-party notices:\n  " + strings.Join(problems, "\n  "))
	}
	return n, nil
}

// licensesOf identifies the licenses in c's license files, in the order of
// the files; missing are the license files where it recognised none.
func licensesOf(c Component) (licenses, missing []string) {
	for _, f := range c.Files {
		if f.Kind != KindLicense {
			continue
		}
		ids := Identify(f.Text)
		if len(ids) == 0 {
			missing = append(missing, f.Path)
		}
		for _, id := range ids {
			if !slices.Contains(licenses, id) {
				licenses = append(licenses, id)
			}
		}
	}
	return licenses, missing
}

func label(c Component) string {
	if c.Version == "" {
		return c.Name
	}
	return c.Name + " " + c.Version
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// gplText is the GNU General Public License, version 3, as the FSF
// publishes it (https://www.gnu.org/licenses/gpl-3.0.txt). The GPL and the
// LGPL require that it travel with the work, and the GPL components here
// carry only the notice that points to it.
//
//go:embed GPL-3.0.txt
var gplText string

// mitText is the MIT License without a copyright line, for packages that
// declare it but ship no license file.
const mitText = `MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

// standardTexts are the licenses whose standard text can stand in for a
// missing license file.
var standardTexts = map[string]string{"MIT": mitText}
