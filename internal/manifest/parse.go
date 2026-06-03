package manifest

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSkill reads SKILL.md at dir, decodes its YAML frontmatter, and looks for
// a skillsig manifest in two places (in this order):
//
//  1. A fenced YAML code block inside the SKILL.md body whose content begins
//     with "skillsig:".
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

	if mf, ok := extractSidecar(body); ok {
		var m Manifest
		if err := yaml.Unmarshal(mf, &m); err != nil {
			return nil, fmt.Errorf("%s: skillsig sidecar yaml: %w", skillPath, err)
		}
		s.Manifest = &m
		s.ManifestSrc = "sidecar"
		return s, nil
	}

	siblingPath := filepath.Join(dir, "SKILLSIG.yaml")
	siblingRaw, err := os.ReadFile(siblingPath)
	if err == nil {
		var m Manifest
		if err := yaml.Unmarshal(siblingRaw, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", siblingPath, err)
		}
		s.Manifest = &m
		s.ManifestSrc = "SKILLSIG.yaml"
		return s, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", siblingPath, err)
	}

	return s, nil
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
		fmLines   []string
		body      strings.Builder
		seenOpen  bool
		inFM      bool
		closedFM  bool
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

// extractSidecar walks the SKILL.md body looking for a fenced code block
// (```yaml, ```yml, or plain ```) whose content begins with "skillsig:".
// The fence info string is ignored other than as a delimiter; what matters is
// the first non-blank key inside the block.
func extractSidecar(body []byte) ([]byte, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inFence := false
	var fence strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		trim := strings.TrimSpace(line)
		if !inFence {
			if strings.HasPrefix(trim, "```") {
				inFence = true
				fence.Reset()
			}
			continue
		}
		if strings.HasPrefix(trim, "```") {
			candidate := fence.String()
			head := strings.TrimLeft(candidate, " \t\n\r")
			if strings.HasPrefix(head, "skillsig:") {
				return []byte(candidate), true
			}
			inFence = false
			continue
		}
		fence.WriteString(line)
		fence.WriteByte('\n')
	}
	return nil, false
}
