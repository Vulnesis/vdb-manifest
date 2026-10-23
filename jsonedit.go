package manifest

import (
	"bytes"
	"encoding/json"
	"strings"
)

// This file edits JSON by splicing text. It never decodes and re-encodes the
// document, so key order, indentation, a trailing newline and any odd spacing
// survive. It understands just enough JSON to find object members: strings with
// escapes, nested objects and arrays, and literals.

type jsonMember struct {
	key              string
	keyStart, keyEnd int // span of the quoted key
	valStart, valEnd int // span of the value
}

func jsonSkipWS(s string, i int) int {
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

// jsonStringEnd returns the index just after the string that opens at s[i], or
// -1 when it never closes.
func jsonStringEnd(s string, i int) int {
	if i >= len(s) || s[i] != '"' {
		return -1
	}
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return -1
}

// jsonValueEnd returns the index just after the value that starts at s[i], or
// -1 when the text is not a well formed value.
func jsonValueEnd(s string, i int) int {
	if i >= len(s) {
		return -1
	}
	switch s[i] {
	case '"':
		return jsonStringEnd(s, i)
	case '{', '[':
		depth := 0
		for j := i; j < len(s); j++ {
			switch s[j] {
			case '"':
				end := jsonStringEnd(s, j)
				if end < 0 {
					return -1
				}
				j = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1
				}
			}
		}
		return -1
	default:
		j := i
		for j < len(s) && !strings.ContainsRune(",}] \t\r\n", rune(s[j])) {
			j++
		}
		if j == i {
			return -1
		}
		return j
	}
}

// jsonMembers lists the members of the object that opens at s[open] and returns
// the index of its closing brace.
func jsonMembers(s string, open int) ([]jsonMember, int, bool) {
	if open >= len(s) || s[open] != '{' {
		return nil, 0, false
	}
	var out []jsonMember
	i := jsonSkipWS(s, open+1)
	if i < len(s) && s[i] == '}' {
		return out, i, true
	}
	for {
		i = jsonSkipWS(s, i)
		if i >= len(s) || s[i] != '"' {
			return nil, 0, false
		}
		keyEnd := jsonStringEnd(s, i)
		if keyEnd < 0 {
			return nil, 0, false
		}
		var key string
		if err := json.Unmarshal([]byte(s[i:keyEnd]), &key); err != nil {
			return nil, 0, false
		}
		j := jsonSkipWS(s, keyEnd)
		if j >= len(s) || s[j] != ':' {
			return nil, 0, false
		}
		valStart := jsonSkipWS(s, j+1)
		valEnd := jsonValueEnd(s, valStart)
		if valEnd < 0 {
			return nil, 0, false
		}
		out = append(out, jsonMember{key: key, keyStart: i, keyEnd: keyEnd, valStart: valStart, valEnd: valEnd})
		j = jsonSkipWS(s, valEnd)
		if j >= len(s) {
			return nil, 0, false
		}
		switch s[j] {
		case ',':
			i = j + 1
		case '}':
			return out, j, true
		default:
			return nil, 0, false
		}
	}
}

func jsonQuote(v string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(buf.String(), "\n")
}

// lineIndent is the leading whitespace of the line that holds s[pos].
func lineIndent(s string, pos int) string {
	start := strings.LastIndexByte(s[:pos], '\n') + 1
	end := start
	for end < pos && (s[end] == ' ' || s[end] == '\t') {
		end++
	}
	return s[start:end]
}

type jsonStyle struct {
	eol   string
	unit  string // one indent level
	multi bool   // the document is laid out over several lines
}

