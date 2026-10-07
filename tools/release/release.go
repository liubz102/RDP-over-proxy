// Package release keeps the version in one place and makes releases. The
// version is the owner's to decide and is written only in build/config.yml
// (info.version). The build passes it to the program (-X main.version) and
// writes the Windows resources' copies (Stamp); a release tag has to match
// it (Check); Pack zips a build. Nothing here changes a version.
package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ConfigFile holds the version, relative to the repository.
const ConfigFile = "build/config.yml"

var (
	// The version under info:, indented; the file's own `version: '3'` is not.
	configVersion = regexp.MustCompile(`(?m)^[ \t]+version:[ \t]*"([^"]*)"`)
	numeric       = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	tagPattern    = regexp.MustCompile(`^v(\d+\.\d+\.\d+)(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	// Our own assembly's version in the manifest, not the Common Controls'.
	manifestVersion = regexp.MustCompile(`(<assemblyIdentity[^>]*name="io\.github\.liubz102\.rdp-over-proxy"[^>]*version=")[^"]*(")`)
)

// Version reads the version from build/config.yml in the repository at
// root. It is major.minor.patch, numbers only, since Windows' file version
// can hold nothing else; a release tag may add a pre-release.
func Version(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ConfigFile)))
	if err != nil {
		return "", err
	}
	m := configVersion.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("%s: no version under info", ConfigFile)
	}
	v := string(m[1])
	if !numeric.MatchString(v) {
		return "", fmt.Errorf("%s: version %q is not major.minor.patch (numbers only)", ConfigFile, v)
	}
	return v, nil
}

// Check compares a release tag with the version. The tag is the version
// with a "v" in front ("v1.2.3") and may add a pre-release ("v1.2.3-beta.1").
// It returns what the program is to call itself (the tag without the "v")
// and whether it is a pre-release.
func Check(tag, version string) (string, bool, error) {
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return "", false, fmt.Errorf("tag %q is not v<major>.<minor>.<patch>, optionally with -<pre-release>", tag)
	}
	if m[1] != version {
		return "", false, fmt.Errorf("tag %s is not the version in %s, %s", tag, ConfigFile, version)
	}
	return strings.TrimPrefix(tag, "v"), m[2] != "", nil
}

// Stamp writes the Windows resources' inputs with the version into out:
// info.json and wails.exe.manifest from build/windows in the repository at
// root, whose own copies carry no version. The build makes the exe's
// resources from out.
func Stamp(root, out, version string) error {
	dir := filepath.Join(root, "build", "windows")
	data, err := os.ReadFile(filepath.Join(dir, "info.json"))
	if err != nil {
		return err
	}
	var info map[string]any
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("info.json: %w", err)
	}
	info["fixed"] = map[string]any{"file_version": version, "product_version": version}
	tables, _ := info["info"].(map[string]any)
	if len(tables) == 0 {
		return fmt.Errorf("info.json: no string table under info")
	}
	for _, t := range tables {
		if table, ok := t.(map[string]any); ok {
			table["FileVersion"] = version
			table["ProductVersion"] = version
		}
	}
	stamped, err := json.MarshalIndent(info, "", "\t")
	if err != nil {
		return err
	}

	manifest, err := os.ReadFile(filepath.Join(dir, "wails.exe.manifest"))
	if err != nil {
		return err
	}
	if !manifestVersion.Match(manifest) {
		return fmt.Errorf("wails.exe.manifest: no assemblyIdentity of the app with a version")
	}
	manifest = manifestVersion.ReplaceAll(manifest, []byte("${1}"+version+".0${2}"))

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "info.json"), append(stamped, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "wails.exe.manifest"), manifest, 0o644)
}
