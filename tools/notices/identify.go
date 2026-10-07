package notices

import (
	"path"
	"regexp"
	"strings"
)

// Identify returns the licenses a license text grants, as SPDX identifiers
// ("MIT", "Apache-2.0", "GPL-3.0-or-later"), in a fixed order; nil when it
// recognises none. One file may hold several licenses (code taken from
// elsewhere keeps its own). It goes by the phrases each license is worded
// with, so it recognises a license, not every condition someone may have
// added to one: the texts are in the notices for people to read.
func Identify(text string) []string {
	t := normalize(text)
	// The GNU licenses mention one another (the GPL ends by recommending
	// the LGPL; the LGPL builds on the GPL), so they are told apart by
	// their titles and by the notice a work carries, and not by the sample
	// notice in the GPL's own appendix.
	body := beforeHowToApply(t)
	var ids []string
	add := func(id string) { ids = append(ids, id) }

	if strings.Contains(t, "gnu lesser general public license version 3, 29 june 2007") ||
		strings.Contains(body, "under the terms of the gnu lesser general public license as published by the free software foundation") {
		id := "LGPL-3.0-only"
		if strings.Contains(body, "or (at your option) any later version") {
			id = "LGPL-3.0-or-later"
		}
		if strings.Contains(t, "as a special exception to the gnu lesser general public license version 3") {
			id += " WITH LGPL-3.0-linking-exception"
		}
		add(id)
	}
	if strings.Contains(t, "gnu affero general public license version 3, 19 november 2007") ||
		strings.Contains(body, "under the terms of the gnu affero general public license as published by the free software foundation") {
		add("AGPL-3.0")
	}
	gplNotice := strings.Contains(body, "under the terms of the gnu general public license as published by the free software foundation")
	switch {
	case gplNotice && strings.Contains(body, "either version 3 of the license, or (at your option) any later version"):
		add("GPL-3.0-or-later")
	case gplNotice && strings.Contains(body, "version 3 of the license"),
		strings.Contains(t, "gnu general public license version 3, 29 june 2007"):
		add("GPL-3.0-only")
	case gplNotice && strings.Contains(body, "version 2 of the license"),
		strings.Contains(t, "gnu general public license version 2, june 1991"):
		add("GPL-2.0")
	case gplNotice:
		add("GPL")
	}
	if strings.Contains(t, "this is free and unencumbered software released into the public domain") {
		add("Unlicense")
	}
	if strings.Contains(t, "mozilla public license version 2.0") ||
		strings.Contains(t, "mozilla public license, version 2.0") ||
		strings.Contains(t, "mozilla public license, v. 2.0") {
		add("MPL-2.0")
	}
	if strings.Contains(t, "apache license version 2.0, january 2004") ||
		strings.Contains(t, "licensed under the apache license, version 2.0") {
		add("Apache-2.0")
	}
	if strings.Contains(t, "redistribution and use in source and binary forms, with or without modification, are permitted provided that") {
		switch {
		case strings.Contains(t, "all advertising materials mentioning features or use of this software"):
			add("BSD-4-Clause")
		case strings.Contains(t, "endorse or promote products derived from this software"):
			add("BSD-3-Clause")
		default:
			add("BSD-2-Clause")
		}
	}
	if strings.Contains(t, "permission is hereby granted, free of charge, to any person obtaining a copy") {
		add("MIT")
	}
	switch {
	case strings.Contains(t, "this software for any purpose with or without fee is hereby granted, provided that the above copyright notice and this permission notice appear in all copies"):
		add("ISC")
	case strings.Contains(t, "this software for any purpose with or without fee is hereby granted."):
		add("0BSD")
	}
	// Licenses no build of this app may contain, named so the refusal
	// says what it found.
	for _, r := range refused {
		if strings.Contains(t, r.phrase) {
			add(r.id)
		}
	}
	return ids
}

// refused are licenses that restrict use; a phrase as general as
// "noncommercial" would not do (the GPL says "noncommercially").
var refused = []struct{ phrase, id string }{
	{"commons clause", "Commons-Clause"},
	{"server side public license", "SSPL-1.0"},
	{"business source license", "BUSL-1.1"},
	{"elastic license", "Elastic-2.0"},
	{"polyform", "PolyForm"},
	{"attribution-noncommercial", "CC-BY-NC"},
}

// beforeHowToApply cuts off the appendix of the GPL and the AGPL, whose
// sample notice says "or (at your option) any later version" whatever the
// work's license is.
func beforeHowToApply(t string) string {
	if i := strings.Index(t, "how to apply these terms to your new programs"); i >= 0 {
		return t[:i]
	}
	return t
}

// normalize makes copies of one license compare equal: case, line breaks,
// typographic quotes and Markdown emphasis differ between them.
func normalize(text string) string {
	text = strings.ToLower(text)
	text = quotes.Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

var quotes = strings.NewReplacer(
	"“", `"`, "”", `"`, "‘", "'", "’", "'",
	"*", "", "#", "", "`", "",
)

// Kinds of files that come with a component.
const (
	// KindLicense is a file that grants a license.
	KindLicense = "license"
	// KindNotice is a notice or patent grant that has to travel with the
	// component (Apache's NOTICE, Go's PATENTS).
	KindNotice = "notice"
)

// FileKind says whether a file name is a license or a notice, by the words
// in it: LICENSE, LICENSE.md, LICENSE-Go, THIRD-PARTY-LICENSE, COPYING,
// NOTICE.txt, PATENTS. Source files (license.go) are neither.
func FileKind(name string) (kind string, ok bool) {
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
	if ext := path.Ext(base); codeExt[ext] {
		return "", false
	}
	kind = ""
	for _, word := range strings.FieldsFunc(base, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
		switch {
		case noticeWord.MatchString(word):
			kind = KindNotice
		case licenseWord.MatchString(word):
			return KindLicense, true
		}
	}
	return kind, kind != ""
}

var (
	licenseWord = regexp.MustCompile(`^([a-z]*licen[cs]es?|copying\d*)$`)
	noticeWord  = regexp.MustCompile(`^([a-z]*notices?|patents)$`)
	codeExt     = map[string]bool{
		".go": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
		".json": true, ".html": true, ".css": true, ".map": true, ".yml": true, ".yaml": true,
	}
)
