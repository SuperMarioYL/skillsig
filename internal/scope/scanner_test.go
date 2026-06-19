package scope

import (
	"testing"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
)

// TestScopeGrowth_TightenedToolWildcardIsNotGrowth pins the bug fix on the
// tools axis: narrowing "Bash(git status*)" to a concrete subcommand that the
// wildcard already covered is a refinement, not an escalation.
func TestScopeGrowth_TightenedToolWildcardIsNotGrowth(t *testing.T) {
	prev := manifest.Declares{Tools: []string{"Read", "Bash(git status*)"}}
	curr := manifest.Declares{Tools: []string{"Read", "Bash(git status -s)"}}
	if g := scopeGrowth(prev, curr); len(g) != 0 {
		t.Errorf("refinement under an existing wildcard reported as growth: %v", g)
	}
}

// TestScopeGrowth_NewToolOutsideWildcardIsGrowth confirms the fix did not go
// too far: a grant the previous wildcard does NOT cover is still real growth.
func TestScopeGrowth_NewToolOutsideWildcardIsGrowth(t *testing.T) {
	prev := manifest.Declares{Tools: []string{"Bash(git status*)"}}
	curr := manifest.Declares{Tools: []string{"Bash(git status*)", "Bash(rm -rf ~/)"}}
	g := scopeGrowth(prev, curr)
	if len(g) == 0 {
		t.Fatalf("a grant outside the prior wildcard should be growth, got none")
	}
}

// TestScopeGrowth_PathUnderExistingGlobIsNotGrowth pins the fix on the
// fs_write / network_egress axes: a concrete path that falls under a previously
// declared glob ("${WORKSPACE}/**") is narrowing, not escalation.
func TestScopeGrowth_PathUnderExistingGlobIsNotGrowth(t *testing.T) {
	prev := manifest.Declares{
		FSWrite:       []string{"${WORKSPACE}/**"},
		NetworkEgress: []string{"api.github.com"},
	}
	curr := manifest.Declares{
		FSWrite:       []string{"${WORKSPACE}/build/out.txt"},
		NetworkEgress: []string{"api.github.com"},
	}
	if g := scopeGrowth(prev, curr); len(g) != 0 {
		t.Errorf("path under an existing glob reported as growth: %v", g)
	}
}

// TestScopeGrowth_NewPathOutsideGlobIsGrowth confirms a write outside every
// declared glob (escaping the workspace to $HOME) is still flagged.
func TestScopeGrowth_NewPathOutsideGlobIsGrowth(t *testing.T) {
	prev := manifest.Declares{FSWrite: []string{"${WORKSPACE}/**"}}
	curr := manifest.Declares{FSWrite: []string{"${WORKSPACE}/**", "~/.claude/config"}}
	g := scopeGrowth(prev, curr)
	if len(g) == 0 {
		t.Fatalf("a write outside the workspace glob should be growth, got none")
	}
}
