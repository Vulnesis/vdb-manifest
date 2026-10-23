# vdb-manifest

Last Updated: 2026-10-23

Text level edits to package manifests, and the commands that regenerate their
lockfiles. One module so the CLI (`vulnetix fix`), the language server, vdb-site
and vdb-api produce the same bytes for the same fix.

It is standard library only, plus `Masterminds/semver` for range checks. It reads
and writes strings. It never touches the file system, the network or a shell.

```go
import manifest "github.com/Vulnetix/vdb-manifest"

files := map[string]string{
    "package.json":      packageJSONText,
    "package-lock.json": "", // only its name matters, for package manager detection
}
final, skipped, err := manifest.Apply(files, []manifest.Change{
    {Name: "lodash", Ecosystem: "npm", From: "4.17.20", To: "4.17.21", Method: manifest.MethodDirect, File: "package.json"},
    {Name: "semver", Ecosystem: "npm", From: "7.5.1", To: "7.7.2", Method: manifest.MethodOverride, File: "package.json"},
})

argv, lockFile, ok := manifest.LockCommand("package.json", "npm", "npm")
// argv: npm install --package-lock-only --ignore-scripts --no-audit --no-fund
// run it in the manifest's directory with manifest.LockEnv("npm") added
```

## API

| Name | What |
| --- | --- |
| `Apply(files, changes)` | Writes the changes, returns the final text of every file, plus `[]Skipped`. An `error` means a malformed `Change`. |
| `Change` | `Name`, `Ecosystem`, `From`, `To`, `Method` (`direct`, `override`, `parent`), `File`, `Section`, `PackageManager`. |
| `Skipped` | A change that was not written: `Reason` and, when a command can still do it, `Lock` (argv). |
| `EditManifestText(file, content, name, to)` | One direct bump, for editors that hold a buffer. |
| `LockCommand(manifestPath, ecosystem, packageManager)` | `argv`, lockfile path, `ok`. Never a shell string. Install scripts off. |
| `LockEnv(packageManager)` | Environment that keeps scripts off where a flag is not enough. |
| `DetectPackageManager(siblings, packageManagerField)` | `npm`, `yarn`, `pnpm`, `bun` or `""`. |
| `OverrideSection(file, packageManager)` | Display label such as `package.json > pnpm.overrides`. |
| `Satisfies`, `BestInRange` | Range checks for choosing a fix version. Not for affected status. |

Methods: `direct` bumps a declared dependency. `parent` bumps the direct parent
that brings a vulnerable child (`Name` and `To` are the parent's) and is written
like `direct`. `override` pins a transitive package in the ecosystem's section.

## Writers

| File | direct, parent | override |
| --- | --- | --- |
| `package.json` | dependency blocks | text edit of `overrides` (npm, bun), `pnpm.overrides`, `resolutions` (yarn) |
| `composer.json` | require blocks | adds to `require` (re-marshals, sorted keys, four spaces) |
| `go.mod` | require lines | bumps or adds an `// indirect` require |
| `requirements.txt`, `*.in` | pin or bump | appends a pin |
| `pyproject.toml` | Poetry table, PEP 621 array | uv: `[tool.uv] override-dependencies`; otherwise only if already declared |
| `Cargo.toml` | string and inline table | skipped, reason `lock only`, `Skipped.Lock` is `cargo update -p NAME --precise VERSION` |
| `pom.xml` | `<version>` (not `${property}`) | `<dependencyManagement>` entry |
| `Gemfile` | `gem "name", "range"` | pins or appends a gem |

Files with no writer yet (Pipfile, setup.*, Gradle, sbt, .NET, Swift, Dart, Elixir,
Clojure, Conda) come back as `Skipped` with reason `no writer for this file`.

## Format preservation

`package.json` overrides are spliced into the text. Key order, indentation (spaces
or tabs), CRLF and the trailing newline survive, and a new section goes after the
last existing top level key. Golden cases cover each shape.

## Tests

```sh
go test ./...
go test ./... -run TestGolden -update   # rewrite golden outputs, then read the diff
```

Golden cases are `testdata/golden/*.txt` (see the header of `golden_test.go`).
Cases named `legacy-*` pin the eight formats the CLI had before this module
existed; their output is byte-identical to the CLI's old output.

## Consumers

The CLI imports it through `cli/internal/fix`. vdb-site and vdb-api consume it as a
sibling module (`replace github.com/Vulnetix/vdb-manifest => ../vdb-manifest`),
cloned at container build time like `vdb-sca-match`.
