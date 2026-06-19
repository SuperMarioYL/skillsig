package scope

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
)

// LockFileName is the per-user state file name. The full path lives under
// either the user's home dir or a path the caller pins via Scanner.LockPath
// (tests use this to point at a tmp dir without touching $HOME).
const LockFileName = "lock.yaml"

// LockEntry records the declared scope skillsig last saw TRUSTED for one
// skill_id. The next time verify runs, the scope package compares the current
// manifest's declares against this snapshot — that is the m3 drift check that
// catches a re-signed skill quietly broadening its grants.
type LockEntry struct {
	SkillID  string            `yaml:"skill_id"`
	Version  string            `yaml:"version"`
	Declares manifest.Declares `yaml:"declares"`
	SeenAt   string            `yaml:"seen_at"`
}

// LockFile is the on-disk shape of ~/.skillsig/lock.yaml. The map is keyed by
// skill_id so re-installing the same skill (possibly from a different path on
// disk) still resolves to the same drift baseline.
type LockFile struct {
	Version int                  `yaml:"version"`
	Entries map[string]LockEntry `yaml:"entries"`
}

// Scanner walks a root directory, parses every SKILL.md it finds, evaluates a
// verdict for each, and (optionally) cross-references a lock file so verify
// can flag drift across versions in addition to drift inside one version.
//
// Scanner is constructed with sensible defaults via DefaultScanner. Tests
// override LockPath to a tmp file so unit tests stay hermetic.
type Scanner struct {
	// LockPath points at the per-user lock file. Empty disables lock-aware
	// checks: every skill is evaluated against itself only (the m1 default).
	LockPath string

	mu   sync.Mutex
	lock *LockFile
}

// DefaultScanner returns a Scanner that resolves the lock file from $HOME, or
// from $SKILLSIG_HOME if set (CI integrations and tests use the env var to
// avoid touching real user state). A missing lock file is not an error — the
// scanner treats it as an empty baseline.
func DefaultScanner() *Scanner {
	return &Scanner{LockPath: defaultLockPath()}
}

func defaultLockPath() string {
	if dir := os.Getenv("SKILLSIG_HOME"); dir != "" {
		return filepath.Join(dir, LockFileName)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".skillsig", LockFileName)
}

// Scan walks root, parses every SKILL.md, and returns one Result per skill.
// The returned slice is stable (lexicographic by directory) so the verify
// command's output is reproducible across machines.
func (s *Scanner) Scan(root string) ([]Result, error) {
	dirs, err := manifest.FindSkillDirs(root)
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)

	skills := make([]*manifest.Skill, 0, len(dirs))
	for _, d := range dirs {
		sk, err := manifest.ParseSkill(d)
		if err != nil {
			return nil, err
		}
		skills = append(skills, sk)
	}

	results := EvaluateAll(skills)
	if s.LockPath == "" {
		return results, nil
	}
	lock, err := s.loadLock()
	if err != nil {
		return nil, fmt.Errorf("load lock: %w", err)
	}
	if lock == nil {
		return results, nil
	}
	for i, r := range results {
		results[i] = applyLockDrift(r, skills[i], lock)
	}
	return results, nil
}

// loadLock reads s.LockPath at most once per Scanner. A missing file resolves
// to a nil lock — callers treat that as "no baseline yet."
func (s *Scanner) loadLock() (*LockFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != nil {
		return s.lock, nil
	}
	raw, err := os.ReadFile(s.LockPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lf LockFile
	if err := yaml.Unmarshal(raw, &lf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.LockPath, err)
	}
	if lf.Entries == nil {
		lf.Entries = map[string]LockEntry{}
	}
	s.lock = &lf
	return s.lock, nil
}

// Save writes the lock file back to disk. Called by `verify --trust` (a future
// flag) and by tests; the verify command at v0.1 is read-only against the
// lock to keep first-run side effects predictable.
func (s *Scanner) Save(lock *LockFile) error {
	if s.LockPath == "" {
		return errors.New("scanner: empty LockPath")
	}
	if err := os.MkdirAll(filepath.Dir(s.LockPath), 0o755); err != nil {
		return err
	}
	if lock.Version == 0 {
		lock.Version = 1
	}
	data, err := yaml.Marshal(lock)
	if err != nil {
		return err
	}
	return os.WriteFile(s.LockPath, data, 0o644)
}