func detectJSONStyle(s string, rootOpen int) jsonStyle {
	st := jsonStyle{eol: "\n", unit: "  "}
	if strings.Contains(s, "\r\n") {
		st.eol = "\r\n"
	}
	members, closeIdx, ok := jsonMembers(s, rootOpen)
	if !ok {
		return st
	}
	if len(members) == 0 {
		st.multi = strings.Contains(s[rootOpen:closeIdx], "\n") || strings.Contains(strings.TrimRight(s, "\r\n"), "\n")
		return st
	}
	if strings.Contains(s[rootOpen+1:members[0].keyStart], "\n") {
		st.multi = true
		if ind := lineIndent(s, members[0].keyStart); ind != "" {
			st.unit = ind
		}
	}
	return st
}

// setJSONEntry makes path.name equal to the string value in a JSON document,
// creating the objects along path (and the name) when they are absent. A new
// member is added after the last existing one. When name already holds an
// object (npm allows nested overrides such as {"foo": {".": "1.2.3"}}) the "."
// key of that object is set instead, so nothing nested is lost. It reports
// false when the document or an object on the path is not what was expected.
func setJSONEntry(content string, path []string, name, value string) (string, bool) {
	root := jsonSkipWS(content, 0)
	if root >= len(content) || content[root] != '{' {
		return content, false
	}
	st := detectJSONStyle(content, root)
	return upsertJSON(content, root, path, name, value, st)
}

func upsertJSON(s string, open int, path []string, name, value string, st jsonStyle) (string, bool) {
	members, closeIdx, ok := jsonMembers(s, open)
	if !ok {
		return s, false
	}
	want := name
	if len(path) > 0 {
		want = path[0]
	}
	for _, m := range members {
		if m.key != want {
			continue
		}
		cur := s[m.valStart:m.valEnd]
		if len(path) > 0 {
			if cur[0] != '{' {
				return s, false
			}
			return upsertJSON(s, m.valStart, path[1:], name, value, st)
		}
		switch cur[0] {
		case '"':
			quoted := jsonQuote(value)
			if cur == quoted {
				return s, true
			}
			return s[:m.valStart] + quoted + s[m.valEnd:], true
		case '{':
			return upsertJSON(s, m.valStart, nil, ".", value, st)
		default:
			return s, false
		}
	}
	return insertJSONMember(s, open, closeIdx, members, want, path, name, value, st), true
}

// renderJSONNested renders the object that holds the chain path -> name: value,
// laid out at indent.
func renderJSONNested(path []string, name, value, indent, colon string, st jsonStyle) string {
	key := name
	if len(path) > 0 {
		key = path[0]
	}
	child := indent + st.unit
	var inner string
	if len(path) == 0 {
		inner = jsonQuote(key) + colon + jsonQuote(value)
	} else {
		inner = jsonQuote(key) + colon + renderJSONNested(path[1:], name, value, child, colon, st)
	}
	if !st.multi {
		return "{" + inner + "}"
	}
	return "{" + st.eol + child + inner + st.eol + indent + "}"
}

func insertJSONMember(s string, open, closeIdx int, members []jsonMember, key string, path []string, name, value string, st jsonStyle) string {
	render := func(indent, colon string) string {
		if len(path) == 0 {
			return jsonQuote(key) + colon + jsonQuote(value)
		}
		return jsonQuote(key) + colon + renderJSONNested(path[1:], name, value, indent, colon, st)
	}
	if len(members) == 0 {
		objIndent := lineIndent(s, open)
		if st.multi {
			child := objIndent + st.unit
			text := st.eol + child + render(child, ": ") + st.eol + objIndent
			return s[:open+1] + text + s[closeIdx:]
		}
		return s[:open+1] + render("", ":") + s[closeIdx:]
	}
	last := members[len(members)-1]
	colon := s[last.keyEnd:last.valStart]
	if strings.Contains(s[open+1:members[0].keyStart], "\n") {
		indent := lineIndent(s, members[0].keyStart)
		return s[:last.valEnd] + "," + st.eol + indent + render(indent, colon) + s[last.valEnd:]
	}
	sep := ","
	if strings.Contains(colon, " ") {
		sep = ", "
	}
	return s[:last.valEnd] + sep + render("", colon) + s[last.valEnd:]
}
