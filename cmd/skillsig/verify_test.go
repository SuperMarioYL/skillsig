package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
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

// hermeticLock points SKILLSIG_HOME at a fresh tmp dir so verify runs against an
// EMPTY lock baseline — the lock-aware Scanner then yields the same in-version
// verdicts as the pre-lock EvaluateAll, keeping these fixture assertions stable
// regardless of any real ~/.skillsig/lock.yaml on the machine running the tests.
func hermeticLock(t *testing.T) {
	t.Helper()
	t.Setenv("SKILLSIG_HOME", t.TempDir())
}

func TestRunVerify_CIExitsOnDrift(t *testing.T) {
	hermeticLock(t)
	root := testdataRoot(t)
	var buf bytes.Buffer
	err := runVerify(&buf, verifyOpts{path: root, ci: true})
	if !errors.Is(err, ErrCIDrift) {
		t.Fatalf("expected ErrCIDrift, got %v", err)
	}
}

func TestRunVerify_NonCIIsZeroEvenWithDrift(t *testing.T) {
	hermeticLock(t)
	root := testdataRoot(t)
	var buf bytes.Buffer
	if err := runVerify(&buf, verifyOpts{path: root}); err != nil {
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
	hermeticLock(t)
	root := testdataRoot(t)
	out := filepath.Join(t.TempDir(), "skillsig.sarif")
	var buf bytes.Buffer
	if err := runVerify(&buf, verifyOpts{path: root, sarifPath: out}); err != nil {
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
	hermeticLock(t)
	root := testdataRoot(t)
	var buf bytes.Buffer
	if err := runVerify(&buf, verifyOpts{path: root, asJSON: true}); err != nil {
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

// writeSkill writes a minimal signed skill (SKILL.md + SKILLSIG.yaml sidecar)
// into dir/<name> so a test can construct a corpus whose declared scope it
// controls. allowed-tools and declares.tools are kept identical so the skill is
// TRUSTED in-version; the lock-drift test then re-writes declares to broaden it.
func writeSkill(t *testing.T, root, name, skillID string, tools []string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	toolsYAML := func(indent string) string {
		var b strings.Builder
		for _, tl := range tools {
			b.WriteString(indent + "- " + tl + "\n")
		}
		return b.String()
	}
	skillMD := "---\nname: " + name + "\nallowed-tools:\n" + toolsYAML("  ") + "---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	sidecar := "skillsig: v1\nskill_id: " + skillID + "\nversion: v1\ndeclares:\n  tools:\n" + toolsYAML("    ")
	if err := os.WriteFile(filepath.Join(dir, "SKILLSIG.yaml"), []byte(sidecar), 0o644); err != nil {
		t.Fatalf("write SKILLSIG.yaml: %v", err)
	}
	return dir
}

// TestRunVerify_TrustThenLockDriftFailsCI is the headline fix for v0.4.0: the
// lock-aware drift path now runs through `verify` (not only `diff`). After
// `verify --trust` records a TRUSTED skill's scope, re-signing that skill with a
// broadened grant makes a plain `verify` report SCOPE-DRIFTED and `verify --ci`
// exit non-zero — the cross-version jqwik vector caught at the CI gate.
func TestRunVerify_TrustThenLockDriftFailsCI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SKILLSIG_HOME", home)
	corpus := t.TempDir()
	writeSkill(t, corpus, "demo", "examples/demo", []string{"Read", "Bash(git status*)"})

	// 1) Seed the baseline: the skill is TRUSTED and gets recorded into the lock.
	var seed bytes.Buffer
	if err := runVerify(&seed, verifyOpts{path: corpus, trust: true}); err != nil {
		t.Fatalf("trust seed: %v", err)
	}
	if !strings.Contains(seed.String(), "TRUSTED") {
		t.Fatalf("seed run should show TRUSTED; got:\n%s", seed.String())
	}
	lock := filepath.Join(home, "lock.yaml")
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("--trust should have written %s: %v", lock, err)
	}

	// 2) Re-sign the SAME skill_id with a broadened grant (adds a dangerous tool).
	writeSkill(t, corpus, "demo", "examples/demo", []string{"Read", "Bash(git status*)", "Bash(rm -rf ~/)"})

	// 3) A plain (read-only) verify now flags drift vs. the recorded baseline.
	var plain bytes.Buffer
	if err := runVerify(&plain, verifyOpts{path: corpus}); err != nil {
		t.Fatalf("plain verify after drift: %v", err)
	}
	if !strings.Contains(plain.String(), "SCOPE-DRIFTED") {
		t.Errorf("re-signed broadened skill should be SCOPE-DRIFTED vs lock; got:\n%s", plain.String())
	}

	// 4) verify --ci exits non-zero on that lock drift (the CI merge gate).
	var ci bytes.Buffer
	if err := runVerify(&ci, verifyOpts{path: corpus, ci: true}); !errors.Is(err, ErrCIDrift) {
		t.Errorf("verify --ci should fail on lock drift; got err=%v", err)
	}
}

// TestRunVerify_LockAwareWithoutTrustStaysTrusted guards the inverse: with an
// empty lock and no --trust, the same TRUSTED skill stays TRUSTED — the lock
// path must not invent drift where there is no recorded baseline.
func TestRunVerify_LockAwareWithoutTrustStaysTrusted(t *testing.T) {
	t.Setenv("SKILLSIG_HOME", t.TempDir())
	corpus := t.TempDir()
	writeSkill(t, corpus, "demo", "examples/demo", []string{"Read"})
	var buf bytes.Buffer
	if err := runVerify(&buf, verifyOpts{path: corpus, ci: true}); err != nil {
		t.Fatalf("a single TRUSTED skill with no lock baseline must pass --ci; got %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "TRUSTED") {
		t.Errorf("expected TRUSTED; got:\n%s", buf.String())
	}
}

// TestRunVerify_SARIFStdoutIsSingleDocument is the second v0.4.0 fix: `--sarif -`
// must emit ONE valid SARIF document on stdout, not the human table/JSON
// concatenated with the SARIF JSON. The whole stdout must JSON-parse, and the
// human table tokens must be absent.
func TestRunVerify_SARIFStdoutIsSingleDocument(t *testing.T) {
	hermeticLock(t)
	root := testdataRoot(t)
	var buf bytes.Buffer
	if err := runVerify(&buf, verifyOpts{path: root, sarifPath: "-"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.Bytes()
	var got struct {
		Version string `json:"version"`
		Runs    []struct {
			Results []struct {
				RuleID string `json:"ruleId"`
				Level  string `json:"level"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("stdout is not a single valid SARIF document: %v\n%s", err, out)
	}
	if got.Version != "2.1.0" {
		t.Errorf("sarif version = %q, want 2.1.0", got.Version)
	}
	// The human-readable table header must NOT be on stdout in this mode.
	if strings.Contains(string(out), "VERDICT") {
		t.Errorf("table header leaked into --sarif - stdout:\n%s", out)
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

// TestRunVerify_JSONAndSARIFStdoutNotConcatenated nails the exact concat bug:
// `--json --sarif -` previously joined two root JSON objects back-to-back (the
// --json report then the SARIF doc), which no parser can read. Now stdout is a
// single SARIF object — json.Decoder must see exactly one value with no trailing
// data.
func TestRunVerify_JSONAndSARIFStdoutNotConcatenated(t *testing.T) {
	hermeticLock(t)
	root := testdataRoot(t)
	var buf bytes.Buffer
	if err := runVerify(&buf, verifyOpts{path: root, asJSON: true, sarifPath: "-"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	var first map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout did not decode as one JSON object: %v\n%s", err, buf.String())
	}
	// There must be NO second JSON value trailing the first.
	var second map[string]any
	if err := dec.Decode(&second); err == nil {
		t.Errorf("stdout contained a SECOND concatenated JSON object (the old bug):\n%s", buf.String())
	}
	// And the single object must be the SARIF doc (has a "runs" array), not the
	// --json report — SARIF is the sole stdout artifact in this mode.
	if _, ok := first["runs"]; !ok {
		t.Errorf("the single stdout object should be the SARIF doc (with \"runs\"); got keys %v", keysOf(first))
	}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
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
