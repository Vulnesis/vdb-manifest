package manifest

import (
	"path"
	"strings"
)

// LockCommand returns the argument vector that regenerates the lockfile of the
// manifest at manifestPath, and the path of the lockfile it writes (beside the
// manifest). Run it with the manifest's directory as the working directory.
//
// The argv is meant for exec without a shell. Install scripts and lifecycle
// hooks are switched off wherever the tool has a flag for it; for the tools
// that only read a setting, [LockEnv] returns the environment to add.
//
// ecosystem is npm, pypi, golang, cargo, composer, rubygems or maven (the
// vulnetix names). packageManager narrows it: npm, yarn, pnpm or bun for npm
// ("yarn@1" selects yarn classic, anything else yarn berry); poetry or uv for
// pypi. ok is false when the ecosystem has no lockfile to generate (pip,
// Maven) or is unknown.
func LockCommand(manifestPath, ecosystem, packageManager string) (argv []string, lockFile string, ok bool) {
	dir := dirName(manifestPath)
	join := func(name string) string {
		if dir == "" {
			return name
		}
		return path.Join(dir, name)
	}
	pm := strings.ToLower(strings.TrimSpace(packageManager))

	switch strings.ToLower(ecosystem) {
	case "npm":
		switch {
		case pm == PMPnpm:
			return []string{"pnpm", "install", "--lockfile-only", "--ignore-scripts"}, join("pnpm-lock.yaml"), true
		case pm == PMBun:
			return []string{"bun", "install", "--lockfile-only", "--ignore-scripts"}, join("bun.lock"), true
		case pm == PMYarn || strings.HasPrefix(pm, "yarn@"):
			if pm == "yarn@1" || strings.HasPrefix(pm, "yarn@1.") {
				return []string{"yarn", "install", "--ignore-scripts"}, join("yarn.lock"), true
			}
			return []string{"yarn", "install", "--mode=update-lockfile"}, join("yarn.lock"), true
		default:
			return []string{"npm", "install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"}, join("package-lock.json"), true
		}
	case "pypi":
		switch pm {
		case "uv":
			return []string{"uv", "lock"}, join("uv.lock"), true
		case "poetry":
			return []string{"poetry", "lock"}, join("poetry.lock"), true
		}
		return nil, "", false
	case "golang":
		return []string{"go", "mod", "tidy"}, join("go.sum"), true
	case "cargo":
		return []string{"cargo", "update", "--workspace"}, join("Cargo.lock"), true
	case "composer":
		return []string{"composer", "update", "--lock", "--no-scripts", "--no-plugins", "--no-install", "--no-interaction"}, join("composer.lock"), true
	case "rubygems":
		return []string{"bundle", "lock"}, join("Gemfile.lock"), true
	}
	return nil, "", false
}

// LockEnv returns KEY=VALUE pairs to add to the environment of a lock command
// so the package manager runs no install scripts, for the managers where a flag
// is not enough (yarn berry reads a setting). Tools whose flags already disable
// scripts get no extra variables.
func LockEnv(packageManager string) []string {
	pm := strings.ToLower(strings.TrimSpace(packageManager))
	switch {
	case pm == PMNpm || pm == PMPnpm || pm == PMBun || pm == "":
		return []string{"npm_config_ignore_scripts=true"}
	case pm == PMYarn || strings.HasPrefix(pm, "yarn@"):
		return []string{"YARN_ENABLE_SCRIPTS=false", "npm_config_ignore_scripts=true"}
	}
	return nil
}

// cargoPinCommand is the lock only fix for a transitive Cargo crate: cargo has
// no manifest section that pins a dependency of a dependency, so the lockfile
// is moved with `cargo update -p NAME --precise VERSION` instead.
func cargoPinCommand(name, version string) []string {
	return []string{"cargo", "update", "-p", name, "--precise", strings.TrimPrefix(version, "v")}
}
