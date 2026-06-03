// Package manifest defines the on-disk shape of the skillsig manifest and the
// SKILL.md frontmatter we read alongside it. The schema is intentionally narrow
// at v1: only the four declared-scope axes (model, tools, fs_write,
// network_egress) plus a publisher / version pair and an optional attestation
// pointer. Everything else stays out so that author edits stay small.
package manifest

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
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	AllowedTools []string `yaml:"allowed-tools"`
}

// Skill is one parsed SKILL.md plus its skillsig manifest (if any). A nil
// Manifest means the skill is unsigned; verify reports it as UNSIGNED.
type Skill struct {
	Dir         string
	Frontmatter SkillFrontmatter
	Manifest    *Manifest
	ManifestSrc string
}
