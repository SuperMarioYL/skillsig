// Package manifest defines the on-disk shape of the skillsig manifest and the
// SKILL.md frontmatter we read alongside it. The schema is intentionally narrow
// at v1: only the four declared-scope axes (model, tools, fs_write,
// network_egress) plus a publisher / version pair and an optional attestation
// pointer. Everything else stays out so that author edits stay small.
package manifest

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillsigVersion is the only manifest schema version recognized at m1.
const SkillsigVersion = "v1"

// Manifest is the parsed shape of a skillsig: YAML block (either embedded in
// SKILL.md as a fenced sidecar or written to a sibling SKILLSIG.yaml).
type Manifest struct {
	Skillsig    string       `yaml:"skillsig"`
	SkillID     string       `yaml:"skill_id"`
	Version     string       `yaml:"version"`
	Declares    Declares     `yaml:"declares"`
	Attestation *Attestation `yaml:"attestation,omitempty"`
}

// Declares is the (model × tools × fs_write × network_egress) tuple that names
// the new noun this product attests. Order of entries is not significant; the
// scope package normalizes before comparing.
type Declares struct {
	Model         string   `yaml:"model"`
	Tools         []string `yaml:"tools"`
	FSWrite       []string `yaml:"fs_write"`
	NetworkEgress []string `yaml:"network_egress"`
}

// SchemaSupported reports whether m is a skillsig manifest whose schema version
// this build understands. It is the SINGLE owner of that rule: sign, verify and
// diff all ask it instead of each comparing against SkillsigVersion themselves,
// so they cannot disagree about what a valid manifest is. A manifest this build
// cannot interpret must never be vouched for (verify) and must never be signed
// (sign).
func (m *Manifest) SchemaSupported() bool {
	return m != nil && m.Skillsig == SkillsigVersion
}

// Attestation is the optional pointer to a Sigstore bundle. m1 does not verify
// the bundle yet; the field is parsed so m2 can light it up without breaking
// the schema.
type Attestation struct {
	SigstoreBundle string `yaml:"sigstore_bundle"`
}

// SkillFrontmatter is the YAML frontmatter that hosts (Claude Code today) read
// from SKILL.md. The grants that actually get honored at runtime live in
// AllowedTools — that is the field skillsig compares against the manifest.
type SkillFrontmatter struct {
	Name         string     `yaml:"name"`
	Description  string     `yaml:"description"`
	AllowedTools StringList `yaml:"allowed-tools"`
}

// StringList is a list of strings that accepts EITHER a YAML sequence or a
// comma-separated scalar string.
//
// It exists for the one frontmatter field skillsig reads but does not define:
// the host's `allowed-tools`. Host-authored SKILL.md files are written in both
// dialects — a YAML sequence
//
//	allowed-tools:
//	  - Read
//	  - "Bash(git status*)"
//
// and a single comma-separated line
//
//	allowed-tools: Read, "Bash(git status*)"
//
// A plain []string only accepts the first, so the second failed the frontmatter
// unmarshal, which aborted the entire verify walk instead of reporting one
// skill. Both dialects decode to the same []string of grants here, and the type
// still MARSHALS as a sequence, so declares.tools that `skillsig init` seeds
// stays a list.
type StringList []string

// UnmarshalYAML implements yaml.Unmarshaler for the two accepted dialects. An
// empty value (`allowed-tools:` with nothing after it) decodes as a null scalar
// and yields an empty list.
func (l *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var raw string
		if err := node.Decode(&raw); err != nil {
			return err
		}
		*l = splitCommaList(raw)
		return nil
	case yaml.SequenceNode:
		var out []string
		if err := node.Decode(&out); err != nil {
			return err
		}
		*l = out
		return nil
	default:
		return fmt.Errorf("allowed-tools: expected a list or a comma-separated string")
	}
}

// splitCommaList turns "Read, , Grep" into ["Read", "Grep"]: entries are
// whitespace-trimmed and empty ones dropped, matching the scope package's
// normalization of every other grant list.
func splitCommaList(raw string) StringList {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Skill is one parsed SKILL.md plus its skillsig manifest (if any). A nil
// Manifest means the skill is unsigned; verify reports it as UNSIGNED.
type Skill struct {
	Dir         string
	Frontmatter SkillFrontmatter
	Manifest    *Manifest
	ManifestSrc string
}
