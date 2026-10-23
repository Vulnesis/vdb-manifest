// Package manifest edits package manifests as text and says how to regenerate
// their lockfiles.
//
// It is the one place the CLI (`vulnetix fix`), the language server and the
// server side manifest curator get their edits from, so a fix suggested in an
// editor, written by the CLI or proposed in a pull request is the same bytes.
//
// Rules every writer follows:
//
//   - Text in, text out. Nothing re-marshals a whole file, so key order,
//     indentation, comments and the trailing newline survive. The one
//     exception is composer.json when the "require" block has to be created
//     (kept byte-identical with the CLI's long-standing behaviour).
//   - Standard library only, plus a semver library for range checks. No file
//     or network access: callers hand over contents and get contents back.
//   - Lockfiles are never edited. [LockCommand] returns the argv a sandboxed
//     runner executes to regenerate one, with install scripts off, never as a
//     shell string.
//
// Entry points:
//
//	Apply            a set of Changes over a set of files
//	EditManifestText one direct version bump on one file
//	LockCommand      argv and lockfile name for a manifest
//	DetectPackageManager  npm, yarn, pnpm or bun from sibling files
//	Satisfies, BestInRange  range checks used to pick fix versions
package manifest
