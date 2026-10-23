package manifest

import (
	"fmt"
	"maps"
	"path"
	"strings"
)

// Method says how a Change reaches the manifest.
type Method string

const (
	// MethodDirect bumps a dependency the manifest declares.
	MethodDirect Method = "direct"
	// MethodOverride pins a transitive package through the ecosystem's
	// override section (npm overrides, pnpm.overrides, resolutions, go.mod
	// indirect requirements, uv override-dependencies, dependencyManagement,
	// a pinned requirement, a composer require, a Gemfile entry).
	MethodOverride Method = "override"
	// MethodParent bumps the direct parent that brings a vulnerable child.
	// Name and To are the PARENT's, and it is written exactly like a direct
	// change; the lockfile then re-resolves the child.
	MethodParent Method = "parent"
)

// Reasons a Change comes back as Skipped. Callers may compare against them.
const (
	ReasonLockOnly     = "lock only"
	ReasonNotDeclared  = "not declared in the manifest"
	ReasonNoWriter     = "no writer for this file"
	ReasonMissingFile  = "file not provided"
	ReasonNoOverride   = "no override section for this project"
	ReasonBadStructure = "the section is not a JSON object"
)

// Change is one edit to one manifest.
type Change struct {
	// Name is the package: an npm name, a Go module path, a Maven
	// groupId:artifactId, a PyPI, crate, gem or composer name.
	Name string `json:"name"`
	// Ecosystem is npm, pypi, golang, cargo, composer, rubygems or maven.
	Ecosystem string `json:"ecosystem"`
	// From is the version in place now. It is informational; writers match on
	// Name and keep the declared range operator.
	From string `json:"from,omitempty"`
	// To is the version to write, without a leading "v".
	To string `json:"to"`
	// Method is direct, override or parent.
	Method Method `json:"method"`
	// File is the key of the manifest in the files map handed to Apply.
	File string `json:"file"`
	// Section labels where the change lands, for display (see OverrideSection).
	// Apply does not read it.
	Section string `json:"section,omitempty"`
	// PackageManager (npm, yarn, pnpm, bun, uv) picks the override style when
	// the caller already knows it. Empty means detect it from the files.
	PackageManager string `json:"packageManager,omitempty"`
}

// Skipped is a Change that was not written, with the reason.
type Skipped struct {
	Change Change `json:"change"`
	Reason string `json:"reason"`
	// Lock is set when the change can still be made by a command: the argv
	// that moves the lockfile (Cargo transitive pins are lock only).
	Lock []string `json:"lock,omitempty"`
}

// Apply writes changes into the manifests in files (path to content) and
// returns the final texts of all of them, files that did not change included.
// The input map is not modified.
//
// A writer is chosen by the lower-cased base name of Change.File. Changes apply
// in order, so two changes to one file compose. A change that cannot be written
// (package not declared, no writer yet, lock only) is reported in the Skipped
// list and never fails the call; an error means the Change itself is invalid
// (missing Name, To or File, or an unknown Method).
func Apply(files map[string]string, changes []Change) (map[string]string, []Skipped, error) {
	out := make(map[string]string, len(files))
	maps.Copy(out, files)
	var skipped []Skipped

	for i, c := range changes {
		switch {
		case c.Name == "":
			return nil, nil, fmt.Errorf("change %d: name is required", i)
		case c.To == "":
			return nil, nil, fmt.Errorf("change %d (%s): to is required", i, c.Name)
		case c.File == "":
			return nil, nil, fmt.Errorf("change %d (%s): file is required", i, c.Name)
		}
		switch c.Method {
		case MethodDirect, MethodOverride, MethodParent:
		default:
			return nil, nil, fmt.Errorf("change %d (%s): unknown method %q", i, c.Name, c.Method)
		}
		content, ok := out[c.File]
		if !ok {
			skipped = append(skipped, Skipped{Change: c, Reason: ReasonMissingFile})
			continue
		}
		var (
			next string
			skip *Skipped
		)
		if c.Method == MethodOverride {
			next, skip = applyOverride(out, c, content)
		} else {
			next, skip = applyDirect(c, content)
		}
		if skip != nil {
			skipped = append(skipped, *skip)
			continue
		}
		out[c.File] = next
	}
	return out, skipped, nil
}

