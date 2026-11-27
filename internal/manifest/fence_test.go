package manifest

import (
	"strings"
	"testing"
)

// TestFindSkillsigFence_CommentHeadedBlock is the regression for
// fix-comment-headed-inline-manifest-ignored. A YAML comment above the mapping
// is legal YAML and the block is still the manifest; before the fix the fence
// scanner required the first non-blank line to literally start with "skillsig:",
// so this block was invisible and ParseSkill fell back to a stale sibling
// SKILLSIG.yaml.
func TestFindSkillsigFence_CommentHeadedBlock(t *testing.T) {
	body := []byte("---\nname: demo\n---\n\n# demo\n\n```yaml\n# skillsig manifest for this skill\nskillsig: v1\nskill_id: local/demo\nversion: 2.0.0\n```\n\ntrailing prose\n")

	fence, ok := FindSkillsigFence(body)
	if !ok {
		t.Fatalf("a comment-headed fenced manifest must be found; body:\n%s", body)
	}
	if !strings.HasPrefix(string(fence.Content), "# skillsig manifest") {
		t.Errorf("content should be the block's interior, got %q", fence.Content)
	}
	// Start/End must be the exact byte range of the whole fenced block so init's
	// splice replaces precisely what ParseSkill reads.
	block := string(body[fence.Start:fence.End])
	if !strings.HasPrefix(block, "```yaml\n") || !strings.HasSuffix(block, "```\n") {
		t.Errorf("Start/End do not cover the fenced block; got %q", block)
	}
}

// TestFindSkillsigFence_SkipsLeadingNoise covers the other YAML that may legally
// precede the key: blank lines and a `---` document marker.
func TestFindSkillsigFence_SkipsLeadingNoise(t *testing.T) {
	body := []byte("intro\n\n```\n\n---\n\n# note\nskillsig: v1\nskill_id: local/demo\n```\n")
	if _, ok := FindSkillsigFence(body); !ok {
		t.Fatalf("a block with blank/comment/--- lines above `skillsig:` must be found; body:\n%s", body)
	}
}

// TestFindSkillsigFence_IgnoresNonManifestBlocks pins that detection is about
// the block declaring a skillsig manifest, not about being the first fenced
// block: a shell snippet followed by the manifest resolves to the manifest.
func TestFindSkillsigFence_IgnoresNonManifestBlocks(t *testing.T) {
	body := []byte("run this:\n\n```sh\nskillsig verify .\n```\n\nmanifest:\n\n```yaml\nskillsig: v1\nskill_id: local/demo\n```\n")
	fence, ok := FindSkillsigFence(body)
	if !ok {
		t.Fatal("expected the second (manifest) block to be found")
	}
	if strings.Contains(string(fence.Content), "skillsig verify") {
		t.Errorf("resolved the shell block instead of the manifest block: %q", fence.Content)
	}
	if !strings.Contains(string(fence.Content), "skillsig: v1") {
		t.Errorf("content = %q, want the manifest block", fence.Content)
	}
}

// TestFindSkillsigFence_NoManifest covers a body with no fenced manifest and an
// unterminated block: neither may resolve.
func TestFindSkillsigFence_NoManifest(t *testing.T) {
	cases := map[string]string{
		"none":         "# demo\n\nno fence here\n",
		"other-blocks": "```sh\necho hi\n```\n",
		"unterminated": "```yaml\nskillsig: v1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if fence, ok := FindSkillsigFence([]byte(body)); ok {
				t.Fatalf("unexpected fence %+v in body:\n%s", fence, body)
			}
		})
	}
}
