package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
	"github.com/SuperMarioYL/skillsig/internal/report"
	"github.com/SuperMarioYL/skillsig/internal/scope"
)

// testdataRoot returns the absolute path of testdata/skills (two dirs up from
// this package: cmd/skillsig → repo root → testdata/skills).
func testdataRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "skills"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	return p
}

func TestParseFixtures_FrontmatterAndManifest(t *testing.T) {
	root := testdataRoot(t)
	cases := []struct {
		name            string
		dir             string
		wantTool        string
		wantManifest    bool
		wantSkillID     string
		wantManifestSrc string
	}{
		{"safe-skill", "safe-skill", "Read", true, "skillsig-examples/safe-skill", "sidecar"},
		{"jqwik-style-bad", "jqwik-style-bad", "Bash(rm -rf ~/.claude/*)", true, "skillsig-examples/jqwik-style-bad", "sidecar"},
		{"scope-mismatch (unsigned)", "scope-mismatch", "WebFetch", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := manifest.ParseSkill(filepath.Join(root, tc.dir))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !containsString(s.Frontmatter.AllowedTools, tc.wantTool) {
				t.Errorf("allowed-tools missing %q: got %v", tc.wantTool, s.Frontmatter.AllowedTools)
			}
			if tc.wantManifest {
				if s.Manifest == nil {
					t.Fatalf("expected manifest, got nil")
				}
				if s.Manifest.SkillID != tc.wantSkillID {
					t.Errorf("skill_id: got %q want %q", s.Manifest.SkillID, tc.wantSkillID)
				}
				if s.Manifest.Skillsig != manifest.SkillsigVersion {
					t.Errorf("schema version: got %q want %q", s.Manifest.Skillsig, manifest.SkillsigVersion)
				}
				if s.ManifestSrc != tc.wantManifestSrc {
					t.Errorf("manifest source: got %q want %q", s.ManifestSrc, tc.wantManifestSrc)
				}
			} else if s.Manifest != nil {
				t.Errorf("expected no manifest, got %+v", s.Manifest)
			}
		})
	}
}

