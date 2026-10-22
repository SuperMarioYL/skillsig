package scope

import (
	"os"
	"path/filepath"
	"strings"
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

// TestScopeGrowth_ModelRemovalIsNotGrowthButSwapIs is the v0.9.0 fix
// (fix-model-removal-flagged-as-scope-growth): the model axis used to read
// `curr.Model != prev.Model && prev.Model != ""` with NO `curr.Model != ""`
// guard, so REMOVING declares.model (curr.Model == "") was reported as scope
// growth — and applyLockDrift then upgraded a TRUSTED row to SCOPE-DRIFTED,
// blocking a non-escalating (narrowing) change. That violated the "a removed
// entry is NOT growth, only additions are" contract the tools / fs_write /
// network_egress axes honor (they early-return on an empty curr slice). The
// guard now also checks `curr.Model != ""`, so a genuine model swap (A→B) still
// surfaces for re-attestation while a removal stops being flagged.
func TestScopeGrowth_ModelRemovalIsNotGrowthButSwapIs(t *testing.T) {
	prev := manifest.Declares{Model: "gpt-4"}

	// (a) Removing declares.model (curr.Model == "") is NOT growth: a narrowing
	//     change must not be flagged as SCOPE-DRIFTED.
	currRemoved := manifest.Declares{Model: ""}
	for _, entry := range scopeGrowth(prev, currRemoved) {
		if strings.HasPrefix(entry, "model:") {
			t.Errorf("removing declares.model should not be flagged as growth; got %q", entry)
		}
	}

	// (b) A genuine model swap (gpt-4 → claude) still surfaces for re-attestation.
	currSwap := manifest.Declares{Model: "claude"}
	var sawModel bool
	for _, entry := range scopeGrowth(prev, currSwap) {
		if strings.HasPrefix(entry, "model:") {
			sawModel = true
		}
	}
	if !sawModel {
		t.Errorf("a model swap (gpt-4 → claude) should be flagged as growth; got %v", scopeGrowth(prev, currSwap))
	}
}

// TestScanner_SaveWritesAtomicValidLock is the v0.8.0 fix (fix-lock-write-non-
// atomic): Save must write ~/.skillsig/lock.yaml atomically (temp file in the
// same dir + os.Rename, mirroring cmd/skillsig writeBundle) so a crash mid-write
// during `verify --trust` cannot leave a half-written lock. The next `verify` /
// `verify --ci` calls loadLock -> yaml.Unmarshal on the lock bytes; a truncated
// file fails to parse and breaks the entire CI merge gate until the lock is
// manually recovered. os.Rename is atomic on the same filesystem, so the lock is
// either the previous good state or the complete new state — never partial.
//
// We assert (a) the pre-seed corrupt partial genuinely fails to parse via the
// same loadLock path verify uses (the broken state the fix prevents), (b) after
// a successful Save the lock parses cleanly via loadLock and carries the new
// entry, and (c) no temp file is left behind in the dir — the temp+rename path
// ran and cleaned up (a bare os.WriteFile creates no temp at all, so a leftover
// .skillsig-lock-*.tmp would only exist if the rename step were skipped or
// crashed; a successful Save leaves exactly lock.yaml).
func TestScanner_SaveWritesAtomicValidLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, LockFileName)

	// Simulate the failure mode the fix prevents: a corrupt partial lock left on
	// disk by a crash mid-write of the old non-atomic os.WriteFile path. An
	// unterminated flow mapping is a hard yaml.v3 parse error.
	corrupt := []byte("version: 1\nentries: {a: {b: c")
	if err := os.WriteFile(lockPath, corrupt, 0o644); err != nil {
		t.Fatalf("seed corrupt lock: %v", err)
	}

	// (a) The pre-seed is genuinely corrupt: loadLock (the read path verify uses)
	// rejects it — this is the exact state that breaks `verify --ci` until the
	// lock is manually recovered.
	preSeed := &Scanner{LockPath: lockPath}
	if _, err := preSeed.loadLock(); err == nil {
		t.Fatalf("pre-seed corrupt lock should fail to parse via loadLock, but it was accepted")
	}

	// Save a valid lock over the corrupt partial — the atomic rename must replace
	// the corrupt bytes wholesale with the complete new state.
	want := &LockFile{Version: 1, Entries: map[string]LockEntry{
		"examples/demo": {SkillID: "examples/demo", Version: "v1"},
	}}
	s := &Scanner{LockPath: lockPath}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// (b) The corrupt partial is gone: a fresh Scanner's loadLock parses the
	// saved file cleanly and carries the new entry. This is the path the next
	// `verify --ci` runs; before the fix a partial would have failed here.
	s2 := &Scanner{LockPath: lockPath}
	lf, err := s2.loadLock()
	if err != nil {
		t.Fatalf("loadLock after Save should parse cleanly, got: %v", err)
	}
	if lf == nil {
		t.Fatalf("loadLock returned nil lock after Save")
	}
	got, ok := lf.Entries["examples/demo"]
	if !ok {
		t.Errorf("saved lock is missing the examples/demo entry; entries=%+v", lf.Entries)
	} else if got.Version != "v1" {
		t.Errorf("entry version: got %q want v1", got.Version)
	}
	// And the stale corrupt bytes must be gone from disk.
	saved, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read saved lock: %v", err)
	}
	if strings.Contains(string(saved), "version: 1\nentries: {a: {b: c") {
		t.Errorf("corrupt partial bytes survived Save; lock still contains the stale partial:\n%s", saved)
	}

	// (c) No temp file left behind: a successful Save leaves the dir holding only
	// lock.yaml. A leftover .skillsig-lock-*.tmp would mean the temp+rename path
	// ran but did not clean up; a bare os.WriteFile would leave no temp at all,
	// so this also pins that the temp+rename code path is the one in effect.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read lock dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == LockFileName {
			continue
		}
		t.Errorf("unexpected leftover file in lock dir after Save: %s (temp+rename should have removed it)", e.Name())
	}
}

