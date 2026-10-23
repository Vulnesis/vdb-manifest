package manifest

import (
	"path"
	"regexp"
	"strings"
)

var (
	uvTableRe    = regexp.MustCompile(`(?m)^\[tool\.uv\][ \t]*(?:#[^\r\n]*)?\r?$`)
	uvAnyToolRe  = regexp.MustCompile(`(?m)^\[+tool\.uv[\].]`)
	tomlTableRe  = regexp.MustCompile(`(?m)^\[`)
	uvOverrideRe = regexp.MustCompile(`(?m)^override-dependencies[ \t]*=[ \t]*\[`)
	reqNameRe    = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)
	pepSepRe     = regexp.MustCompile(`[-_.]+`)
)

// isUvProject reports whether a pyproject.toml is managed by uv: it has a
// [tool.uv] table (or a sub-table of it), or a uv.lock sits beside it.
func isUvProject(content string, siblings []string) bool {
	for _, s := range siblings {
		s = strings.ReplaceAll(s, "\\", "/")
		if strings.ToLower(path.Base(s)) == "uv.lock" {
			return true
		}
	}
	return uvAnyToolRe.MatchString(content)
}

func normalizePyName(s string) string {
	return pepSepRe.ReplaceAllString(strings.ToLower(s), "-")
}

type tomlSpan struct{ start, end int }

// tomlArrayScan walks the array that opens at s[open], string and comment
// aware, and returns the index of its closing bracket and the spans of its
// string items (quotes included).
func tomlArrayScan(s string, open int) (int, []tomlSpan, bool) {
	var items []tomlSpan
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case '"', '\'':
			q := s[i]
			j := i + 1
			for j < len(s) && s[j] != q {
				if q == '"' && s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				return 0, nil, false
			}
			if depth == 1 {
				items = append(items, tomlSpan{i, j + 1})
			}
			i = j
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i, items, true
			}
		}
	}
	return 0, nil, false
}

// setUvOverride pins name to version in [tool.uv] override-dependencies of a
// pyproject.toml. An existing item for the package is rewritten (keeping its
// environment marker); otherwise an item is added to the array, the array to
// the table, or the table to the file, in that order of preference.
func setUvOverride(content, name, version string) (string, bool) {
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	req := name + "==" + version
	line := `override-dependencies = ["` + req + `"]`

	hdr := uvTableRe.FindStringIndex(content)
	if hdr == nil {
		next := content
		if next != "" && !strings.HasSuffix(next, "\n") {
			next += eol
		}
		if next != "" {
			next += eol
		}
		return next + "[tool.uv]" + eol + line + eol, true
	}
	hdrEnd := hdr[1]
	secEnd := len(content)
	if loc := tomlTableRe.FindStringIndex(content[hdrEnd:]); loc != nil {
		secEnd = hdrEnd + loc[0]
	}

	loc := uvOverrideRe.FindStringIndex(content[hdrEnd:secEnd])
	if loc == nil {
		trimmed := strings.TrimRight(content[hdrEnd:secEnd], " \t\r\n")
		at := hdrEnd + len(trimmed)
		return content[:at] + eol + line + content[at:], true
	}
	open := hdrEnd + loc[1] - 1
	closeIdx, items, ok := tomlArrayScan(content, open)
	if !ok {
		return content, false
	}

	want := normalizePyName(name)
	for _, it := range items {
		raw := content[it.start+1 : it.end-1]
		m := reqNameRe.FindStringSubmatch(raw)
		if m == nil || normalizePyName(m[1]) != want {
			continue
		}
		next := m[1] + "==" + version
		if i := strings.Index(raw, ";"); i >= 0 {
			next += " " + strings.TrimSpace(raw[i:])
		}
		if next == raw {
			return content, true
		}
		return content[:it.start+1] + next + content[it.end-1:], true
	}

	quote := `"` + req + `"`
	if len(items) == 0 {
		return content[:open+1] + quote + content[closeIdx:], true
	}
	last := items[len(items)-1]
	if !strings.Contains(content[open:closeIdx], "\n") {
		return content[:last.end] + ", " + quote + content[last.end:], true
	}
	indent := lineIndent(content, last.start)
	after := content[last.end:closeIdx]
	if strings.Contains(strings.SplitN(after, "\n", 2)[0], ",") {
		// Trailing comma style: add a line after the last item's line.
		nl := strings.Index(content[last.end:], "\n")
		at := last.end + nl + 1
		return content[:at] + indent + quote + "," + eol + content[at:], true
	}
	return content[:last.end] + "," + eol + indent + quote + content[last.end:], true
}
