package notices

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const (
	rule = "================================================================================"
	thin = "--------------------------------------------------------------------------------"
)

// Write writes the notices as plain text: the program's own license, the
// components with their licenses, then their license and notice files
// (components with the same files share one entry), then the license texts
// the file refers to (the GNU GPL, when the program or a component is under
// it or the LGPL; standard texts standing in for missing license files).
// The same notices always give the same bytes.
func (n *Notices) Write(w io.Writer) error {
	b := bufio.NewWriter(w)
	groups, gpl, standard := n.groups()
	own, ownGPL := ownLicenses[n.License]

	fmt.Fprintf(b, "%s - License and third-party software / 许可证和第三方软件\n%s\n\n", n.Title, rule)
	if n.Copyright != "" {
		fmt.Fprintf(b, "%s %s\n\n", n.Title, n.Copyright)
	}
	switch {
	case ownGPL:
		b.WriteString(own)
	case n.License != "":
		fmt.Fprintf(b, "License / 许可证: %s\n\n", n.License)
	}
	b.WriteString("This program contains the third-party software listed below. After the list\n" +
		"come their licenses and notices; components with the same license files\n" +
		"share one entry. The source code of each component, at the version used, can\n" +
		"be downloaded from the address under its name.\n\n" +
		"本程序包含下列第三方软件。列表后面是它们的许可证和声明，许可证文件相同的组件合在\n" +
		"一起列出。每个组件所用版本的源代码可以从它名称下面的地址下载。\n\n")
	if gpl {
		b.WriteString("Some of the components are licensed under the GNU General Public License or\n" +
			"the GNU Lesser General Public License (see the list). The full text of the\n" +
			"GNU GPL, version 3, is at the end of this file.\n\n" +
			"其中一些组件按 GNU 通用公共许可证（GPL）或 GNU 宽通用公共许可证（LGPL）授权\n" +
			"（见列表），GNU GPL 第 3 版的全文在本文件末尾。\n\n")
	}

	width := 0
	for _, s := range n.Sections {
		for _, e := range s.Entries {
			width = max(width, len(label(e.Component)))
		}
	}
	width = min(width, 64)
	for _, s := range n.Sections {
		if len(s.Entries) == 0 {
			continue
		}
		fmt.Fprintf(b, "%s\n\n", s.Heading)
		for _, e := range s.Entries {
			fmt.Fprintf(b, "  %-*s  %s\n", width, label(e.Component), strings.Join(e.Licenses, ", "))
		}
		b.WriteString("\n")
	}

	for _, g := range groups {
		b.WriteString("\n" + rule + "\n")
		for _, e := range g.entries {
			b.WriteString(label(e.Component) + "\n")
			if e.Source != "" {
				b.WriteString("    " + e.Source + "\n")
			}
		}
		fmt.Fprintf(b, "License: %s\n%s\n", strings.Join(g.entries[0].Licenses, ", "), thin)
		first := g.entries[0]
		if first.UsesStandard {
			fmt.Fprintf(b, "This package has no license file. Its package.json declares the %s\n"+
				"license, whose standard text is at the end of this file.\n", first.Declared)
			if first.Author != "" {
				fmt.Fprintf(b, "Author named in package.json: %s\n", first.Author)
			}
		}
		for i, f := range first.Files {
			if i > 0 || first.UsesStandard {
				b.WriteString("\n" + thin + "\n")
			}
			fmt.Fprintf(b, "%s:\n\n%s\n", f.Path, clean(f.Text, first.Name))
		}
	}

	if gpl || ownGPL {
		fmt.Fprintf(b, "\n%s\nGNU General Public License, version 3\n%s\n%s", rule, thin, gplText)
	}
	for _, id := range sortedKeys(standard) {
		fmt.Fprintf(b, "\n%s\nThe %s license (standard text)\n%s\n%s", rule, id, thin, standardTexts[id])
	}
	return b.Flush()
}

// ownLicenses are what the file says of the program's own license, for the
// licenses it has words for: the notice the GNU GPL asks a program to carry.
var ownLicenses = map[string]string{
	"GPL-3.0-or-later": "This program is free software: you can redistribute it and/or modify it\n" +
		"under the terms of the GNU General Public License as published by the Free\n" +
		"Software Foundation, either version 3 of the License, or (at your option) any\n" +
		"later version. This program is distributed in the hope that it will be\n" +
		"useful, but WITHOUT ANY WARRANTY; without even the implied warranty of\n" +
		"MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. The text of the license\n" +
		"is at the end of this file.\n\n" +
		"本程序是自由软件：你可以按自由软件基金会发布的 GNU 通用公共许可证第 3 版，或者\n" +
		"（由你选择的）任何更新的版本，再分发和修改它。发布本程序是希望它有用，但不提供\n" +
		"任何担保，也不保证适销性或适用于特定用途。许可证全文在本文件末尾。\n\n",
}

type group struct {
	key     string
	entries []Entry
}

// groups puts entries with the same files together, in the order they
// first appear; gpl says whether a component is under the GPL or the LGPL,
// standard which standard texts entries use.
func (n *Notices) groups() (groups []*group, gpl bool, standard map[string]bool) {
	standard = map[string]bool{}
	byKey := map[string]*group{}
	for _, s := range n.Sections {
		for _, e := range s.Entries {
			for _, id := range e.Licenses {
				if strings.HasPrefix(id, "GPL-") || strings.HasPrefix(id, "LGPL-") {
					gpl = true
				}
			}
			var key strings.Builder
			if e.UsesStandard {
				standard[e.Declared] = true
				key.WriteString("standard:" + e.Declared + "\x00" + e.Author)
			}
			for _, f := range e.Files {
				fmt.Fprintf(&key, "%s\x00%s\x00%s\x01", f.Path, f.Kind, clean(f.Text, e.Name))
			}
			g := byKey[key.String()]
			if g == nil {
				g = &group{key: key.String()}
				byKey[g.key] = g
				groups = append(groups, g)
			}
			g.entries = append(g.entries, e)
		}
	}
	return groups, gpl, standard
}

// clean trims a file's text for the notices: trailing spaces, blank lines
// at either end, and a first line that only names the component (npm
// packages of one project often differ in nothing else).
func clean(text, name string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.EqualFold(strings.TrimSpace(lines[0]), name) {
		lines = lines[1:]
		for len(lines) > 0 && lines[0] == "" {
			lines = lines[1:]
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
