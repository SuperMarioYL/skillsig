package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillMD builds a SKILL.md with an embedded skillsig manifest whose declared
// tools are exactly the given list. fs_write / network_egress stay fixed so the
// tool list is the only axis the test varies.
func skillMD(tools []string) string {
	var b strings.Builder
	b.WriteString("---\nname: demo-skill\ndescription: diff fixture\nallowed-tools:\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "  - %q\n", t)
	}
	b.WriteString("---\n\n# demo-skill\n\n```yaml\nskillsig: v1\nskill_id: skillsig-examples/demo-skill\nversion: 0.1.0\ndeclares:\n  model: claude-opus-4-7\n  tools:\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "    - %q\n", t)
	}
	b.WriteString("  fs_write:\n    - \"${WORKSPACE}/**\"\n  network_egress: []\n```\n")
	return b.String()
}

// writeSkillDir creates a temp dir containing a SKILL.md declaring the given
// tools and returns the dir path.
func writeSkillDir(t *testing.T, tools []string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD(tools)), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return dir
}

// TestRunDiff_CatchesAddedRmRf is the m3 headline: a re-signed skill that
// quietly added Bash(rm -rf*) FAILS diff (non-zero exit) and names the grant.
func TestRunDiff_CatchesAddedRmRf(t *testing.T) {
	oldDir := writeSkillDir(t, []string{"Read", "Edit"})
	newDir := writeSkillDir(t, []string{"Read", "Edit", "Bash(rm -rf*)"})

	var buf bytes.Buffer
	err := runDiff(&buf, oldDir, newDir)
	if !errors.Is(err, ErrScopeEscalation) {
		t.Fatalf("expected ErrScopeEscalation, got %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "rm -rf") {
		t.Errorf("output should name the offending grant; got:\n%s", out)
	}
	if !strings.Contains(out, "escalation") {
		t.Errorf("output should announce an escalation; got:\n%s", out)
	}
}

// TestRunDiff_UnchangedReportsNoEscalation: identical declared scope means no
// escalation and a clean (nil) error.
func TestRunDiff_UnchangedReportsNoEscalation(t *testing.T) {
	tools := []string{"Read", "Edit", "Bash(git status*)"}
	oldDir := writeSkillDir(t, tools)
	newDir := writeSkillDir(t, tools)

	var buf bytes.Buffer
	if err := runDiff(&buf, oldDir, newDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "no scope escalation") {
		t.Errorf("output should report no escalation; got:\n%s", buf.String())
	}
}
