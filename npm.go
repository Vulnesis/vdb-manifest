package manifest

import (
	"encoding/json"
	"path"
	"strings"
)

// Package manager names returned by DetectPackageManager.
const (
	PMNpm  = "npm"
	PMYarn = "yarn"
	PMPnpm = "pnpm"
	PMBun  = "bun"
)

// DetectPackageManager names the JavaScript package manager of a package.json
// from the file names that sit beside it and the value of its "packageManager"
// field (for example "pnpm@9.1.0+sha512.abc"). The field wins, then lockfiles,
// then the weaker signals (pnpm-workspace.yaml, .yarnrc.yml, bunfig.toml). Of
// several lockfiles pnpm beats yarn beats bun beats npm. It returns "" when
// nothing says; callers treat that as npm.
//
// siblings may hold bare names or paths, only the last element is read.
func DetectPackageManager(siblings []string, packageManagerField string) string {
	if f := strings.ToLower(strings.TrimSpace(packageManagerField)); f != "" {
		name := f
		if i := strings.Index(name, "@"); i >= 0 {
			name = name[:i]
		}
		switch name {
		case PMNpm, PMYarn, PMPnpm, PMBun:
			return name
		}
	}
	have := map[string]bool{}
	for _, s := range siblings {
		s = strings.ReplaceAll(s, "\\", "/")
		have[strings.ToLower(path.Base(s))] = true
	}
	switch {
	case have["pnpm-lock.yaml"]:
		return PMPnpm
	case have["yarn.lock"]:
		return PMYarn
	case have["bun.lockb"] || have["bun.lock"]:
		return PMBun
	case have["package-lock.json"] || have["npm-shrinkwrap.json"]:
		return PMNpm
	case have["pnpm-workspace.yaml"]:
		return PMPnpm
	case have[".yarnrc.yml"]:
		return PMYarn
	case have["bunfig.toml"]:
		return PMBun
	}
	return ""
}

// packageManagerField reads the "packageManager" value of a package.json.
func packageManagerField(content string) string {
	var doc struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return ""
	}
	return doc.PackageManager
}

// overridePath is the chain of keys under which package manager pm keeps its
// overrides in package.json: npm and bun "overrides", pnpm "pnpm.overrides",
// yarn "resolutions".
func overridePath(pm string) []string {
	switch strings.ToLower(pm) {
	case PMYarn:
		return []string{"resolutions"}
	case PMPnpm:
		return []string{"pnpm", "overrides"}
	default:
		return []string{"overrides"}
	}
}

// setNpmOverride pins name to version in the override section of a package.json
// for package manager pm, as a text edit. The section is created after the last
// top-level key when it is absent. It reports false when the document is not a
// JSON object or the section is not an object.
func setNpmOverride(content, pm, name, version string) (string, bool) {
	return setJSONEntry(content, overridePath(pm), name, version)
}
