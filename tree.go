package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNoTree is returned by ParseTree for a lockfile that carries no dependency
// edges this module can read (go.sum, requirements, an npm lockfile of version
// 1, and every format that has no reader yet). Callers fall back to the direct
// packages only.
var ErrNoTree = errors.New("lockfile has no readable dependency edges")

// TreeNode is one resolved package in a lockfile.
type TreeNode struct {
	// ID is unique inside a Tree: the lock path for npm
	// ("node_modules/a/node_modules/b"), "name version" for Cargo.
	ID      string
	Name    string
	Version string
	// Dev is set when the lockfile marks the package as reachable only from
	// development dependencies.
	Dev bool
}

// TreeEdge is one resolved requirement. From is "" for a requirement of the
// project itself (a workspace member counts as the project).
type TreeEdge struct {
	From string
	To   string
	// Range is the declared range the parent wrote, empty when the lockfile
	// does not carry it (Cargo.lock does not).
	Range string
}

// Requirer is one package that requires another, as Parents reports it.
type Requirer struct {
	Name    string
	Version string
	Range   string
	// Root is true when the project itself is the requirer.
	Root bool
}

// Tree is the resolved dependency tree read from a lockfile. It is read only
// and built once by ParseTree.
type Tree struct {
	// Nodes by ID.
	Nodes map[string]*TreeNode
	// Edges in a stable order.
	Edges []TreeEdge

	out    map[string][]int // From to indexes into Edges
	in     map[string][]int // To to indexes into Edges
	byName map[string][]string
}

func newTree() *Tree {
	return &Tree{
		Nodes:  map[string]*TreeNode{},
		out:    map[string][]int{},
		in:     map[string][]int{},
		byName: map[string][]string{},
	}
}

func (t *Tree) addNode(n *TreeNode) {
	t.Nodes[n.ID] = n
	t.byName[n.Name] = append(t.byName[n.Name], n.ID)
}

func (t *Tree) addEdge(from, to, rng string) {
	for _, i := range t.out[from] {
		if t.Edges[i].To == to {
			if t.Edges[i].Range == "" {
				t.Edges[i].Range = rng
			}
			return
		}
	}
	t.Edges = append(t.Edges, TreeEdge{From: from, To: to, Range: rng})
	i := len(t.Edges) - 1
	t.out[from] = append(t.out[from], i)
	t.in[to] = append(t.in[to], i)
}

// finish puts the lookups in a stable order.
func (t *Tree) finish() {
	for name, ids := range t.byName {
		sort.Strings(ids)
		t.byName[name] = ids
	}
	for from, idx := range t.out {
		sort.Slice(idx, func(a, b int) bool { return t.Edges[idx[a]].To < t.Edges[idx[b]].To })
		t.out[from] = idx
	}
}

// ParseTree reads the dependency edges of a lockfile. file is only used for its
// base name. Supported: package-lock.json and npm-shrinkwrap.json (lockfile
// versions 2 and 3) and Cargo.lock. Anything else returns ErrNoTree.
func ParseTree(file, content string) (*Tree, error) {
	switch baseName(file) {
	case "package-lock.json", "npm-shrinkwrap.json":
		return parseNpmLockTree(content)
	case "cargo.lock":
		return parseCargoLockTree(content)
	}
	return nil, ErrNoTree
}