// applyLockDrift upgrades a per-skill Result from "inside-version OK" to
// "cross-version drift detected" when the current manifest's declared scope
// has grown vs. the lock entry. A TRUSTED result becomes SCOPE-DRIFTED if any
// of the four declared-scope axes broadened.
func applyLockDrift(r Result, sk *manifest.Skill, lock *LockFile) Result {
	if sk.Manifest == nil {
		return r
	}
	prev, ok := lock.Entries[sk.Manifest.SkillID]
	if !ok {
		return r
	}
	if r.Verdict != VerdictTrusted {
		return r
	}
	grown := scopeGrowth(prev.Declares, sk.Manifest.Declares)
	if len(grown) == 0 {
		return r
	}
	r.Verdict = VerdictScopeDrifted
	r.Details = "scope grew vs. lock (" + prev.Version + "): " + strings.Join(grown, "; ")
	return r
}

// scopeGrowth lists every axis that broadened from previous to current. It is
// intentionally conservative: a removed entry is NOT growth, only additions
// are. The string entries returned are human-readable for the report; the
// caller does not parse them.
//
// Growth is glob-aware on every axis. A new entry that is already covered by a
// previous declaration is a refinement, not an escalation, and is NOT reported
// — e.g. tightening "Bash(git status*)" to "Bash(git status -s)", or narrowing
// "~/**" to "~/.claude/config". This mirrors the in-version compareTools logic
// so the diff/lock path and the verify path agree on what "broader" means.
func scopeGrowth(prev, curr manifest.Declares) []string {
	var out []string
	if curr.Model != prev.Model && prev.Model != "" {
		out = append(out, fmt.Sprintf("model: %s → %s", prev.Model, curr.Model))
	}
	if added := addedTools(prev.Tools, curr.Tools); len(added) > 0 {
		out = append(out, "tools+ ["+strings.Join(added, ", ")+"]")
	}
	if added := addedPaths(prev.FSWrite, curr.FSWrite); len(added) > 0 {
		out = append(out, "fs_write+ ["+strings.Join(added, ", ")+"]")
	}
	if added := addedPaths(prev.NetworkEgress, curr.NetworkEgress); len(added) > 0 {
		out = append(out, "network_egress+ ["+strings.Join(added, ", ")+"]")
	}
	return out
}

// addedTools returns every tool grant in curr that is NOT already covered by
// prev under the Claude Code grant grammar (literal match or a "Tool(prefix*)"
// wildcard). It reuses the same covered() predicate as the in-version scope
// check so a refinement of an existing wildcard is never mistaken for an
// escalation. curr's order is preserved.
func addedTools(prev, curr []string) []string {
	if len(curr) == 0 {
		return nil
	}
	var out []string
	for _, c := range curr {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !covered(prev, c) {
			out = append(out, c)
		}
	}
	return out
}

// addedPaths returns every fs_write / network_egress entry in curr that is NOT
// already covered by some entry in prev, treating a trailing "**" or "*" in a
// prev entry as a glob. A new path that falls under an existing prev glob is a
// refinement (narrowing), not growth, so it is not reported. curr's order is
// preserved.
func addedPaths(prev, curr []string) []string {
	if len(curr) == 0 {
		return nil
	}
	var out []string
	for _, c := range curr {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !pathCovered(prev, c) {
			out = append(out, c)
		}
	}
	return out
}

// pathCovered reports whether path is covered by any entry in declared. An
// entry covers path when they are equal, or when the entry ends in "*" / "**"
// and path shares the entry's literal prefix. "**" and "*" are treated the
// same here (prefix match) because fs_write / network_egress globs are coarse
// scope declarations, not a full path matcher.
func pathCovered(declared []string, path string) bool {
	for _, d := range declared {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if d == path {
			return true
		}
		prefix, isGlob := globPrefix(d)
		if isGlob && strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// globPrefix strips a trailing "**" or "*" off a path glob and reports whether
// one was present. "${WORKSPACE}/**" → ("${WORKSPACE}/", true);
// "~/.claude/config" → ("~/.claude/config", false).
func globPrefix(s string) (prefix string, isGlob bool) {
	switch {
	case strings.HasSuffix(s, "**"):
		return strings.TrimSuffix(s, "**"), true
	case strings.HasSuffix(s, "*"):
		return strings.TrimSuffix(s, "*"), true
	default:
		return s, false
	}
}
