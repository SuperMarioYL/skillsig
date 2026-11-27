package manifest

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSkill reads SKILL.md at dir, decodes its YAML frontmatter, and looks for
// a skillsig manifest in two places (in this order):
//
//  1. A fenced YAML code block inside the SKILL.md body that declares a
//     top-level `skillsig:` key (see FindSkillsigFence).
//  2. A sibling SKILLSIG.yaml file in the same directory.
//
// A missing skillsig manifest is NOT an error — the caller (verify) reports
// that case as UNSIGNED.
func ParseSkill(dir string) (*Skill, error) {
	skillPath := filepath.Join(dir, "SKILL.md")
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", skillPath, err)
	}

	fm, body, err := splitFrontmatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", skillPath, err)
	}

	var front SkillFrontmatter
	if err := yaml.Unmarshal(fm, &front); err != nil {
		return nil, fmt.Errorf("%s: invalid frontmatter yaml: %w", skillPath, err)
	}

	s := &Skill{Dir: dir, Frontmatter: front}

	if fence, ok := FindSkillsigFence(body); ok {
		m, err := parseManifest(fence.Content)
		if err != nil {
			return nil, fmt.Errorf("%s: skillsig sidecar yaml: %w", skillPath, err)
		}
		s.Manifest = m
		s.ManifestSrc = "sidecar"
		return s, nil
	}

	siblingPath := filepath.Join(dir, "SKILLSIG.yaml")
	siblingRaw, err := os.ReadFile(siblingPath)
	if err == nil {
		m, err := parseManifest(siblingRaw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", siblingPath, err)
		}
		s.Manifest = m
		s.ManifestSrc = "SKILLSIG.yaml"
		return s, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", siblingPath, err)
	}

	return s, nil
}

// parseManifest decodes one skillsig manifest document. It is the SINGLE owner
// of that decode, used for both the inline fenced block and SKILLSIG.yaml, and
// it refuses keys the v1 schema does not define.
//
// Unknown fields are a hard error (yaml Decoder.KnownFields) because dropping
// them silently makes a manifest narrower than its author wrote: a misspelled
// `declares:` or `network_egress:` decoded to an empty block, `sign` attested
// that empty block, and verify read TRUSTED. A manifest this build cannot fully
// interpret must never be vouched for or signed — the same rule
// SchemaSupported enforces for the `skillsig:` version. The SKILL.md frontmatter
// decode above deliberately stays lenient: those keys are host-owned, not part
// of the skillsig schema.
//
// An empty document (an empty SKILLSIG.yaml) is not a decode error; it yields a
// zero Manifest, which SchemaSupported then reports as MALFORMED.
func parseManifest(raw []byte) (*Manifest, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return &Manifest{}, nil
		}
		return nil, err
	}
	return &m, nil
}

// FindSkillDirs walks root and returns every directory that contains a
// SKILL.md. The returned list is stable (lexicographic) so report output is
// reproducible.
func FindSkillDirs(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != "SKILL.md" {
			return nil
		}
		out = append(out, filepath.Dir(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// splitFrontmatter returns (frontmatterYAML, body) for a markdown file whose
// first non-blank line is "---". If no frontmatter is present, an empty
// frontmatter slice and the full input as body are returned (no error) — that
// lets ParseSkill produce a usable Skill with a zero-value Frontmatter rather
// than rejecting the file outright.
func splitFrontmatter(raw []byte) ([]byte, []byte, error) {
	s := string(raw)
	trim := strings.TrimLeft(s, " \t\n\r")
	if !strings.HasPrefix(trim, "---") {
		return nil, raw, nil
	}
	// Re-scan against the original to preserve byte offsets.
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var (
		fmLines  []string
		body     strings.Builder
		seenOpen bool
		inFM     bool
		closedFM bool
	)
	for scanner.Scan() {
		line := scanner.Text()
		if !seenOpen {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if strings.TrimSpace(line) == "---" {
				seenOpen = true
				inFM = true
				continue
			}
			// Not a frontmatter doc after all.
			return nil, raw, nil
		}
		if inFM {
			if strings.TrimSpace(line) == "---" {
				inFM = false
				closedFM = true
				continue
			}
			fmLines = append(fmLines, line)
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if !closedFM {
		return nil, nil, errors.New("unterminated frontmatter (missing closing '---')")
	}
	return []byte(strings.Join(fmLines, "\n")), []byte(body.String()), nil
}
