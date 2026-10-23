package manifest

import (
	"strings"
)

// setGoModRequire makes go.mod require module at version (with or without the
// leading "v"). An existing requirement keeps its place and any trailing
// comment, so a "// indirect" mark stays. When the module is not required it is
// added as an "// indirect" requirement: inside the last require block that
// already holds indirect entries (else the last require block), at its sorted
// position; with no block, as a new single line requirement after the last
// one (or at the end of the file).
func setGoModRequire(content, module, version string) (string, bool) {
	version = "v" + strings.TrimPrefix(version, "v")
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(content, "\n")

	type block struct{ open, close int }
	var blocks []block
	inBlock := false
	cur := block{}
	lastSingle := -1
	for i, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch {
		case inBlock:
			if line == ")" {
				cur.close = i
				blocks = append(blocks, cur)
				inBlock = false
				continue
			}
			if module2, ok := goModEntry(line); ok && module2 == module {
				lines[i] = replaceGoModVersion(raw, module, version)
				return strings.Join(lines, "\n"), true
			}
		case isRequireOpen(line):
			inBlock = true
			cur = block{open: i}
		case strings.HasPrefix(line, "require ") || strings.HasPrefix(line, "require\t"):
			lastSingle = i
			if module2, ok := goModEntry(strings.TrimSpace(line[len("require"):])); ok && module2 == module {
				lines[i] = replaceGoModVersion(raw, module, version)
				return strings.Join(lines, "\n"), true
			}
		}
	}
	if inBlock {
		return content, false
	}

	entry := module + " " + version + " // indirect"

	if len(blocks) > 0 {
		target := blocks[len(blocks)-1]
		for _, b := range blocks {
			for i := b.open + 1; i < b.close; i++ {
				if strings.Contains(lines[i], "// indirect") {
					target = b
					break
				}
			}
		}
		at := target.close
		for i := target.open + 1; i < target.close; i++ {
			other, ok := goModEntry(strings.TrimSpace(strings.TrimSuffix(lines[i], "\r")))
			if ok && other > module {
				at = i
				break
			}
		}
		line := "\t" + entry
		if strings.HasSuffix(lines[target.close], "\r") {
			line += "\r"
		}
		lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
		return strings.Join(lines, "\n"), true
	}

	line := "require " + entry
	if lastSingle >= 0 {
		lines = append(lines[:lastSingle+1], append([]string{line}, lines[lastSingle+1:]...)...)
		return strings.Join(lines, "\n"), true
	}
	next := content
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += eol
	}
	if next != "" {
		next += eol
	}
	return next + line + eol, true
}

// isRequireOpen reports whether a trimmed go.mod line opens a require block.
func isRequireOpen(line string) bool {
	rest, ok := strings.CutPrefix(line, "require")
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	if i := strings.Index(rest, "//"); i >= 0 {
		rest = strings.TrimSpace(rest[:i])
	}
	return rest == "("
}

// goModEntry returns the module path of a go.mod requirement line such as
// "golang.org/x/net v0.20.0 // indirect".
func goModEntry(line string) (string, bool) {
	f := strings.Fields(line)
	if len(f) < 2 || strings.HasPrefix(f[0], "//") {
		return "", false
	}
	return f[0], true
}

// replaceGoModVersion swaps the version token of a requirement line and keeps
// everything else, including indentation and a trailing comment.
func replaceGoModVersion(line, module, version string) string {
	at := strings.Index(line, module)
	if at < 0 {
		return line
	}
	rest := line[at+len(module):]
	i := 0
	for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
		i++
	}
	j := i
	for j < len(rest) && rest[j] != ' ' && rest[j] != '\t' && rest[j] != '\r' {
		j++
	}
	return line[:at+len(module)] + rest[:i] + version + rest[j:]
}
