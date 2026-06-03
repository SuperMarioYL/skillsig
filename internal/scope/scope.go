// Package scope models a skill's declared permission scope and compares it
// against the runtime grants that a host (Claude Code today) actually honors.
// At m1 we have one comparison: SKILL.md `allowed-tools` (the actual grants)
// against the skillsig manifest's `declares.tools` (the allowlist). Anything
// in the actuals not in the allowlist is a drift entry — the jqwik attack
// vector — and surfaces as SCOPE-DRIFTED in the report.
package scope

import (
	"sort"
	"strings"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
)

// Verdict is the per-skill outcome printed by the verify command.
type Verdict string

const (
	VerdictTrusted      Verdict = "TRUSTED"
	VerdictUnsigned     Verdict = "UNSIGNED"
	VerdictScopeDrifted Verdict = "SCOPE-DRIFTED"
)

// Result is what the report renders. Details is a short, human-readable
// summary of WHY the verdict landed where it did (e.g. which tool grant was
// not in the declared allowlist).
type Result struct {
	SkillID string
	Dir     string
	Verdict Verdict
	Details string
}

// Evaluate returns the verdict for one parsed skill against m1 rules:
//
//   - No skillsig manifest → UNSIGNED.
//   - Manifest present, every entry in SKILL.md allowed-tools is covered by
//     declares.tools (literal match OR matched by a declared glob like
//     "Bash(git status*)") → TRUSTED.
//   - Manifest present, one or more allowed-tools entries are NOT covered →
//     SCOPE-DRIFTED.
//
// m2 layers Sigstore identity on top (TRUSTED requires a verified bundle);
// m3 layers ~/.skillsig/lock.yaml on top (drift across versions, not just
// drift inside one version). Both extend Evaluate without changing the
// signature.
func Evaluate(s *manifest.Skill) Result {
	r := Result{
		SkillID: skillID(s),
		Dir:     s.Dir,
	}
	if s.Manifest == nil {
		r.Verdict = VerdictUnsigned
		r.Details = "no skillsig manifest (sidecar or SKILLSIG.yaml)"
		return r
	}
	drift := compareTools(s.Manifest.Declares.Tools, s.Frontmatter.AllowedTools)
	if len(drift) == 0 {
		r.Verdict = VerdictTrusted
		r.Details = manifestSourceNote(s)
		return r
	}
	r.Verdict = VerdictScopeDrifted
	r.Details = "undeclared grant(s): " + strings.Join(drift, ", ")
	return r
}

// EvaluateAll is a small convenience for the verify command.
func EvaluateAll(skills []*manifest.Skill) []Result {
	out := make([]Result, 0, len(skills))
	for _, s := range skills {
		out = append(out, Evaluate(s))
	}
	return out
}

// compareTools returns every actual grant that is NOT covered by any declared
// allowlist entry. A declared entry covers an actual entry when:
//
//   - The strings are equal (case-insensitive on the tool name part), OR
//   - The declared entry uses a trailing `*` wildcard inside parentheses and
//     the actual entry's prefix matches (e.g. "Bash(git status*)" covers
//     "Bash(git status -s)" but NOT "Bash(rm -rf ~/)").
//
// The wildcard semantics deliberately mirror the Claude Code allowed-tools
// grant grammar so authors can copy entries between the two files.
func compareTools(declared, actual []string) []string {
	if len(actual) == 0 {
		return nil
	}
	var drift []string
	for _, a := range actual {
		if !covered(declared, a) {
			drift = append(drift, a)
		}
	}
	sort.Strings(drift)
	return drift
}

func covered(declared []string, actual string) bool {
	a := strings.TrimSpace(actual)
	for _, d := range declared {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if strings.EqualFold(d, a) {
			return true
		}
		if matchGlob(d, a) {
			return true
		}
	}
	return false
}

// matchGlob handles the "Tool(prefix*)" pattern used by Claude Code allowed-
// tools entries. Anything else falls back to plain equality (handled by the
// caller). The wildcard is honored only when both sides share the same outer
// tool name (e.g. "Bash") to prevent a "*" entry from accidentally covering
// every grant.
func matchGlob(declared, actual string) bool {
	dTool, dArg, dOK := splitToolArg(declared)
	aTool, aArg, aOK := splitToolArg(actual)
	if !dOK || !aOK {
		return false
	}
	if !strings.EqualFold(dTool, aTool) {
		return false
	}
	if !strings.HasSuffix(dArg, "*") {
		return strings.EqualFold(dArg, aArg)
	}
	prefix := strings.TrimSuffix(dArg, "*")
	return strings.HasPrefix(aArg, prefix)
}

func splitToolArg(s string) (tool, arg string, ok bool) {
	lp := strings.Index(s, "(")
	rp := strings.LastIndex(s, ")")
	if lp < 0 || rp < 0 || rp <= lp {
		return s, "", false
	}
	return s[:lp], s[lp+1 : rp], true
}

func skillID(s *manifest.Skill) string {
	if s.Manifest != nil && s.Manifest.SkillID != "" {
		return s.Manifest.SkillID
	}
	if s.Frontmatter.Name != "" {
		return s.Frontmatter.Name
	}
	return s.Dir
}

func manifestSourceNote(s *manifest.Skill) string {
	if s.ManifestSrc == "" {
		return "scope matches declared manifest"
	}
	return "scope matches declared manifest (" + s.ManifestSrc + ")"
}