func applyDirect(c Change, content string) (string, *Skipped) {
	next, ok := EditManifestText(c.File, content, c.Name, c.To)
	if ok {
		return next, nil
	}
	if !hasWriter(baseName(c.File)) {
		return content, &Skipped{Change: c, Reason: ReasonNoWriter}
	}
	return content, &Skipped{Change: c, Reason: ReasonNotDeclared}
}

func applyOverride(files map[string]string, c Change, content string) (string, *Skipped) {
	base := baseName(c.File)
	skip := func(reason string) (string, *Skipped) {
		return content, &Skipped{Change: c, Reason: reason}
	}
	switch {
	case base == "package.json":
		pm := c.PackageManager
		if pm == "" {
			pm = DetectPackageManager(siblingNames(files, c.File), packageManagerField(content))
		}
		next, ok := setNpmOverride(content, pm, c.Name, c.To)
		if !ok {
			return skip(ReasonBadStructure)
		}
		return next, nil
	case base == "go.mod":
		next, ok := setGoModRequire(content, c.Name, c.To)
		if !ok {
			return skip(ReasonNoWriter)
		}
		return next, nil
	case base == "cargo.toml":
		return content, &Skipped{Change: c, Reason: ReasonLockOnly, Lock: cargoPinCommand(c.Name, c.To)}
	case base == "pyproject.toml":
		if c.PackageManager == "uv" || isUvProject(content, siblingNames(files, c.File)) {
			next, ok := setUvOverride(content, c.Name, c.To)
			if !ok {
				return skip(ReasonNoOverride)
			}
			return next, nil
		}
		if next, ok := replacePyproject(content, c.Name, c.To); ok {
			return next, nil
		}
		return skip(ReasonNoOverride)
	}
	if next, ok := pinTransitive(c.File, content, c.Name, c.To); ok {
		return next, nil
	}
	if !hasWriter(base) {
		return skip(ReasonNoWriter)
	}
	return skip(ReasonNotDeclared)
}

// hasWriter reports whether a manifest base name has any writer.
func hasWriter(base string) bool {
	switch {
	case base == "package.json", base == "composer.json", base == "go.mod",
		base == "requirements.txt", strings.HasSuffix(base, ".in"),
		base == "pyproject.toml", base == "cargo.toml", base == "pom.xml", base == "gemfile":
		return true
	}
	return false
}

// siblingNames lists the base names of the files that share a directory with file.
func siblingNames(files map[string]string, file string) []string {
	dir := dirName(file)
	var out []string
	for p := range files {
		if dirName(p) == dir {
			out = append(out, path.Base(strings.ReplaceAll(p, "\\", "/")))
		}
	}
	return out
}

// OverrideSection labels where an override for file lands, for display in a
// change list: "package.json > overrides", "package.json > pnpm.overrides",
// "package.json > resolutions", "go.mod > require (indirect)" and so on.
// packageManager is the one DetectPackageManager returned (or "uv" for a uv
// pyproject.toml).
func OverrideSection(file, packageManager string) string {
	name := path.Base(strings.ReplaceAll(file, "\\", "/"))
	switch baseName(file) {
	case "package.json":
		return name + " > " + strings.Join(overridePath(packageManager), ".")
	case "go.mod":
		return name + " > require (indirect)"
	case "pyproject.toml":
		if packageManager == "uv" {
			return name + " > tool.uv.override-dependencies"
		}
		return name
	case "pom.xml":
		return name + " > dependencyManagement"
	case "composer.json":
		return name + " > require"
	case "cargo.toml":
		return "Cargo.lock"
	}
	return name
}
