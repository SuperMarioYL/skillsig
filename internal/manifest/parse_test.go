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

// writeManifestFixture writes SKILL.md with the given frontmatter allowed-tools
// block and the given raw SKILLSIG.yaml body (empty string = no sibling file),
// returning the directory.
func writeManifestFixture(t *testing.T, allowedTools, siblingYAML string) string {
	t.Helper()
	dir := t.TempDir()
	skillMD := "---\nname: demo\ndescription: d\nallowed-tools:" + allowedTools + "---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if siblingYAML != "" {
		if err := os.WriteFile(filepath.Join(dir, "SKILLSIG.yaml"), []byte(siblingYAML), 0o644); err != nil {
			t.Fatalf("write SKILLSIG.yaml: %v", err)
		}
	}
	return dir
}

// TestParseSkill_AllowedToolsScalarUnquoted is the regression for
// fix-quoted-allowed-tools-entry-not-unquoted. The comma-separated dialect only
// trimmed its entries, so a quoted grant kept its literal quote characters while
// the YAML-sequence dialect had them stripped by the YAML parser — the same
// grant then compared unequal and a correctly declared skill read
// SCOPE-DRIFTED. The quoted form is the one this package documents as accepted.
func TestParseSkill_AllowedToolsScalarUnquoted(t *testing.T) {
	s, err := ParseSkill(writeSkillMD(t, " Read, \"Bash(git status*)\", 'Grep'\n"))
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	want := []string{"Read", "Bash(git status*)", "Grep"}
	if got := []string(s.Frontmatter.AllowedTools); !reflect.DeepEqual(got, want) {
		t.Errorf("allowed-tools = %q, want %q (quotes must be dropped like the YAML dialect does)", got, want)
	}
}

// TestParseSkill_AllowedToolsScalarKeepsInnerQuotes pins the narrowness of the
// unquoting: only a matching pair surrounding the WHOLE entry is dropped, so a
// grant whose argument legitimately contains quotes (or whose quotes do not
// surround the entry) is untouched.
func TestParseSkill_AllowedToolsScalarKeepsInnerQuotes(t *testing.T) {
	s, err := ParseSkill(writeSkillMD(t, " Bash(echo \"hi\"), \"Bash(unterminated)\n"))
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	want := []string{`Bash(echo "hi")`, `"Bash(unterminated)`}
	if got := []string(s.Frontmatter.AllowedTools); !reflect.DeepEqual(got, want) {
		t.Errorf("allowed-tools = %q, want %q", got, want)
	}
}

// TestParseSkill_CommentHeadedInlineManifestWins is the parse-level regression
// for fix-comment-headed-inline-manifest-ignored: a fenced manifest introduced
// by a YAML comment must be read as the manifest (source "sidecar"), not skipped
// in favour of a stale sibling SKILLSIG.yaml.
func TestParseSkill_CommentHeadedInlineManifestWins(t *testing.T) {
	dir := t.TempDir()
	skillMD := "---\nname: demo\ndescription: d\nallowed-tools:\n  - Read\n---\nbody\n\n" +
		"```yaml\n# fresh manifest\nskillsig: v1\nskill_id: local/demo\nversion: 2.0.0\ndeclares:\n  tools:\n    - Read\n```\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	stale := "skillsig: v1\nskill_id: local/demo\nversion: 1.0.0\ndeclares:\n  tools:\n    - Read\n  fs_write:\n    - \"~/**\"\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILLSIG.yaml"), []byte(stale), 0o644); err != nil {
		t.Fatalf("write SKILLSIG.yaml: %v", err)
	}

	s, err := ParseSkill(dir)
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if s.ManifestSrc != "sidecar" {
		t.Fatalf("manifest source = %q, want sidecar (the comment-headed inline block was skipped)", s.ManifestSrc)
	}
	if s.Manifest.Version != "2.0.0" {
		t.Errorf("manifest version = %q, want 2.0.0 (the inline block; 1.0.0 is the stale sibling)", s.Manifest.Version)
	}
}

// TestParseSkill_UnknownManifestKeyRejected is the regression for
// fix-manifest-unknown-keys-silently-dropped. Both manifest sources used plain
// yaml.Unmarshal, which ignores keys the v1 schema does not define, so a
// misspelled `declares:` decoded to an empty block that sign then attested and
// verify read TRUSTED.
func TestParseSkill_UnknownManifestKeyRejected(t *testing.T) {
	cases := map[string]string{
		"top-level-typo": "skillsig: v1\nskill_id: local/demo\nversion: 1.0.0\ndecares:\n  tools:\n    - Read\n",
		"declares-typo":  "skillsig: v1\nskill_id: local/demo\nversion: 1.0.0\ndeclares:\n  toolz:\n    - Read\n",
	}
	for name, manifestYAML := range cases {
		t.Run(name+"/sibling", func(t *testing.T) {
			if _, err := ParseSkill(writeManifestFixture(t, "\n", manifestYAML)); err == nil {
				t.Fatal("a manifest with a key outside the v1 schema must not parse cleanly")
			}
		})
		t.Run(name+"/inline", func(t *testing.T) {
			dir := writeManifestFixture(t, "\n", "")
			skillMD := "---\nname: demo\ndescription: d\n---\nbody\n\n```yaml\n" + manifestYAML + "```\n"
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
				t.Fatalf("write SKILL.md: %v", err)
			}
			if _, err := ParseSkill(dir); err == nil {
				t.Fatal("an inline manifest with a key outside the v1 schema must not parse cleanly")
			}
		})
	}
}

// TestParseSkill_EmptySiblingIsMalformedNotError pins the decode boundary: an
// empty SKILLSIG.yaml still yields a zero manifest (reported MALFORMED by the
// schema rule) rather than a parse error or a nil manifest.
func TestParseSkill_EmptySiblingIsMalformedNotError(t *testing.T) {
	s, err := ParseSkill(writeManifestFixture(t, "\n", "\n"))
	if err != nil {
		t.Fatalf("an empty SKILLSIG.yaml must not be a decode error: %v", err)
	}
	if s.Manifest == nil {
		t.Fatal("expected a (zero) manifest so it can be reported MALFORMED")
	}
	if s.Manifest.SchemaSupported() {
		t.Error("a zero manifest must not be schema-supported")
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
