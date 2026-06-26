package scope

import (
	"strings"
	"testing"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
)

// TestHostCovered_PrefixConfusion locks the fix for fix-egress-prefix-confusion:
// a declared egress glob must NOT cover a look-alike host that merely shares its
// literal prefix. Before v0.3.0 the raw strings.HasPrefix coverage let
// "api.github.com.attacker.net" hide under "api.github.com*".
func TestHostCovered_PrefixConfusion(t *testing.T) {
	cases := []struct {
		name     string
		declared []string
		host     string
		covered  bool
	}{
		{"exact match", []string{"registry.npmjs.org"}, "registry.npmjs.org", true},
		{"glob covers real subdomain", []string{"api.github.com*"}, "api.github.com", true},
		{"glob covers dotted child", []string{"github.com**"}, "api.github.com", false}, // boundary: prefix "github.com" not at '.' boundary of "api.github.com"
		{"glob covers child under recursive", []string{"github.com.**"}, "github.com.gist", true},
		{"look-alike host NOT covered", []string{"api.github.com*"}, "api.github.com.attacker.net", false},
		{"sibling host NOT covered", []string{"api.github.com*"}, "api.github.com-evil.net", false},
		{"unrelated host NOT covered", []string{"registry.npmjs.org"}, "evil.example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostCovered(tc.declared, tc.host); got != tc.covered {
				t.Fatalf("hostCovered(%v, %q) = %v, want %v", tc.declared, tc.host, got, tc.covered)
			}
		})
	}
}

// TestFSPathCovered_SiblingPrefix locks the fs_write half of
// fix-egress-prefix-confusion: "/workspace/foo*" must not swallow
// "/workspace/foobar-evil" (a sibling sharing the literal prefix).
func TestFSPathCovered_SiblingPrefix(t *testing.T) {
	cases := []struct {
		name     string
		declared []string
		path     string
		covered  bool
	}{
		{"exact", []string{"/workspace/foo"}, "/workspace/foo", true},
		{"single-star direct child", []string{"/workspace/foo/*"}, "/workspace/foo/a", true},
		{"sibling-prefix NOT covered", []string{"/workspace/foo*"}, "/workspace/foobar-evil", false},
		{"recursive covers deep", []string{"/workspace/**"}, "/workspace/a/b/c", true},
		{"unrelated path NOT covered", []string{"/workspace/**"}, "/etc/passwd", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fsPathCovered(tc.declared, tc.path); got != tc.covered {
				t.Fatalf("fsPathCovered(%v, %q) = %v, want %v", tc.declared, tc.path, got, tc.covered)
			}
		})
	}
}

// TestFSPathCovered_SingleVsDoubleStar locks fix-single-vs-double-star-glob:
// a single "*" matches only within one segment, "**" is recursive, and a ".."
// traversal remainder is never covered.
func TestFSPathCovered_SingleVsDoubleStar(t *testing.T) {
	cases := []struct {
		name     string
		declared []string
		path     string
		covered  bool
	}{
		{"single-star covers direct child", []string{"${WORKSPACE}/*"}, "${WORKSPACE}/file", true},
		{"single-star does NOT cover deep path", []string{"${WORKSPACE}/*"}, "${WORKSPACE}/a/b/secret", false},
		{"double-star covers deep path", []string{"${WORKSPACE}/**"}, "${WORKSPACE}/a/b/secret", true},
		{"single-star rejects traversal", []string{"${WORKSPACE}/*"}, "${WORKSPACE}/../etc", false},
		{"double-star rejects traversal", []string{"${WORKSPACE}/**"}, "${WORKSPACE}/../etc", false},
		{"parent itself covered by recursive", []string{"${WORKSPACE}/**"}, "${WORKSPACE}", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fsPathCovered(tc.declared, tc.path); got != tc.covered {
				t.Fatalf("fsPathCovered(%v, %q) = %v, want %v", tc.declared, tc.path, got, tc.covered)
			}
		})
	}
}

// TestScopeGrowth_BoundaryAware is the end-to-end assertion: a re-signed skill
// that broadens egress to a look-alike host, or adds a deeper path under a
// single-"*" fs_write glob, must surface as growth in scopeGrowth — which feeds
// both lock-aware verify (applyLockDrift) and the diff command.
func TestScopeGrowth_BoundaryAware(t *testing.T) {
	prev := manifest.Declares{
		FSWrite:       []string{"${WORKSPACE}/*"},
		NetworkEgress: []string{"api.github.com*"},
	}
	curr := manifest.Declares{
		FSWrite:       []string{"${WORKSPACE}/*", "${WORKSPACE}/a/b/secret"},
		NetworkEgress: []string{"api.github.com*", "api.github.com.attacker.net"},
	}
	grown := scopeGrowth(prev, curr)
	joined := strings.Join(grown, " | ")
	if !strings.Contains(joined, "fs_write+") || !strings.Contains(joined, "${WORKSPACE}/a/b/secret") {
		t.Errorf("expected fs_write deep-path growth to be reported, got: %q", joined)
	}
	if !strings.Contains(joined, "network_egress+") || !strings.Contains(joined, "api.github.com.attacker.net") {
		t.Errorf("expected look-alike egress host to be reported as growth, got: %q", joined)
	}
}

// TestScopeGrowth_RefinementNotGrowth guards against false positives: a genuine
// narrowing (a real subpath under a recursive glob) must NOT be reported.
func TestScopeGrowth_RefinementNotGrowth(t *testing.T) {
	prev := manifest.Declares{FSWrite: []string{"${WORKSPACE}/**"}}
	curr := manifest.Declares{FSWrite: []string{"${WORKSPACE}/**", "${WORKSPACE}/src/main.go"}}
	if grown := scopeGrowth(prev, curr); len(grown) != 0 {
		t.Errorf("a path under an existing recursive glob is a refinement, not growth; got: %v", grown)
	}
}
