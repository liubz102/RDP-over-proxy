//go:build ignore

// Notices writes the third-party notices of the Windows build: the Go
// modules linked into the exe and the npm packages whose code is in the
// frontend bundle, with their licenses and notices. The build runs it after
// the frontend and before the exe, which embeds frontend/dist:
//
//	go run tools/notices/main.go [-goarch amd64] [-out file]
//
// It reads which npm packages the bundle holds from what the frontend's
// bundler reported (frontend/plugins/bundledPackages.ts), and fails when a
// component has no license it recognises or one the policy below does not
// allow it.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/liubz102/RDP-over-proxy/tools/notices"
)

// policy is what a build may contain. Copyleft is allowed only to the
// components named here, each looked at first: why it is in the build and
// what its license asks of the program.
var policy = notices.Policy{
	Allowed: []string{"0BSD", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "MIT", "MPL-2.0"},
	Reviewed: map[string][]string{
		// Xray-core uses them for Shadowsocks 2022 and in its transport
		// code; it cannot be built without them. They are why the program
		// itself is under the GPL (docs/PROGRESS.md, 2026-10-07).
		"github.com/sagernet/sing":             {"GPL-3.0-or-later"},
		"github.com/sagernet/sing-shadowsocks": {"GPL-3.0-or-later"},
		// REALITY's rate limiter. Its exception lets a program link it
		// without the LGPL's relinking requirements.
		"github.com/juju/ratelimit": {"LGPL-3.0-only WITH LGPL-3.0-linking-exception"},
	},
}

func main() {
	goarch := flag.String("goarch", "amd64", "the architecture of the build")
	tags := flag.String("tags", "production", "the build tags of the build")
	frontend := flag.String("frontend", "frontend", "the frontend folder")
	out := flag.String("out", "", "the file to write (default <frontend>/dist/THIRD_PARTY_NOTICES.txt)")
	flag.Parse()
	if *out == "" {
		*out = filepath.Join(*frontend, "dist", "THIRD_PARTY_NOTICES.txt")
	}
	if err := run(*goarch, *tags, *frontend, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(goarch, tags, frontend, out string) error {
	// The bundler's report: the folders of the packages whose code went
	// into the bundle, relative to the frontend folder.
	report := filepath.Join(frontend, "dist", ".vite", "bundled-packages.json")
	data, err := os.ReadFile(report)
	if err != nil {
		return fmt.Errorf("%w (build the frontend first)", err)
	}
	var dirs []string
	if err := json.Unmarshal(data, &dirs); err != nil {
		return fmt.Errorf("%s: %w", report, err)
	}
	npm, err := notices.NpmPackages(frontend, dirs)
	if err != nil {
		return err
	}

	env := append(os.Environ(), "GOOS=windows", "GOARCH="+goarch, "CGO_ENABLED=0")
	listed, err := command(env, "go", "list", "-deps", "-tags", tags, "-json=ImportPath,Dir,Standard,Module", ".")
	if err != nil {
		return err
	}
	pkgs, err := notices.ReadGoList(bytes.NewReader(listed))
	if err != nil {
		return err
	}
	mods, err := notices.GoModules(pkgs)
	if err != nil {
		return err
	}
	goenv, err := command(env, "go", "env", "GOROOT", "GOVERSION")
	if err != nil {
		return err
	}
	// One value per line; GOROOT may have spaces in it.
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(goenv), "\r\n", "\n")), "\n")
	if len(lines) != 2 {
		return fmt.Errorf("go env: unexpected output %q", goenv)
	}
	toolchain, err := notices.GoToolchain(lines[0], lines[1])
	if err != nil {
		return err
	}

	n, err := notices.Build("RDP over Proxy", []notices.List{
		{Heading: "Go modules, linked into the program / 链接进程序的 Go 模块", Components: append([]notices.Component{toolchain}, mods...)},
		{Heading: "npm packages, in the program's window / 程序窗口里的 npm 包", Components: npm},
	}, policy)
	if err != nil {
		return err
	}
	// The program's own license, the owner's decision (LICENSE).
	n.License = "GPL-3.0-or-later"
	n.Copyright = "Copyright (C) 2026 liubz102"
	var buf bytes.Buffer
	if err := n.Write(&buf); err != nil {
		return err
	}
	return os.WriteFile(out, buf.Bytes(), 0o644)
}

// command runs a command and returns what it wrote; what it complained
// about goes to stderr.
func command(env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}
