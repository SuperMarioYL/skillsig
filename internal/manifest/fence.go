package manifest

import "strings"

// Fence locates one fenced code block in a SKILL.md body that declares itself a
// skillsig manifest.
//
// Start/End are byte offsets into the body: Start is the opening fence line,
// End is just past the closing fence line (including its trailing newline when
// present), so body[Start:End] is exactly the block. Content is the block's
// content without the two fence lines, ready for yaml decoding.
type Fence struct {
	Start   int
	End     int
	Content []byte
}

// FindSkillsigFence returns the first fenced code block in body that is a
// skillsig manifest, mirroring how a host-authored SKILL.md embeds one.
//
// This is the SINGLE owner of "where is the manifest block". Two callers depend
// on it and must never disagree: ParseSkill reads Content, and `skillsig init`
// splices Start/End to replace or remove the block. When those two carried
// their own copies of the scan they could drift — and a writer that replaces a
// different block than the reader sees is exactly the stale-manifest bug of
// v0.8.0/v0.11.0.
//
// A block is a skillsig manifest when its first content line starts with
// "skillsig:". Leading blank lines, YAML comments and a `---` document marker
// are skipped first: they are legal YAML above the same mapping, so requiring
// the FIRST line to be the key made a commented block invisible — ParseSkill
// then fell through to a stale sibling SKILLSIG.yaml and vouched for the wrong
// manifest.
//
// Detection stays textual (it does not require the block to decode) so a block
// that names `skillsig:` but has broken YAML is still found and reported as
// MALFORMED instead of silently falling back to a sibling file.
func FindSkillsigFence(body []byte) (Fence, bool) {
	lines := strings.SplitAfter(string(body), "\n")
	off := 0
	inFence := false
	var (
		start   int
		content strings.Builder
	)
	for _, ln := range lines {
		next := off + len(ln)
		trimmed := strings.TrimSpace(ln)
		switch {
		case !inFence:
			if strings.HasPrefix(trimmed, "```") {
				inFence = true
				start = off
				content.Reset()
			}
		case strings.HasPrefix(trimmed, "```"):
			// Closing fence: keep looking unless this block is the manifest.
			if declaresSkillsig(content.String()) {
				return Fence{Start: start, End: next, Content: []byte(content.String())}, true
			}
			inFence = false
		default:
			content.WriteString(ln)
		}
		off = next
	}
	return Fence{}, false
}

// declaresSkillsig reports whether a fenced block starts with the manifest's
// top-level `skillsig:` key once the YAML that may legally precede it — blank
// lines, comments, and a `---` document marker — is skipped.
func declaresSkillsig(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" || strings.HasPrefix(line, "#") {
			continue
		}
		return strings.HasPrefix(line, "skillsig:")
	}
	return false
}
