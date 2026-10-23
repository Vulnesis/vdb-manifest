# vdb-manifest notes for agents

Last Updated: 2026-10-23

Purpose: one implementation of manifest edits and lock commands for the CLI,
vdb-site and vdb-api. See README.md for the API.

## Rules

- Standard library only (plus semver). No file, network or process access. Callers
  pass text and receive text.
- Text in, text out. Do not decode and re-encode a whole document. Splice. If a
  format truly needs a parser, say so in the writer's doc comment.
- The eight legacy writers in `formats.go` are byte-identical with what the CLI
  produced before extraction. Do not "tidy" their regular expressions or output;
  a change there changes `vulnetix fix` and the golden `legacy-*` cases. Improve a
  legacy writer only with a golden case that shows the new output and a note in the
  commit.
- Lock commands are argv slices for exec, never `sh -c` strings, with install
  scripts off. Add environment through `LockEnv`.
- No em dashes in prose or comments.

## How to add a writer

1. Pick the file by lower-cased base name. Add the name to `hasWriter` in
   `apply.go` so a miss reports "not declared" and not "no writer".
2. Direct bumps: add a case to `EditManifestText` in `formats.go` with a
   `replaceXxx(content, name, target) (string, bool)` function. Return false when
   the package is not declared. Keep the range operator with `preserveRangeOperator`.
3. Overrides: add a case to `applyOverride` in `apply.go`. Write the edit as a
   function that returns `(string, bool)` and is idempotent (applying it twice
   gives the same text). For JSON use `setJSONEntry` (jsonedit.go). If the
   ecosystem can only pin in the lockfile, return a `Skipped` with
   `ReasonLockOnly` and put the command in `Skipped.Lock`.
4. Add a label to `OverrideSection`.
5. Lockfile: add the ecosystem to `LockCommand` (and `LockEnv` if a variable is
   needed). Say in the doc comment what runs and why scripts are off.
6. Tests: add `testdata/golden/<name>.txt` cases (a changed file, a skipped one,
   a format quirk such as tabs, CRLF or no trailing newline). Run
   `go test ./... -run TestGolden -update`, read the diff, commit it. Cases not
   prefixed `legacy-` must be idempotent; the harness checks it.
7. Update the writers table in README.md.

## Checks

```sh
go vet ./... && gofmt -l . && go test ./...
```

`gofmt -l .` must print nothing.

## Consumers and wiring

- CLI: `cli/internal/fix` delegates (`edit.go`, `commands.go`, `satisfy.go`).
  Its go.mod has a `replace` to `../vdb-manifest`; go.work lists it.
- vdb-site and vdb-api: clone `https://github.com/Vulnetix/vdb-manifest.git` in the
  Containerfile next to `vdb-sca-match`, add `replace github.com/Vulnetix/vdb-manifest => ../vdb-manifest`.