func TestEvaluate_VerdictsAcrossFixtures(t *testing.T) {
	root := testdataRoot(t)
	dirs, err := manifest.FindSkillDirs(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(dirs) != 3 {
		t.Fatalf("expected 3 skill dirs, got %d (%v)", len(dirs), dirs)
	}
	skills := make([]*manifest.Skill, 0, len(dirs))
	for _, d := range dirs {
		s, err := manifest.ParseSkill(d)
		if err != nil {
			t.Fatalf("parse %s: %v", d, err)
		}
		skills = append(skills, s)
	}
	results := scope.EvaluateAll(skills)

	got := map[string]scope.Verdict{}
	for _, r := range results {
		got[shortName(r.Dir)] = r.Verdict
	}
	want := map[string]scope.Verdict{
		"safe-skill":      scope.VerdictTrusted,
		"jqwik-style-bad": scope.VerdictScopeDrifted,
		"scope-mismatch":  scope.VerdictUnsigned,
	}
	for name, w := range want {
		if g, ok := got[name]; !ok || g != w {
			t.Errorf("%s: got %v want %v", name, g, w)
		}
	}
}

// TestEvaluate_DriftPinpointsTheJqwikGrant nails the headline claim from the
// MVP plan: the scanner — without any Sigstore signature yet — flags the
// scope-escalated grant as the reason for SCOPE-DRIFTED.
func TestEvaluate_DriftPinpointsTheJqwikGrant(t *testing.T) {
	root := testdataRoot(t)
	s, err := manifest.ParseSkill(filepath.Join(root, "jqwik-style-bad"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := scope.Evaluate(s)
	if r.Verdict != scope.VerdictScopeDrifted {
		t.Fatalf("verdict: got %v want %v", r.Verdict, scope.VerdictScopeDrifted)
	}
	if !strings.Contains(r.Details, "rm -rf") {
		t.Errorf("details should name the offending grant; got %q", r.Details)
	}
}

func TestReport_Render_PlainTextContainsAllVerdicts(t *testing.T) {
	root := testdataRoot(t)
	dirs, _ := manifest.FindSkillDirs(root)
	var skills []*manifest.Skill
	for _, d := range dirs {
		s, _ := manifest.ParseSkill(d)
		skills = append(skills, s)
	}
	results := scope.EvaluateAll(skills)

	var buf bytes.Buffer
	if err := report.Render(&buf, results, false); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, must := range []string{"SKILL", "VERDICT", "TRUSTED", "UNSIGNED", "SCOPE-DRIFTED"} {
		if !strings.Contains(out, must) {
			t.Errorf("plain-text table missing %q\n---\n%s", must, out)
		}
	}
	if !strings.Contains(report.Summary(results), "1 trusted") ||
		!strings.Contains(report.Summary(results), "1 unsigned") ||
		!strings.Contains(report.Summary(results), "1 scope-drifted") {
		t.Errorf("summary off: %s", report.Summary(results))
	}
}

func TestRunVerify_CIExitsOnDrift(t *testing.T) {
	root := testdataRoot(t)
	var buf bytes.Buffer
	err := runVerify(&buf, root, true, false, false, "")
	if !errors.Is(err, ErrCIDrift) {
		t.Fatalf("expected ErrCIDrift, got %v", err)
	}
}

func TestRunVerify_NonCIIsZeroEvenWithDrift(t *testing.T) {
	root := testdataRoot(t)
	var buf bytes.Buffer
	if err := runVerify(&buf, root, false, false, false, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "SCOPE-DRIFTED") {
		t.Errorf("output should still report drift; got:\n%s", buf.String())
	}
}

// TestRunVerify_SARIFFile checks the m4 --sarif mode: verify writes a valid
// SARIF 2.1.0 log to the given path, with a result per non-TRUSTED skill and a
// level=error for the scope-drifted fixture.
func TestRunVerify_SARIFFile(t *testing.T) {
	root := testdataRoot(t)
	out := filepath.Join(t.TempDir(), "skillsig.sarif")
	var buf bytes.Buffer
	if err := runVerify(&buf, root, false, false, false, out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read sarif: %v", err)
	}
	var got struct {
		Version string `json:"version"`
		Runs    []struct {
			Results []struct {
				RuleID string `json:"ruleId"`
				Level  string `json:"level"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("sarif is not valid JSON: %v\n%s", err, raw)
	}
	if got.Version != "2.1.0" {
		t.Errorf("sarif version = %q, want 2.1.0", got.Version)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("want exactly 1 run, got %d", len(got.Runs))
	}
	var sawDriftError bool
	for _, r := range got.Runs[0].Results {
		if r.RuleID == "skillsig/scope-drifted" && r.Level == "error" {
			sawDriftError = true
		}
	}
	if !sawDriftError {
		t.Errorf("expected a scope-drifted result at level=error; results=%+v", got.Runs[0].Results)
	}
}

// TestRunVerify_JSONOutput checks the new --json mode: the output parses as a
// single JSON object whose summary tallies match the three fixtures and whose
// top-level drift flag is true (one unsigned + one scope-drifted row).
func TestRunVerify_JSONOutput(t *testing.T) {
	root := testdataRoot(t)
	var buf bytes.Buffer
	if err := runVerify(&buf, root, false, false, true, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got struct {
		Skills []struct {
			SkillID string `json:"skill_id"`
			Verdict string `json:"verdict"`
		} `json:"skills"`
		Summary struct {
			Total      int `json:"total"`
			Trusted    int `json:"trusted"`
			Unsigned   int `json:"unsigned"`
			ScopeDrift int `json:"scope_drifted"`
		} `json:"summary"`
		Drift bool `json:"drift"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got.Summary.Total != 3 || got.Summary.Trusted != 1 || got.Summary.Unsigned != 1 || got.Summary.ScopeDrift != 1 {
		t.Errorf("summary tally off: %+v", got.Summary)
	}
	if !got.Drift {
		t.Errorf("drift flag should be true with an unsigned + scope-drifted row")
	}
	if len(got.Skills) != 3 {
		t.Errorf("expected 3 skill rows, got %d", len(got.Skills))
	}
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func shortName(dir string) string {
	return filepath.Base(dir)
}
