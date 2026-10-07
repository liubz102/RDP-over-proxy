//go:build ignore

// Release reads the version, which is written only in build/config.yml, and
// makes releases:
//
//	go run tools/release/main.go                       says the version and where it is written
//	go run tools/release/main.go -tag v1.2.3           checks a release tag against it
//	go run tools/release/main.go -print-version [-tag v1.2.3]
//	go run tools/release/main.go -stamp <dir>          writes the Windows resources' inputs with the version
//	go run tools/release/main.go -pack -version 1.2.3 -exe <exe> -out <dir>
//
// With -tag alone it writes, for the release workflow's step outputs,
//
//	version=1.2.3
//	prerelease=false
//
// -print-version writes only the version: the tag's without the "v", or the
// file's when there is no tag. The build uses -print-version and -stamp;
// `wails3 task release` runs the rest. It never changes the version: that is
// the owner's decision, made in build/config.yml.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/liubz102/RDP-over-proxy/tools/release"
)

func main() {
	tag := flag.String("tag", "", "the release tag to check (v1.2.3 or v1.2.3-beta.1)")
	printVersion := flag.Bool("print-version", false, "write only the version")
	stamp := flag.String("stamp", "", "write info.json and wails.exe.manifest with the version into this folder")
	pack := flag.Bool("pack", false, "pack a build into -out")
	version := flag.String("version", "", "with -pack: the version the zip is named by")
	exe := flag.String("exe", "bin/RDP-over-proxy.exe", "with -pack: the built program")
	out := flag.String("out", "", "with -pack: the folder to write into")
	flag.Parse()

	if *pack {
		if *version == "" || *out == "" {
			fail(fmt.Errorf("-pack needs -version and -out"))
		}
		if dirty() {
			fmt.Fprintln(os.Stderr, "note: the working tree has changes that are not committed; this build includes them")
		}
		written, err := release.Pack(release.Files{
			Version: *version,
			Exe:     *exe,
			License: "LICENSE",
			Notices: "frontend/dist/THIRD_PARTY_NOTICES.txt",
		}, *out)
		if err != nil {
			fail(err)
		}
		for _, p := range written {
			fmt.Println(p)
		}
		return
	}

	v, err := release.Version(".")
	if err != nil {
		fail(err)
	}
	switch {
	case *stamp != "":
		if err := release.Stamp(".", *stamp, v); err != nil {
			fail(err)
		}
	case *tag != "":
		named, prerelease, err := release.Check(*tag, v)
		if err != nil {
			fail(err)
		}
		if *printVersion {
			fmt.Println(named)
		} else {
			fmt.Printf("version=%s\nprerelease=%t\n", named, prerelease)
		}
	case *printVersion:
		fmt.Println(v)
	default:
		fmt.Printf("%s (%s, info.version)\n", v, release.ConfigFile)
	}
}

// dirty reports whether git sees uncommitted changes (it only reads).
func dirty() bool {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
