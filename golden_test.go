package manifest

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Golden cases live in testdata/golden/*.txt. A case file is a list of
// sections, each opened by a line "-- NAME --":
//
//	-- changes --        a JSON array of Change
//	-- in:PATH --        an input file
//	-- out:PATH --       the expected final text of a file that changes
//	-- skipped --        the expected Skipped list as JSON (absent when none)
//
// A section whose header ends " nonl --" has no trailing newline. Text before
// the first header is a comment. Run `go test -update` to rewrite the out and
// skipped sections from the current output, then read the diff.
var update = flag.Bool("update", false, "rewrite golden files")

var goldenHeader = regexp.MustCompile(`^-- (changes|skipped|in:\S+|out:\S+)( nonl)? --$`)

type goldenSection struct {
	name string
	body string
}

type goldenCase struct {
	comment  string
	sections []goldenSection
}

func parseGolden(t *testing.T, text string) goldenCase {
	t.Helper()
	var gc goldenCase
	lines := strings.SplitAfter(text, "\n")
	var cur *goldenSection
	var nonl bool
	flush := func() {
		if cur == nil {
			return
		}
		if nonl {
			cur.body = strings.TrimSuffix(cur.body, "\n")
		}
		gc.sections = append(gc.sections, *cur)
		cur = nil
	}
	for _, l := range lines {
		if m := goldenHeader.FindStringSubmatch(strings.TrimSuffix(l, "\n")); m != nil {
			flush()
			cur = &goldenSection{name: m[1]}
			nonl = m[2] != ""
			continue
		}
		if cur == nil {
			gc.comment += l
			continue
		}
		cur.body += l
	}
	flush()
	return gc
}

func writeSection(b *strings.Builder, name, body string) {
	b.WriteString("-- " + name)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString(" nonl")
		body += "\n"
	}
	b.WriteString(" --\n" + body)
}

func TestGolden(t *testing.T) {
	paths, err := filepath.Glob("testdata/golden/*.txt")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no golden cases: %v", err)
	}
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".txt")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			gc := parseGolden(t, string(raw))
			var changes []Change
			in := map[string]string{}
			var inOrder []string
			want := map[string]string{}
			var wantSkipped string
			for _, s := range gc.sections {
				switch {
				case s.name == "changes":
					if err := json.Unmarshal([]byte(s.body), &changes); err != nil {
						t.Fatalf("changes: %v", err)
					}
				case s.name == "skipped":
					wantSkipped = s.body
				case strings.HasPrefix(s.name, "in:"):
					in[strings.TrimPrefix(s.name, "in:")] = s.body
					inOrder = append(inOrder, strings.TrimPrefix(s.name, "in:"))
				case strings.HasPrefix(s.name, "out:"):
					want[strings.TrimPrefix(s.name, "out:")] = s.body
				}
			}
			snapshot := map[string]string{}
			for k, v := range in {
				snapshot[k] = v
			}

			got, skipped, err := Apply(in, changes)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range snapshot {
				if in[k] != v {
					t.Fatalf("Apply modified its input for %s", k)
				}
			}
			changed := map[string]string{}
			for k, v := range got {
				if v != in[k] {
					changed[k] = v
				}
			}
			var skippedJSON string
			if len(skipped) > 0 {
				b, _ := json.MarshalIndent(skipped, "", "  ")
				skippedJSON = string(b) + "\n"
			}

			if *update {
				var b strings.Builder
				b.WriteString(gc.comment)
				for _, s := range gc.sections {
					if s.name == "changes" {
						writeSection(&b, s.name, s.body)
					}
				}
				for _, k := range inOrder {
					writeSection(&b, "in:"+k, in[k])
				}
				keys := make([]string, 0, len(changed))
				for k := range changed {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					writeSection(&b, "out:"+k, changed[k])
				}
				if skippedJSON != "" {
					writeSection(&b, "skipped", skippedJSON)
				}
				if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			for k, v := range changed {
				w, ok := want[k]
				if !ok {
					t.Errorf("%s changed but the case has no out section:\n%s", k, v)
					continue
				}
				if v != w {
					t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", k, v, w)
				}
			}
			for k := range want {
				if _, ok := changed[k]; !ok {
					t.Errorf("%s expected to change but did not", k)
				}
			}
			if skippedJSON != wantSkipped {
				t.Errorf("skipped mismatch\n--- got ---\n%s\n--- want ---\n%s", skippedJSON, wantSkipped)
			}

			// Writing the result again must change nothing (not for the legacy
			// writers, some of which append on every call).
			if strings.HasPrefix(name, "legacy-") {
				return
			}
			again, _, err := Apply(got, changes)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range got {
				if again[k] != v {
					t.Errorf("not idempotent for %s\n--- first ---\n%s\n--- second ---\n%s", k, v, again[k])
				}
			}
		})
	}
}
