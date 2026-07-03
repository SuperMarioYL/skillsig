package scope

import (
	"testing"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
)

// TestMatchGlob_ArgumentTokenBoundary pins the v0.5.0 fix on the tools axis:
// a declared "Tool(prefix*)" wildcard covers a candidate only when the extra
// text begins a NEW argument token (a space boundary), mirroring the '/'-path
// and '.'-host boundary discipline in glob.go. Before the fix a raw
// strings.HasPrefix let a look-alike SIBLING command ("git statuscheckout")
// pass under "git status*".
func TestMatchGlob_ArgumentTokenBoundary(t *testing.T) {
	cases := []struct {
		name     string
		declared string
		actual   string
		want     bool
	}{
		// Legit refinements: extra text starts a new token → covered.
		{"flag refinement", "Bash(git status*)", "Bash(git status -s)", true},
		{"long-flag refinement", "Bash(git status*)", "Bash(git status --porcelain)", true},
		{"exact prefix", "Bash(git status*)", "Bash(git status)", true},
		{"prefix ends at space", "Bash(git *)", "Bash(git log)", true},
		{"bare star covers all args", "Bash(*)", "Bash(rm -rf ~/)", true},

		// Look-alike sibling commands: extra text continues the SAME token → NOT covered.
		{"sibling subcommand", "Bash(git status*)", "Bash(git statuscheckout --force)", false},
		{"glued suffix", "Bash(git status*)", "Bash(git statuses)", false},
		{"different tool", "Bash(git status*)", "Read(git status -s)", false},

		// Non-wildcard declared entries fall back to equality.
		{"literal equal", "Bash(git status -s)", "Bash(git status -s)", true},
		{"literal unequal", "Bash(git status -s)", "Bash(git status -v)", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchGlob(tc.declared, tc.actual); got != tc.want {
				t.Fatalf("matchGlob(%q, %q) = %v, want %v", tc.declared, tc.actual, got, tc.want)
			}
		})
	}
}

// TestCovered_LookAlikeToolTokenNotCovered is the same boundary assertion routed
// through the exported covered() predicate the cross-version growth check uses.
func TestCovered_LookAlikeToolTokenNotCovered(t *testing.T) {
	declared := []string{"Bash(git status*)"}
	if !covered(declared, "Bash(git status -s)") {
		t.Errorf("legit refinement 'git status -s' should stay covered under 'git status*'")
	}
	if covered(declared, "Bash(git statuscheckout --force)") {
		t.Errorf("look-alike sibling 'git statuscheckout' must NOT be covered by 'git status*'")
	}
}

// TestScopeGrowth_LookAlikeToolTokenIsGrowth is the end-to-end witness on the
// SHARED cross-version path (scopeGrowth -> addedTools -> covered -> matchGlob),
// used by both `skillsig diff` and lock-aware `verify --ci`. A re-signed skill
// that swaps a declared subcommand for a same-prefix SIBLING command must be
// flagged SCOPE-DRIFTED, while a genuine refinement must not.
func TestScopeGrowth_LookAlikeToolTokenIsGrowth(t *testing.T) {
	prev := manifest.Declares{Tools: []string{"Bash(git status*)"}}

	// Look-alike sibling token → growth.
	curr := manifest.Declares{Tools: []string{"Bash(git status*)", "Bash(git statuscheckout --force)"}}
	if g := scopeGrowth(prev, curr); len(g) == 0 {
		t.Fatalf("look-alike sibling command 'git statuscheckout' should be reported as scope growth, got none")
	}

	// Genuine refinement → no growth (no false positive).
	refine := manifest.Declares{Tools: []string{"Bash(git status*)", "Bash(git status -s)"}}
	if g := scopeGrowth(prev, refine); len(g) != 0 {
		t.Errorf("space-delimited refinement 'git status -s' must not be growth, got %v", g)
	}
}
