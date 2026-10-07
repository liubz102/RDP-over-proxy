//go:build ignore

// Release checks the version the project's files carry and packs a build:
//
//	go run tools/release/main.go                       lists where the version is written; fails when they differ
//	go run tools/release/main.go -tag v1.2.3           checks a release tag against them
//	go run tools/release/main.go -print-version [-tag v1.2.3]
//	go run tools/release/main.go -pack -version 1.2.3 -exe <exe> -out <dir>
//
// With -tag alone it writes, for the release workflow's step outputs,
//
//	version=1.2.3
//	prerelease=false
//
// -print-version writes only the version a release is named by: the tag's
// without the "v", or the files' when there is no tag. -pack writes the zip,
// the notices and SHA256SUMS.txt into the folder (release.Pack). `wails3 task
// release` runs them all. It never changes a version: that is the owner's
// decision.
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
	printVersion := flag.Bool("print-version", false, "write only the version a release is named by")
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

	places, err := release.Places(".")
	if err != nil {
		fail(err)
	}
	switch {
	case *printVersion && *tag == "":
		v, err := release.Version(places)
		if err != nil {
			fail(err)
		}
		fmt.Println(v)
	case *printVersion:
		v, _, err := release.Check(*tag, places)
		if err != nil {
			fail(err)
		}
		fmt.Println(v)
	case *tag != "":
		v, prerelease, err := release.Check(*tag, places)
		if err != nil {
			fail(err)
		}
		fmt.Printf("version=%s\nprerelease=%t\n", v, prerelease)
	default:
		for _, p := range places {
			fmt.Printf("%-12s %s (%s)\n", p.Version, p.File, p.Where)
		}
		if _, err := release.Version(places); err != nil {
			fail(err)
		}
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
