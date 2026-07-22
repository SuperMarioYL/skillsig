package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
)

// inlineSkillMD builds a SKILL.md that already embeds an inline skillsig
// manifest whose declared model is staleModel and whose declared tools are
// exactly the given list. It mirrors the shape appendInline itself produces, so
// re-running `skillsig init --inline --force` on it exercises the replace path.
func inlineSkillMD(staleModel string, tools []string) string {
	var b strings.Builder
	b.WriteString("---\nname: demo\ndescription: init fixture\nallowed-tools:\n")
	for _, t := range tools {
		b.WriteString("  - " + t + "\n")
	}
	b.WriteString("---\n\n# demo\n\n## skillsig manifest\n\n```yaml\n")
	b.WriteString("skillsig: v1\nskill_id: local/demo\nversion: 0.1.0\ndeclares:\n  model: " + staleModel + "\n  tools:\n")
	for _, t := range tools {
		b.WriteString("    - " + t + "\n")
	}
	b.WriteString("  fs_write:\n    - \"${WORKSPACE}/**\"\n  network_egress: []\nattestation:\n  sigstore_bundle: ./skillsig.bundle\n```\n")
	return b.String()
}

// TestRunInit_InlineForceReplacesStaleBlock is the v0.8.0 fix
// (fix-init-inline-force-leaves-stale-block): re-running `skillsig init --inline
// --force` on a SKILL.md that already embeds an inline skillsig manifest must
// REPLACE the existing fenced block in place so only one skillsig block remains
// and extractSidecar (internal/manifest/parse.go) returns the regenerated
// manifest. Before the fix, --force skipped the "already contains" guard and
// then unconditionally APPENDED a second fenced block; extractSidecar returns
// the FIRST match, so the stale manifest stayed in effect and the freshly
// regenerated one was dead text — the doc comment claimed it "rewrites it in
// place" but it did not.
func TestRunInit_InlineForceReplacesStaleBlock(t *testing.T) {
	dir := t.TempDir()
	// Seed a SKILL.md whose embedded manifest declares a STALE model we can
	// distinguish from the regenerated one. allowed-tools == declares.tools so the
	// skill is otherwise scope-consistent.
	staleModel := "claude-opus-4-7"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(inlineSkillMD(staleModel, []string{"Read"})), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Re-run init --inline --force, overriding the model to a value distinct from
	// the stale one so a parsed manifest that still shows the stale model proves
	// extractSidecar read the OLD (first) block.
	newModel := "claude-opus-4-99"
	var buf bytes.Buffer
	if err := runInit(&buf, dir, initOptions{
		inline:  true,
		force:   true,
		skillID: "local/demo",
		version: "0.1.0",
		model:   newModel,
	}); err != nil {
		t.Fatalf("runInit --inline --force: %v", err)
	}

	// (a) extractSidecar must return the REGENERATED manifest, not the stale
	// first block. Under the bug (two blocks), ParseSkill reads the first = stale
	// model, so this assertion fails.
	sk, err := manifest.ParseSkill(dir)
	if err != nil {
		t.Fatalf("parse after re-init: %v", err)
	}
	if sk.Manifest == nil {
		t.Fatalf("expected a manifest after re-init, got nil (manifest src=%q)", sk.ManifestSrc)
	}
	if sk.Manifest.Declares.Model != newModel {
		t.Errorf("extractSidecar returned the stale block: model=%q want %q (the regenerated one); manifest src=%q",
			sk.Manifest.Declares.Model, newModel, sk.ManifestSrc)
	}
	if sk.Manifest.SkillID != "local/demo" {
		t.Errorf("skill_id after re-init: got %q want local/demo", sk.Manifest.SkillID)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md after re-init: %v", err)
	}
	body := string(raw)

	// (b) Exactly ONE fenced skillsig block may remain. Under the bug the stale
	// block was appended-to (not replaced), leaving two "```yaml" fences. A single
	// fenced block means the stale one was spliced out, not duplicated.
	if got := strings.Count(body, "```yaml"); got != 1 {
		t.Errorf("expected exactly 1 fenced ```yaml block after --force, got %d; SKILL.md:\n%s", got, body)
	}
	if got := strings.Count(body, "skillsig: v1"); got != 1 {
		t.Errorf("expected exactly 1 skillsig block header after --force, got %d; SKILL.md:\n%s", got, body)
	}

	// (c) The stale model must be GONE from the file — the old block was removed,
	// not merely supplemented with a second one. Under the bug the stale block
	// (carrying staleModel) is still present.
	if strings.Contains(body, "model: "+staleModel) {
		t.Errorf("the stale skillsig block (model: %s) survived --force; --force should replace, not append; SKILL.md:\n%s",
			staleModel, body)
	}
	// And the regenerated model IS present.
	if !strings.Contains(body, "model: "+newModel) {
		t.Errorf("regenerated manifest (model: %s) not found in SKILL.md:\n%s", newModel, body)
	}
}

// TestRunInit_InlineNoForceRejectsExistingBlock guards the non-force path: a
// SKILL.md that already has an inline skillsig manifest must be rejected (the
// caller must opt in via --force). runInit's own manifest-present guard fires
// before appendInline is reached, so this pins that the replace-in-place
// behavior stays gated on --force at the CLI layer.
func TestRunInit_InlineNoForceRejectsExistingBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte(inlineSkillMD("claude-opus-4-7", []string{"Read"})), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	var buf bytes.Buffer
	err := runInit(&buf, dir, initOptions{
		inline:  true,
		force:   false,
		skillID: "local/demo",
		version: "0.1.0",
		model:   "claude-opus-4-99",
	})
	if err == nil {
		t.Fatalf("runInit --inline WITHOUT --force on a skill that already has a manifest should error, got nil")
	}
	if !strings.Contains(err.Error(), "force") {
		t.Errorf("error should steer the user toward --force; got: %v", err)
	}
	// The file must be untouched — no second block appended, no stale block replaced.
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if strings.Contains(string(raw), "claude-opus-4-99") {
		t.Errorf("non-force run must not write the regenerated manifest; SKILL.md:\n%s", raw)
	}
}

// TestRunInit_InlineForceAppendsWhenNoBlock guards the no-existing-block path:
// when no fenced skillsig block exists yet, --force (or a plain first run)
// appends one at the end. This confirms the replace path did not break the
// first-run seeding behavior.
func TestRunInit_InlineForceAppendsWhenNoBlock(t *testing.T) {
	dir := t.TempDir()
	skillMD := "---\nname: fresh\ndescription: no manifest yet\nallowed-tools:\n  - Read\n---\n\n# fresh\n\nbody with no sidecar.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	var buf bytes.Buffer
	if err := runInit(&buf, dir, initOptions{
		inline:  true,
		force:   true,
		skillID: "local/fresh",
		version: "0.1.0",
		model:   "claude-opus-4-99",
	}); err != nil {
		t.Fatalf("runInit --inline --force on a block-less skill: %v", err)
	}
	sk, err := manifest.ParseSkill(dir)
	if err != nil {
		t.Fatalf("parse after init: %v", err)
	}
	if sk.Manifest == nil || sk.Manifest.Declares.Model != "claude-opus-4-99" {
		t.Fatalf("expected the freshly appended manifest; got %+v (src=%q)", sk.Manifest, sk.ManifestSrc)
	}
	if sk.ManifestSrc != "sidecar" {
		t.Errorf("manifest source: got %q want sidecar", sk.ManifestSrc)
	}
}
