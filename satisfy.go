package manifest

import (
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

// unicodeOps maps unicode comparison operators (seen in upstream advisory
// text) to their ASCII equivalents.
var unicodeOps = strings.NewReplacer(
	"≥", ">=",
	"≤", "<=",
	"≠", "!=",
	"＜", "<",
	"＞", ">",
	"＝", "=",
	"．", ".",
)

// normalizeVersion prepares a raw version for parsing: unicode operators become
// ASCII, whitespace is collapsed, and a leading "npm:" or a "v" before a digit
// is dropped. It matches the CLI's versions.Normalize.
func normalizeVersion(s string) string {
	s = unicodeOps.Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimPrefix(s, "npm:")
	if len(s) >= 2 && (s[0] == 'v' || s[0] == 'V') && s[1] >= '0' && s[1] <= '9' {
		s = s[1:]
	}
	return s
}

// Satisfies reports whether version satisfies an npm-style constraint
// (^, ~, ranges) via Masterminds/semver.
//
// Masterminds constraints exclude prerelease versions unless the constraint
// itself contains one (">= 1.0.0" does not match "1.2.0-beta"). That posture is
// right here because Satisfies only selects fix versions (an upgrade target);
// never use it to decide whether a version is affected by an advisory.
func Satisfies(version, constraint string) bool {
	version = normalizeVersion(version)
	constraint = strings.TrimSpace(constraint)
	if version == "" || constraint == "" || constraint == "*" {
		return constraint == "" || constraint == "*"
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return strings.TrimPrefix(version, "v") == strings.TrimPrefix(constraint, "v")
	}
	v, err := semver.NewVersion(version)
	if err != nil {
		return false
	}
	return c.Check(v)
}

// BestInRange returns the highest of candidates that satisfies constraint, or
// "" when none does. Candidates that are not semver are ignored. The result is
// normalized (no leading "v").
func BestInRange(candidates []string, constraint string) string {
	for _, v := range sortableVersions(candidates) {
		if Satisfies(v, constraint) {
			return v
		}
	}
	return ""
}

// sortableVersions normalizes the semver parseable inputs and sorts them,
// newest first.
func sortableVersions(input []string) []string {
	out := make([]string, 0, len(input))
	for _, raw := range input {
		v := normalizeVersion(raw)
		if _, err := semver.NewVersion(v); err == nil {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		vi, _ := semver.NewVersion(out[i])
		vj, _ := semver.NewVersion(out[j])
		return vj.LessThan(vi)
	})
	return out
}