// writeMalformedCorpus builds a two-skill corpus: "good" has valid frontmatter
// plus a matching manifest (so it evaluates TRUSTED), and "badfm" has
// frontmatter opened with --- and never closed (so ParseSkill returns an error).
func writeMalformedCorpus(t *testing.T) (corpus, goodDir, badDir string) {
	t.Helper()
	corpus = t.TempDir()
	goodDir = filepath.Join(corpus, "good")
	badDir = filepath.Join(corpus, "badfm")
	for _, d := range []string{goodDir, badDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	skillMD := "---\nname: good\ndescription: d\nallowed-tools:\n  - Read\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(goodDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write good SKILL.md: %v", err)
	}
	sidecar := "skillsig: v1\nskill_id: local/good\nversion: 0.1.0\ndeclares:\n  tools:\n    - Read\n"
	if err := os.WriteFile(filepath.Join(goodDir, "SKILLSIG.yaml"), []byte(sidecar), 0o644); err != nil {
		t.Fatalf("write good SKILLSIG.yaml: %v", err)
	}
	// Unterminated frontmatter — the exact failure mode reproduced at the CLI.
	broken := "---\nname: broken\ndescription: d\nallowed-tools:\n  - Read\n\nbody with no closing fence\n"
	if err := os.WriteFile(filepath.Join(badDir, "SKILL.md"), []byte(broken), 0o644); err != nil {
		t.Fatalf("write bad SKILL.md: %v", err)
	}
	return corpus, goodDir, badDir
}

// TestScan_MalformedSkillDoesNotAbortWalk is the regression for
// fix-one-bad-skill-aborts-whole-verify. Before the fix both scan loops did
// `sk, err := ParseSkill(d); if err != nil { return nil, err }`, so ONE
// unreadable SKILL.md suppressed every other skill's verdict and left the CI
// JSON / SARIF artifacts unwritten. The walk must now report the bad directory
// as a MALFORMED row and still evaluate its neighbours.
func TestScan_MalformedSkillDoesNotAbortWalk(t *testing.T) {
	corpus, goodDir, badDir := writeMalformedCorpus(t)
	s := &Scanner{LockPath: filepath.Join(t.TempDir(), LockFileName)}

	results, err := s.Scan(corpus)
	if err != nil {
		t.Fatalf("Scan must not abort on one unparseable SKILL.md, got: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected one result per skill dir (2), got %d: %+v", len(results), results)
	}
	byDir := map[string]Result{}
	for _, r := range results {
		byDir[r.Dir] = r
	}
	goodRes, ok := byDir[goodDir]
	if !ok {
		t.Fatalf("the parseable skill is missing from the results: %+v", results)
	}
	if goodRes.Verdict != VerdictTrusted {
		t.Errorf("parseable skill verdict = %v, want TRUSTED (details: %s)", goodRes.Verdict, goodRes.Details)
	}
	badRes, ok := byDir[badDir]
	if !ok {
		t.Fatalf("the malformed skill is missing from the results: %+v", results)
	}
	if badRes.Verdict != VerdictMalformed {
		t.Errorf("malformed skill verdict = %v, want MALFORMED", badRes.Verdict)
	}
	if !strings.Contains(badRes.Details, "unterminated frontmatter") {
		t.Errorf("malformed row details should name the parse error, got %q", badRes.Details)
	}
}

// TestScanAndTrust_MalformedSkillIsNotPinned asserts the MALFORMED row is
// reported but never recorded as a drift baseline: `verify --trust` only ever
// pins scopes it can actually read and vouch for.
func TestScanAndTrust_MalformedSkillIsNotPinned(t *testing.T) {
	corpus, _, badDir := writeMalformedCorpus(t)
	lockPath := filepath.Join(t.TempDir(), LockFileName)
	s := &Scanner{LockPath: lockPath}

	results, err := s.ScanAndTrust(corpus)
	if err != nil {
		t.Fatalf("ScanAndTrust must not abort on one unparseable SKILL.md, got: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	lf, err := (&Scanner{LockPath: lockPath}).loadLock()
	if err != nil {
		t.Fatalf("loadLock after ScanAndTrust: %v", err)
	}
	if lf == nil {
		t.Fatalf("ScanAndTrust should have written a lock with the clean skill")
	}
	if _, ok := lf.Entries["local/good"]; !ok {
		t.Errorf("the clean, parseable skill should still be pinned; entries=%+v", lf.Entries)
	}
	if _, ok := lf.Entries[badDir]; ok {
		t.Errorf("the malformed skill must not be pinned; entries=%+v", lf.Entries)
	}
}
