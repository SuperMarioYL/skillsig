package scope

import (
	"fmt"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
)

// DiffSkills loads the skillsig manifest from each of two skill directories
// (an "old" version and a "new" version of the same skill) and returns every
// scope escalation the new version introduced: an added tool, a broader
// fs_write glob, a new network-egress host, or a widened model. The returned
// slice is human-readable and empty when the new version stayed within the old
// version's declared scope.
//
// This is the m3 cross-version drift check exposed for the `diff` subcommand.
// Unlike applyLockDrift (which compares against ~/.skillsig/lock.yaml), diff
// compares two directories the caller names directly, so a re-signed skill that
// quietly broadened its grants is caught even when its signature is valid.
//
// DiffSkills reuses manifest.ParseSkill so it never duplicates parsing logic,
// and delegates the escalation analysis to scopeGrowth (shared with the
// lock-aware path).
func DiffSkills(oldDir, newDir string) ([]string, error) {
	prev, err := loadDeclares(oldDir)
	if err != nil {
		return nil, fmt.Errorf("old: %w", err)
	}
	curr, err := loadDeclares(newDir)
	if err != nil {
		return nil, fmt.Errorf("new: %w", err)
	}
	return scopeGrowth(prev, curr), nil
}

// loadDeclares parses the SKILL.md at dir and returns its declared scope. A
// skill without a skillsig manifest is an error here: diff compares declared
// scope, so there is nothing to compare without a manifest on both sides.
func loadDeclares(dir string) (manifest.Declares, error) {
	s, err := manifest.ParseSkill(dir)
	if err != nil {
		return manifest.Declares{}, err
	}
	if s.Manifest == nil {
		return manifest.Declares{}, fmt.Errorf("%s: no skillsig manifest to diff", dir)
	}
	return s.Manifest.Declares, nil
}
