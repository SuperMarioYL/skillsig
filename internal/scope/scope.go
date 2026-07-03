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
//
// The wildcard prefix is ARGUMENT-TOKEN boundary-aware (v0.5.0), mirroring the
// path/host boundary discipline in glob.go so the tools axis agrees with fs_write
// and network_egress on what "broader" means. A declared "Bash(git status*)"
// covers a refinement whose extra text begins a NEW argument token —
// "Bash(git status -s)" / "Bash(git status --porcelain)" — but does NOT raw-cover
// a look-alike SIBLING command "Bash(git statuscheckout --force)", where "checkout"
// continues the same token. Before this fix a raw strings.HasPrefix swallowed the
// look-alike, so a re-signed skill that swapped a declared subcommand for a
// same-prefix different subcommand escaped SCOPE-DRIFTED on both `diff` and the
// lock-aware `verify --ci` (the shared scopeGrowth->addedTools->covered->matchGlob
// path) — the exact cross-version escalation skillsig exists to catch.
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
	return argPrefixCovers(prefix, aArg)
}

// argSep is the argument-token separator in the Claude Code Bash grant grammar:
// a space between shell tokens. "git status*" covers "git status <anything as a
// new token>" but not "git statusX" (X continues the same token).
const argSep = ' '

// argPrefixCovers reports whether a wildcard prefix covers a candidate argument
// at an argument-token boundary:
//   - an empty prefix ("*" alone) covers everything for this tool;
//   - a prefix already ending at the separator covers any candidate that starts
//     with it (the boundary is on the prefix side);
//   - otherwise the candidate must equal the prefix, or its next char after the
//     prefix must be the separator — so the extra text begins a NEW token.
//
// This is the tools-axis analogue of boundaryPrefix() in glob.go (which uses '/'
// for fs paths and '.' for hosts); here the separator is a space.
func argPrefixCovers(prefix, candidate string) bool {
	if prefix == "" {
		return true // "Tool(*)" — declared everything on this tool
	}
	if !strings.HasPrefix(candidate, prefix) {
		return false
	}
	if candidate == prefix {
		return true
	}
	// The prefix already ends at a token boundary (declared "git status *").
	if prefix[len(prefix)-1] == argSep {
		return true
	}
	// Otherwise the char right after the prefix must start a new token.
	return candidate[len(prefix)] == argSep
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