// Names lists every package name in the tree, sorted.
func (t *Tree) Names() []string {
	out := make([]string, 0, len(t.byName))
	for n := range t.byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Versions lists the installed versions of a package, in node ID order.
func (t *Tree) Versions(name string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range t.byName[name] {
		v := t.Nodes[id].Version
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// RootRequires returns the version of the package the project itself requires
// by that name, and false when the project does not require it.
func (t *Tree) RootRequires(name string) (string, bool) {
	for _, i := range t.out[""] {
		n := t.Nodes[t.Edges[i].To]
		if n != nil && n.Name == name {
			return n.Version, true
		}
	}
	return "", false
}

// Parents lists the packages that require name at version, one entry per
// distinct requirer, the project itself first and then by name.
func (t *Tree) Parents(name, version string) []Requirer {
	var out []Requirer
	seen := map[string]bool{}
	for _, id := range t.byName[name] {
		if t.Nodes[id].Version != version {
			continue
		}
		for _, i := range t.in[id] {
			e := t.Edges[i]
			r := Requirer{Range: e.Range}
			if e.From == "" {
				r.Root = true
			} else if p := t.Nodes[e.From]; p != nil {
				r.Name, r.Version = p.Name, p.Version
			} else {
				continue
			}
			key := fmt.Sprintf("%v|%s|%s", r.Root, r.Name, r.Version)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Root != out[b].Root {
			return out[a].Root
		}
		if out[a].Name != out[b].Name {
			return out[a].Name < out[b].Name
		}
		return out[a].Version < out[b].Version
	})
	return out
}

// Path is a route from the project to one package.
type Path struct {
	// Steps are "name version" strings from the direct dependency to the
	// package itself, both included.
	Steps []string
	// Names are the package names of Steps.
	Names []string
}

// PathTo returns the shortest route from the project to name at version, or a
// zero Path when it is not reachable. When several routes have the same length
// the one through the alphabetically first IDs wins, so the answer is stable.
func (t *Tree) PathTo(name, version string) Path {
	targets := map[string]bool{}
	for _, id := range t.byName[name] {
		if t.Nodes[id].Version == version {
			targets[id] = true
		}
	}
	if len(targets) == 0 {
		return Path{}
	}
	prev := map[string]string{}
	seen := map[string]bool{"": true}
	queue := []string{""}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, i := range t.out[cur] {
			to := t.Edges[i].To
			if seen[to] {
				continue
			}
			seen[to] = true
			prev[to] = cur
			if targets[to] {
				return t.buildPath(prev, to)
			}
			queue = append(queue, to)
		}
	}
	return Path{}
}

func (t *Tree) buildPath(prev map[string]string, end string) Path {
	var ids []string
	for cur := end; cur != ""; cur = prev[cur] {
		ids = append(ids, cur)
	}
	var p Path
	for i := len(ids) - 1; i >= 0; i-- {
		n := t.Nodes[ids[i]]
		p.Steps = append(p.Steps, n.Name+" "+n.Version)
		p.Names = append(p.Names, n.Name)
	}
	return p
}

// DirectAncestors lists the direct dependencies of the project that reach name
// at version, nearest first (shortest route, then name). A package that is
// itself direct lists itself. Each entry carries the version of the direct
// dependency.
func (t *Tree) DirectAncestors(name, version string) []Requirer {
	targets := map[string]bool{}
	for _, id := range t.byName[name] {
		if t.Nodes[id].Version == version {
			targets[id] = true
		}
	}
	if len(targets) == 0 {
		return nil
	}
	// Breadth first search backwards from the targets. dist is the route length
	// to the nearest target.
	dist := map[string]int{}
	queue := []string{}
	for id := range targets {
		dist[id] = 0
		queue = append(queue, id)
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, i := range t.in[cur] {
			from := t.Edges[i].From
			if from == "" {
				continue
			}
			if _, ok := dist[from]; !ok {
				dist[from] = dist[cur] + 1
				queue = append(queue, from)
			}
		}
	}
	type cand struct {
		r Requirer
		d int
	}
	var cands []cand
	seen := map[string]bool{}
	for _, i := range t.out[""] {
		id := t.Edges[i].To
		d, ok := dist[id]
		if !ok {
			continue
		}
		n := t.Nodes[id]
		key := n.Name + "@" + n.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		cands = append(cands, cand{Requirer{Name: n.Name, Version: n.Version, Range: t.Edges[i].Range}, d})
	}
	sort.SliceStable(cands, func(a, b int) bool {
		if cands[a].d != cands[b].d {
			return cands[a].d < cands[b].d
		}
		return cands[a].r.Name < cands[b].r.Name
	})
	out := make([]Requirer, len(cands))
	for i, c := range cands {
		out[i] = c.r
	}
	return out
}

// ── npm package-lock.json, lockfile versions 2 and 3 ─────────────────────

type npmLockEntry struct {
	Version              string            `json:"version"`
	Link                 bool              `json:"link"`
	Dev                  bool              `json:"dev"`
	DevOptional          bool              `json:"devOptional"`
	Name                 string            `json:"name"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

func parseNpmLockTree(content string) (*Tree, error) {
	var lock struct {
		LockfileVersion int                     `json:"lockfileVersion"`
		Packages        map[string]npmLockEntry `json:"packages"`
	}
	if err := json.Unmarshal([]byte(content), &lock); err != nil {
		return nil, fmt.Errorf("invalid package-lock.json: %w", err)
	}
	if len(lock.Packages) == 0 {
		return nil, fmt.Errorf("%w: lockfileVersion %d has no packages table", ErrNoTree, lock.LockfileVersion)
	}

	t := newTree()
	// installed maps a lock path to its node. Links and workspace members are
	// not installed packages: a member stands in for the project.
	local := map[string]bool{"": true}
	for p, e := range lock.Packages {
		if p == "" {
			continue
		}
		if !strings.Contains(p, "node_modules/") {
			local[p] = true
			continue
		}
		if e.Link || e.Version == "" {
			continue
		}
		t.addNode(&TreeNode{ID: p, Name: npmNameOfPath(p), Version: e.Version, Dev: e.Dev || e.DevOptional})
	}

	paths := make([]string, 0, len(lock.Packages))
	for p := range lock.Packages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		e := lock.Packages[p]
		from := p
		switch {
		case local[p]:
			from = ""
		case t.Nodes[p] == nil:
			continue
		}
		for _, deps := range []map[string]string{e.Dependencies, e.DevDependencies, e.OptionalDependencies, e.PeerDependencies} {
			for name, rng := range deps {
				if to, ok := npmResolve(t, p, name); ok {
					t.addEdge(from, to, rng)
				}
			}
		}
	}
	t.finish()
	return t, nil
}

// npmNameOfPath is the package name of a lock path: the segment after the last
// "node_modules/".
func npmNameOfPath(p string) string {
	if i := strings.LastIndex(p, "node_modules/"); i >= 0 {
		return p[i+len("node_modules/"):]
	}
	return p
}

// npmResolve finds the node a package at lock path from gets for name, by the
// rule node itself uses: look in its own node_modules, then in each enclosing
// one up to the root.
func npmResolve(t *Tree, from, name string) (string, bool) {
	dir := from
	for {
		var cand string
		if dir == "" {
			cand = "node_modules/" + name
		} else {
			cand = dir + "/node_modules/" + name
		}
		if _, ok := t.Nodes[cand]; ok {
			return cand, true
		}
		if dir == "" {
			return "", false
		}
		dir = npmEnclosing(dir)
	}
}

// npmEnclosing is the directory whose node_modules holds the package at dir:
// "node_modules/a/node_modules/b" is held by "node_modules/a"; a top level
// install and a workspace member are held by the root.
func npmEnclosing(dir string) string {
	i := strings.LastIndex(dir, "/node_modules/")
	if i < 0 {
		return ""
	}
	return dir[:i]
}

// ── Cargo.lock ───────────────────────────────────────────────────────────

// parseCargoLockTree reads Cargo.lock's [[package]] tables. A package with no
// source is a workspace member or a path crate; it stands in for the project,
// so its requirements are requirements of the root. Cargo.lock does not record
// the version ranges the crates asked for, so every edge has an empty Range.
func parseCargoLockTree(content string) (*Tree, error) {
	type pkg struct {
		name, version, source string
		deps                  []string
	}
	var pkgs []*pkg
	var cur *pkg
	inDeps := false
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch {
		case line == "[[package]]":
			cur = &pkg{}
			pkgs = append(pkgs, cur)
			inDeps = false
			continue
		case cur == nil || line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "["):
			// [metadata], [[patch.unused]] and friends end the package list.
			cur = nil
			inDeps = false
			continue
		}
		if inDeps {
			if strings.HasPrefix(line, "]") {
				inDeps = false
				continue
			}
			if s, ok := cargoQuoted(line); ok {
				cur.deps = append(cur.deps, s)
			}
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "name":
			cur.name, _ = cargoQuoted(val)
		case "version":
			cur.version, _ = cargoQuoted(val)
		case "source":
			cur.source, _ = cargoQuoted(val)
		case "dependencies":
			if strings.HasPrefix(val, "[") {
				rest := strings.TrimSpace(strings.TrimPrefix(val, "["))
				if strings.HasSuffix(rest, "]") {
					for _, part := range strings.Split(strings.TrimSuffix(rest, "]"), ",") {
						if s, ok := cargoQuoted(strings.TrimSpace(part)); ok {
							cur.deps = append(cur.deps, s)
						}
					}
				} else {
					inDeps = true
					if s, ok := cargoQuoted(strings.TrimSuffix(rest, ",")); ok {
						cur.deps = append(cur.deps, s)
					}
				}
			}
		}
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("%w: Cargo.lock has no packages", ErrNoTree)
	}

	t := newTree()
	byName := map[string][]*pkg{}
	for _, p := range pkgs {
		if p.name == "" || p.version == "" {
			continue
		}
		byName[p.name] = append(byName[p.name], p)
		if p.source != "" {
			t.addNode(&TreeNode{ID: p.name + " " + p.version, Name: p.name, Version: p.version})
		}
	}
	for _, p := range pkgs {
		if p.name == "" || p.version == "" {
			continue
		}
		from := ""
		if p.source != "" {
			from = p.name + " " + p.version
		}
		for _, d := range p.deps {
			// "name", "name version" or "name version (source)".
			fields := strings.Fields(d)
			if len(fields) == 0 {
				continue
			}
			name := fields[0]
			var to *pkg
			if len(fields) >= 2 {
				for _, c := range byName[name] {
					if c.version == fields[1] {
						to = c
						break
					}
				}
			} else if cs := byName[name]; len(cs) == 1 {
				to = cs[0]
			}
			if to == nil || to.source == "" {
				continue
			}
			t.addEdge(from, to.name+" "+to.version, "")
		}
	}
	t.finish()
	return t, nil
}

// cargoQuoted unquotes a TOML basic string that is the whole of s, ignoring a
// trailing comma.
func cargoQuoted(s string) (string, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ","))
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	return strings.ReplaceAll(s[1:len(s)-1], `\"`, `"`), true
}
