package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeSkillMD writes a SKILL.md with the given raw frontmatter `allowed-tools`
// value (everything after the colon is passed through verbatim, so a test can
// choose between the scalar and the sequence dialect) and returns its dir.
func writeSkillMD(t *testing.T, allowedTools string) string {
	t.Helper()
	dir := t.TempDir()
	body := "---\nname: dialect\ndescription: d\nallowed-tools:" + allowedTools + "---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return dir
}

// TestParseSkill_AllowedToolsBothDialects is the regression for
// fix-scalar-allowed-tools-unparseable. The host writes this frontmatter field
// two ways — a YAML sequence and a single comma-separated line — and before the
// fix the scalar form failed the frontmatter unmarshal ("cannot unmarshal !!str
// into []string"), which aborted the entire verify walk. Both dialects must
// decode to the same grant list.
func TestParseSkill_AllowedToolsBothDialects(t *testing.T) {
	want := []string{"Read", "Grep", "Bash(git status)"}
	cases := []struct {
		name         string
		allowedTools string
	}{
		{"sequence", "\n  - Read\n  - Grep\n  - Bash(git status)\n"},
		{"scalar-comma", " Read, Grep, Bash(git status)\n"},
		// The exact shape the host ships on this machine:
		//   allowed-tools: Bash(agent-browser:*), Bash(npx agent-browser:*)
		{"scalar-comma-globs", " Bash(agent-browser:*), Bash(npx agent-browser:*)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseSkill(writeSkillMD(t, tc.allowedTools))
			if err != nil {
				t.Fatalf("ParseSkill: %v (the scalar dialect must be accepted)", err)
			}
			wantTools := want
			if tc.name == "scalar-comma-globs" {
				wantTools = []string{"Bash(agent-browser:*)", "Bash(npx agent-browser:*)"}
			}
			if got := []string(s.Frontmatter.AllowedTools); !reflect.DeepEqual(got, wantTools) {
				t.Errorf("allowed-tools = %v, want %v", got, wantTools)
			}
		})
	}
}

// TestParseSkill_AllowedToolsScalarNormalized pins the normalization the scalar
// dialect needs: surrounding whitespace is trimmed and empty items dropped, so
// a hand-written list cannot smuggle an empty grant into the comparison.
func TestParseSkill_AllowedToolsScalarNormalized(t *testing.T) {
	s, err := ParseSkill(writeSkillMD(t, "  Read ,,  Grep ,\n"))
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if got, want := []string(s.Frontmatter.AllowedTools), []string{"Read", "Grep"}; !reflect.DeepEqual(got, want) {
		t.Errorf("allowed-tools = %v, want %v", got, want)
	}
}

// TestParseSkill_AllowedToolsEmptyAndAbsent covers the two degenerate inputs: an
// `allowed-tools:` key with no value (a null scalar) and no key at all. Both
// must yield an empty grant list rather than an error.
func TestParseSkill_AllowedToolsEmptyAndAbsent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		allowedTools string
	}{
		{"empty-value", "\n"},
		{"absent", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dir string
			if tc.name == "absent" {
				dir = t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
					[]byte("---\nname: absent\ndescription: d\n---\nbody\n"), 0o644); err != nil {
					t.Fatalf("write SKILL.md: %v", err)
				}
			} else {
				dir = writeSkillMD(t, tc.allowedTools)
			}
			s, err := ParseSkill(dir)
			if err != nil {
				t.Fatalf("ParseSkill: %v", err)
			}
			if len(s.Frontmatter.AllowedTools) != 0 {
				t.Errorf("allowed-tools = %v, want empty", s.Frontmatter.AllowedTools)
			}
		})
	}
}

// TestSchemaSupported pins the single-owner predicate that sign, verify and diff
// all consult: only the schema version this build understands is supported.
func TestSchemaSupported(t *testing.T) {
	cases := []struct {
		schema string
		want   bool
	}{
		{SkillsigVersion, true},
		{"v2", false},
		{"", false},
	}
	for _, tc := range cases {
		m := &Manifest{Skillsig: tc.schema}
		if got := m.SchemaSupported(); got != tc.want {
			t.Errorf("SchemaSupported(skillsig=%q) = %v, want %v", tc.schema, got, tc.want)
		}
	}
	var nilManifest *Manifest
	if nilManifest.SchemaSupported() {
		t.Error("a nil manifest must not be supported")
	}
}
