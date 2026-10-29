package manifest

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetectPackageManager(t *testing.T) {
	cases := []struct {
		name     string
		siblings []string
		field    string
		want     string
	}{
		{"none", []string{"package.json", "README.md"}, "", ""},
		{"npm lock", []string{"package.json", "package-lock.json"}, "", "npm"},
		{"shrinkwrap", []string{"npm-shrinkwrap.json"}, "", "npm"},
		{"yarn lock", []string{"yarn.lock"}, "", "yarn"},
		{"pnpm lock", []string{"pnpm-lock.yaml"}, "", "pnpm"},
		{"bun lockb", []string{"bun.lockb"}, "", "bun"},
		{"bun lock", []string{"bun.lock"}, "", "bun"},
		{"field wins over lock", []string{"package-lock.json"}, "pnpm@9.1.0+sha512.abc", "pnpm"},
		{"field with caps", nil, "Yarn@4.1.0", "yarn"},
		{"unknown field falls to lock", []string{"yarn.lock"}, "deno@1", "yarn"},
		{"paths are reduced to names", []string{"web/pnpm-lock.yaml"}, "", "pnpm"},
		{"pnpm beats npm", []string{"package-lock.json", "pnpm-lock.yaml"}, "", "pnpm"},
		{"workspace file", []string{"pnpm-workspace.yaml"}, "", "pnpm"},
		{"yarnrc", []string{".yarnrc.yml"}, "", "yarn"},
		{"bunfig", []string{"bunfig.toml"}, "", "bun"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectPackageManager(tc.siblings, tc.field); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestApplyDetectsPackageManagerFromField(t *testing.T) {
	files := map[string]string{"package.json": "{\n  \"packageManager\": \"pnpm@9.0.0\"\n}\n"}
	out, skipped, err := Apply(files, []Change{{Name: "semver", Ecosystem: "npm", To: "7.7.2", Method: MethodOverride, File: "package.json"}})
	if err != nil || len(skipped) != 0 {
		t.Fatalf("err=%v skipped=%v", err, skipped)
	}
	if !strings.Contains(out["package.json"], `"pnpm": {`) {
		t.Fatalf("expected pnpm.overrides, got:\n%s", out["package.json"])
	}
}

func TestApplyPackageManagerOnChangeWins(t *testing.T) {
	files := map[string]string{"package.json": "{\n  \"name\": \"a\"\n}\n", "yarn.lock": ""}
	out, _, err := Apply(files, []Change{{Name: "semver", To: "7.7.2", Method: MethodOverride, File: "package.json", PackageManager: "npm"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["package.json"], `"overrides"`) || strings.Contains(out["package.json"], "resolutions") {
		t.Fatalf("got:\n%s", out["package.json"])
	}
}

func TestApplyValidation(t *testing.T) {
	cases := map[string]Change{
		"no name":   {To: "1", Method: MethodDirect, File: "package.json"},
		"no to":     {Name: "a", Method: MethodDirect, File: "package.json"},
		"no file":   {Name: "a", To: "1", Method: MethodDirect},
		"no method": {Name: "a", To: "1", File: "package.json"},
		"bad":       {Name: "a", To: "1", Method: "sideways", File: "package.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Apply(map[string]string{"package.json": "{}"}, []Change{c}); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestApplyKeepsUnchangedFilesAndComposesChanges(t *testing.T) {
	files := map[string]string{
		"package.json": "{\n  \"dependencies\": {\n    \"a\": \"^1.0.0\"\n  }\n}\n",
		"other.txt":    "untouched",
	}
	out, skipped, err := Apply(files, []Change{
		{Name: "a", To: "1.1.0", Method: MethodDirect, File: "package.json"},
		{Name: "a", To: "1.2.0", Method: MethodDirect, File: "package.json"},
	})
	if err != nil || len(skipped) != 0 {
		t.Fatalf("err=%v skipped=%v", err, skipped)
	}
	if out["other.txt"] != "untouched" || !strings.Contains(out["package.json"], `"a": "^1.2.0"`) {
		t.Fatalf("got %v", out)
	}
	if !strings.Contains(files["package.json"], "^1.0.0") {
		t.Fatal("input map was modified")
	}
}

func TestEditManifestTextWindowsPath(t *testing.T) {
	next, ok := EditManifestText(`sub\Cargo.TOML`, "[dependencies]\nserde = \"1.0\"\n", "serde", "1.0.200")
	if !ok || !strings.Contains(next, `serde = "1.0.200"`) {
		t.Fatalf("got %q %v", next, ok)
	}
	if _, ok := EditManifestText("README.md", "serde", "serde", "1"); ok {
		t.Fatal("README has no writer")
	}
}

func TestSetJSONEntryCRLF(t *testing.T) {
	in := "{\r\n  \"name\": \"a\"\r\n}\r\n"
	got, ok := setJSONEntry(in, []string{"overrides"}, "semver", "7.7.2")
	want := "{\r\n  \"name\": \"a\",\r\n  \"overrides\": {\r\n    \"semver\": \"7.7.2\"\r\n  }\r\n}\r\n"
	if !ok || got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSetJSONEntryRejectsBadDocuments(t *testing.T) {
	for name, in := range map[string]string{
		"array":      "[]",
		"empty":      "",
		"truncated":  `{"a": "b"`,
		"no colon":   `{"a" "b"}`,
		"overrides0": `{"overrides": 1}`,
		"overridesA": `{"overrides": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := setJSONEntry(in, []string{"overrides"}, "semver", "7.7.2")
			if ok || got != in {
				t.Fatalf("expected a refusal that leaves the text alone, got %q %v", got, ok)
			}
		})
	}
}

func TestSetJSONEntryIgnoresBracesInStrings(t *testing.T) {
	in := "{\n  \"description\": \"a } tricky { string \\\" with quotes\",\n  \"overrides\": {\n    \"a}b\": \"1.0.0\"\n  }\n}\n"
	got, ok := setJSONEntry(in, []string{"overrides"}, "semver", "7.7.2")
	want := "{\n  \"description\": \"a } tricky { string \\\" with quotes\",\n  \"overrides\": {\n    \"a}b\": \"1.0.0\",\n    \"semver\": \"7.7.2\"\n  }\n}\n"
	if !ok || got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSetGoModRequireCRLF(t *testing.T) {
	in := "module x\r\n\r\ngo 1.24\r\n\r\nrequire (\r\n\ta.com/b v1.0.0 // indirect\r\n)\r\n"
	got, ok := setGoModRequire(in, "c.com/d", "1.2.3")
	want := "module x\r\n\r\ngo 1.24\r\n\r\nrequire (\r\n\ta.com/b v1.0.0 // indirect\r\n\tc.com/d v1.2.3 // indirect\r\n)\r\n"
	if !ok || got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	got, ok = setGoModRequire(in, "a.com/b", "v1.1.0")
	if !ok || !strings.Contains(got, "\ta.com/b v1.1.0 // indirect\r\n") {
		t.Fatalf("got %q", got)
	}
}

func TestSetGoModRequireLeavesReplaceAlone(t *testing.T) {
	in := "module x\n\nrequire a.com/b v1.0.0\n\nreplace (\n\ta.com/c v1.0.0 => ./c\n)\n"
	got, ok := setGoModRequire(in, "a.com/c", "1.5.0")
	if !ok {
		t.Fatal("expected success")
	}
	if !strings.Contains(got, "\ta.com/c v1.0.0 => ./c") || !strings.Contains(got, "require a.com/c v1.5.0 // indirect") {
		t.Fatalf("got %q", got)
	}
}

func TestSetUvOverrideKeepsOtherTables(t *testing.T) {
	in := "[tool.uv.sources]\nfoo = { path = \"../foo\" }\n"
	got, ok := setUvOverride(in, "urllib3", "2.2.2")
	want := in + "\n[tool.uv]\noverride-dependencies = [\"urllib3==2.2.2\"]\n"
	if !ok || got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSetUvOverrideBracketInsideString(t *testing.T) {
	in := "[tool.uv]\noverride-dependencies = [\"requests[security]==2.0\"]\n"
	got, ok := setUvOverride(in, "urllib3", "2.2.2")
	want := "[tool.uv]\noverride-dependencies = [\"requests[security]==2.0\", \"urllib3==2.2.2\"]\n"
	if !ok || got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSetUvOverrideEmptyArray(t *testing.T) {
	got, ok := setUvOverride("[tool.uv]\noverride-dependencies = []\n", "urllib3", "2.2.2")
	if !ok || got != "[tool.uv]\noverride-dependencies = [\"urllib3==2.2.2\"]\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIsUvProject(t *testing.T) {
	if isUvProject("[tool.poetry]\n", []string{"pyproject.toml", "poetry.lock"}) {
		t.Fatal("poetry is not uv")
	}
	if !isUvProject("[tool.uv]\n", nil) || !isUvProject("[[tool.uv.index]]\n", nil) || !isUvProject("[tool.uv.sources]\n", nil) {
		t.Fatal("tool.uv tables mark a uv project")
	}
	if !isUvProject("", []string{"uv.lock"}) {
		t.Fatal("uv.lock marks a uv project")
	}
}

func TestLockCommand(t *testing.T) {
	cases := []struct {
		name, manifest, eco, pm string
		argv                    []string
		lock                    string
		ok                      bool
	}{
		{"npm default", "package.json", "npm", "", []string{"npm", "install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"}, "package-lock.json", true},
		{"npm explicit", "app/package.json", "npm", "npm", []string{"npm", "install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"}, "app/package-lock.json", true},
		{"pnpm", "package.json", "npm", "pnpm", []string{"pnpm", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", true},
		{"bun", "package.json", "npm", "bun", []string{"bun", "install", "--lockfile-only", "--ignore-scripts"}, "bun.lock", true},
		{"yarn berry", "package.json", "npm", "yarn", []string{"yarn", "install", "--mode=update-lockfile"}, "yarn.lock", true},
		{"yarn berry field", "package.json", "npm", "yarn@4.1.0", []string{"yarn", "install", "--mode=update-lockfile"}, "yarn.lock", true},
		{"yarn classic", "package.json", "npm", "yarn@1.22.22", []string{"yarn", "install", "--ignore-scripts"}, "yarn.lock", true},
		{"go", "svc/go.mod", "golang", "", []string{"go", "mod", "tidy"}, "svc/go.sum", true},
		{"cargo", "Cargo.toml", "cargo", "", []string{"cargo", "update", "--workspace"}, "Cargo.lock", true},
		{"composer", "composer.json", "composer", "", []string{"composer", "update", "--lock", "--no-scripts", "--no-plugins", "--no-install", "--no-interaction"}, "composer.lock", true},
		{"bundler", "Gemfile", "rubygems", "", []string{"bundle", "lock"}, "Gemfile.lock", true},
		{"uv", "pyproject.toml", "pypi", "uv", []string{"uv", "lock"}, "uv.lock", true},
		{"poetry", "pyproject.toml", "pypi", "poetry", []string{"poetry", "lock"}, "poetry.lock", true},
		{"pip has no lock", "requirements.txt", "pypi", "pip", nil, "", false},
		{"maven has no lock", "pom.xml", "maven", "", nil, "", false},
		{"unknown", "x", "swift", "", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argv, lock, ok := LockCommand(tc.manifest, tc.eco, tc.pm)
			if ok != tc.ok || lock != tc.lock || !reflect.DeepEqual(argv, tc.argv) {
				t.Fatalf("got %v %q %v", argv, lock, ok)
			}
		})
	}
}

func TestLockCommandsDisableScripts(t *testing.T) {
	for _, pm := range []string{"npm", "pnpm", "bun", "yarn@1.22.0"} {
		argv, _, _ := LockCommand("package.json", "npm", pm)
		if !contains(argv, "--ignore-scripts") {
			t.Errorf("%s: %v does not disable scripts", pm, argv)
		}
	}
	if env := LockEnv("yarn@4.1.0"); !contains(env, "YARN_ENABLE_SCRIPTS=false") {
		t.Errorf("yarn berry env %v", env)
	}
	argv, _, _ := LockCommand("composer.json", "composer", "")
	if !contains(argv, "--no-scripts") || !contains(argv, "--no-plugins") {
		t.Errorf("composer %v", argv)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestOverrideSection(t *testing.T) {
	cases := []struct{ file, pm, want string }{
		{"package.json", "npm", "package.json > overrides"},
		{"web/package.json", "bun", "package.json > overrides"},
		{"package.json", "pnpm", "package.json > pnpm.overrides"},
		{"package.json", "yarn", "package.json > resolutions"},
		{"go.mod", "", "go.mod > require (indirect)"},
		{"pyproject.toml", "uv", "pyproject.toml > tool.uv.override-dependencies"},
		{"pom.xml", "", "pom.xml > dependencyManagement"},
		{"Cargo.toml", "", "Cargo.lock"},
		{"requirements.txt", "", "requirements.txt"},
	}
	for _, tc := range cases {
		if got := OverrideSection(tc.file, tc.pm); got != tc.want {
			t.Errorf("%s/%s: got %q want %q", tc.file, tc.pm, got, tc.want)
		}
	}
}

func TestSatisfies(t *testing.T) {
	cases := []struct {
		version, constraint string
		want                bool
	}{
		{"1.2.3", "^1.0.0", true},
		{"2.0.0", "^1.0.0", false},
		{"v1.2.3", "~1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"1.2.3", ">=1.0.0 <2.0.0", true},
		{"1.2.3", "*", true},
		{"1.2.3", "", true},
		{"", "^1", false},
		{"1.2.3", "1.2.3", true},
		{"1.2.3", "not a range", false},
		{"1.2.0-beta", ">=1.0.0", false}, // prereleases are excluded by design
		{"1.2.3", "≥1.0.0", false},       // a unicode operator is not a constraint
		{"garbage", "^1.0.0", false},
	}
	for _, tc := range cases {
		if got := Satisfies(tc.version, tc.constraint); got != tc.want {
			t.Errorf("Satisfies(%q, %q) = %v want %v", tc.version, tc.constraint, got, tc.want)
		}
	}
}

func TestBestInRange(t *testing.T) {
	cands := []string{"1.0.0", "v1.4.2", "1.10.0", "2.0.0", "not-semver", "1.9.9"}
	if got := BestInRange(cands, "^1.0.0"); got != "1.10.0" {
		t.Errorf("got %q", got)
	}
	if got := BestInRange(cands, "~1.4.0"); got != "1.4.2" {
		t.Errorf("got %q", got)
	}
	if got := BestInRange(cands, "^3.0.0"); got != "" {
		t.Errorf("got %q", got)
	}
	if got := BestInRange(nil, "^1.0.0"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestPreserveRangeOperator(t *testing.T) {
	for in, want := range map[string]string{
		"^1.0.0": "^2.0.0", "~1.0.0": "~2.0.0", "~> 1.0": "~>2.0.0", ">=1.0": ">=2.0.0",
		"<=1.0": "<=2.0.0", ">1.0": ">2.0.0", "<1.0": "<2.0.0", "1.0.0": "2.0.0", "": "2.0.0",
	} {
		if got := preserveRangeOperator(in, "2.0.0"); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestApplyKeepsAnNpmGitDependencyAGitDependency(t *testing.T) {
	pkg := `{
  "dependencies": {
    "left-pad": "github:acme/left-pad#v1.3.0",
    "mirror": "git+https://github.com/acme/mirror.git#semver:^2.0.0",
    "short": "acme/short#v0.1.0",
    "react": "^18.2.0"
  }
}`
	changes := []Change{
		{Name: "left-pad", Ecosystem: "npm", To: "v1.3.1", Method: MethodDirect, File: "package.json"},
		{Name: "mirror", Ecosystem: "npm", To: "v2.1.0", Method: MethodDirect, File: "package.json"},
		{Name: "short", Ecosystem: "npm", To: "v0.2.0", Method: MethodDirect, File: "package.json"},
		{Name: "react", Ecosystem: "npm", To: "18.3.1", Method: MethodDirect, File: "package.json"},
	}
	out, skipped, err := Apply(map[string]string{"package.json": pkg}, changes)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("err %v skipped %v", err, skipped)
	}
	got := out["package.json"]
	for _, want := range []string{
		`"left-pad": "github:acme/left-pad#v1.3.1"`,
		`"mirror": "git+https://github.com/acme/mirror.git#semver:^2.1.0"`,
		`"short": "acme/short#v0.2.0"`,
		`"react": "^18.3.1"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
}
