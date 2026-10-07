package notices_test

import (
	. "github.com/liubz102/RDP-over-proxy/tools/notices"

	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The sentences each license is recognised by, as the licenses word them.
const (
	mit = `MIT License

Copyright (c) 2020 Example Authors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction.`
	bsd3 = `Copyright (c) 2009 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:
   * Neither the name of Google Inc. nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.`
	bsd2 = `Copyright (c) 2013 Example Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

  Redistributions of source code must retain the above copyright notice.`
	apache = `
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION`
	gplNotice = `Copyright (C) 2022 by Example <dev@example.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.`
	lgplWithException = `This software is licensed under the LGPLv3, included below.

As a special exception to the GNU Lesser General Public License version 3
("LGPL3"), the copyright holders of this Library give you permission to
convey to a third party a Combined Work that links statically or dynamically
to this Library.

                   GNU LESSER GENERAL PUBLIC LICENSE
                       Version 3, 29 June 2007

  This version of the GNU Lesser General Public License incorporates
the terms and conditions of version 3 of the GNU General Public
License, supplemented by the additional permissions listed below.`
)

func TestIdentify(t *testing.T) {
	gpl, err := os.ReadFile(filepath.Join("..", "..", "tools", "notices", "GPL-3.0.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, text string
		want       []string
	}{
		{"MIT", mit, []string{"MIT"}},
		{"MIT in Markdown, wrapped differently", "**MIT License**\n\nPermission is hereby granted, free of charge,\nto any person obtaining a copy", []string{"MIT"}},
		{"ISC", "ISC License (ISC)\n\nPermission to use, copy, modify, and/or distribute this software for any purpose\nwith or without fee is hereby granted, provided that the above copyright notice\nand this permission notice appear in all copies.", []string{"ISC"}},
		{"0BSD", "Permission to use, copy, modify, and/or distribute this software for any\npurpose with or without fee is hereby granted.\n\nTHE SOFTWARE IS PROVIDED \"AS IS\"", []string{"0BSD"}},
		{"BSD-3-Clause", bsd3, []string{"BSD-3-Clause"}},
		{"BSD-2-Clause", bsd2, []string{"BSD-2-Clause"}},
		{"BSD-4-Clause", bsd2 + "\n  All advertising materials mentioning features or use of this software\n  must display the following acknowledgement", []string{"BSD-4-Clause"}},
		{"Apache-2.0", apache, []string{"Apache-2.0"}},
		{"Apache-2.0 header", `Licensed under the Apache License, Version 2.0 (the "License");`, []string{"Apache-2.0"}},
		{"MPL-2.0", "Mozilla Public License Version 2.0\n==================================", []string{"MPL-2.0"}},
		{"MPL-2.0 header", "This Source Code Form is subject to the terms of the Mozilla Public\nLicense, v. 2.0.", []string{"MPL-2.0"}},
		{"GPL notice, or later", gplNotice, []string{"GPL-3.0-or-later"}},
		{"GPL notice, version 3 only", "it under the terms of the GNU General Public License as published by\nthe Free Software Foundation, version 3 of the License.", []string{"GPL-3.0-only"}},
		// The GPL's own appendix suggests "or any later version", and its
		// last words recommend the LGPL; neither is this text's license.
		{"the GPL itself", string(gpl), []string{"GPL-3.0-only"}},
		{"LGPL with the linking exception", lgplWithException, []string{"LGPL-3.0-only WITH LGPL-3.0-linking-exception"}},
		{"LGPL notice, or later", "under the terms of the GNU Lesser General Public License as published by\nthe Free Software Foundation, either version 3 of the License, or\n(at your option) any later version.", []string{"LGPL-3.0-or-later"}},
		{"AGPL notice", "under the terms of the GNU Affero General Public License as published by\nthe Free Software Foundation", []string{"AGPL-3.0"}},
		{"Unlicense", "This is free and unencumbered software released into the public domain.", []string{"Unlicense"}},
		// Code taken from elsewhere keeps its license in the same file.
		{"MIT and Apache-2.0", mit + "\n\nlocaltime.go:\n" + apache, []string{"Apache-2.0", "MIT"}},
		{"Apache-2.0 and BSD-3-Clause", apache + "\n\n" + bsd3, []string{"Apache-2.0", "BSD-3-Clause"}},
		{"MIT with the Commons Clause", mit + "\n\n\"Commons Clause\" License Condition v1.0", []string{"MIT", "Commons-Clause"}},
		{"not a license", "See the LICENSE-MIT and LICENSE-APACHE files.", nil},
	}
	for _, c := range cases {
		if got := Identify(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Identify = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFileKind(t *testing.T) {
	cases := []struct {
		name, kind string
		ok         bool
	}{
		{"LICENSE", KindLicense, true},
		{"LICENSE.md", KindLicense, true},
		{"license.txt", KindLicense, true},
		{"LICENCE", KindLicense, true},
		{"LICENSE-Go", KindLicense, true},
		{"LICENSE.libyaml", KindLicense, true},
		{"LICENSE-MIT", KindLicense, true},
		{"THIRD-PARTY-LICENSE", KindLicense, true},
		{"COPYING", KindLicense, true},
		{"COPYING3", KindLicense, true},
		{"UNLICENSE", KindLicense, true},
		{"NOTICE", KindNotice, true},
		{"NOTICE.txt", KindNotice, true},
		{"ThirdPartyNotices.txt", KindNotice, true},
		{"PATENTS", KindNotice, true},
		{"license.go", "", false},
		{"licenses.json", "", false},
		{"license_test.go", "", false},
		{"README.md", "", false},
		{"AUTHORS", "", false},
		{"CONTRIBUTORS_GUIDE.md", "", false},
	}
	for _, c := range cases {
		kind, ok := FileKind(c.name)
		if kind != c.kind || ok != c.ok {
			t.Errorf("FileKind(%q) = %q, %v; want %q, %v", c.name, kind, ok, c.kind, c.ok)
		}
	}
}
