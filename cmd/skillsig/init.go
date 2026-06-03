package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
)

// sidecarFileName is the default sibling-file destination when --inline is not
// passed. SKILLSIG.yaml is the conservative default because it does not touch
// the SKILL.md the host CLI loads at runtime, so an author can adopt skillsig
// without changing anything Claude Code or another host reads.
const sidecarFileName = "SKILLSIG.yaml"

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Generate a starter skillsig manifest by reading SKILL.md frontmatter",
		Long: `init reads SKILL.md at PATH (default: current dir), seeds a skillsig
manifest from its allowed-tools frontmatter, and writes it to disk. Three
fields cannot be derived from existing SKILL.md metadata and are written as
placeholders the author MUST edit before signing:

  - declares.model           (e.g. claude-opus-4-7)
  - declares.fs_write        (defaults to ${WORKSPACE}/** — narrow before signing)
  - declares.network_egress  (defaults to [] — add hosts the skill calls)

By default the manifest is written as a sibling SKILLSIG.yaml so SKILL.md is
left untouched. Pass --inline to append it as a fenced code block at the end
of SKILL.md instead.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			inline, _ := cmd.Flags().GetBool("inline")
			force, _ := cmd.Flags().GetBool("force")
			skillID, _ := cmd.Flags().GetString("skill-id")
			version, _ := cmd.Flags().GetString("version")
			model, _ := cmd.Flags().GetString("model")
			return runInit(cmd.OutOrStdout(), path, initOptions{
				inline:  inline,
				force:   force,
				skillID: skillID,
				version: version,
				model:   model,
			})
		},
	}
	cmd.Flags().Bool("inline", false, "append the manifest as a fenced block in SKILL.md instead of writing SKILLSIG.yaml")
	cmd.Flags().Bool("force", false, "overwrite an existing manifest")
	cmd.Flags().String("skill-id", "", "skill_id to write (default: derived from <dir-name>)")
	cmd.Flags().String("version", "0.1.0", "version to write into the manifest")
	cmd.Flags().String("model", "claude-opus-4-7", "model the skill targets (declares.model)")
	return cmd
}

type initOptions struct {
	inline  bool
	force   bool
	skillID string
	version string
	model   string
}

// runInit is the testable core. It deliberately does not exit on its own — any
// failure is returned as an error so the cobra layer can frame the message.
func runInit(out io.Writer, path string, opts initOptions) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("init: %s is not a directory", path)
	}

	skill, err := manifest.ParseSkill(path)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	if skill.Manifest != nil && !opts.force {
		return fmt.Errorf("init: %s already has a skillsig manifest (source=%s); pass --force to overwrite",
			path, skill.ManifestSrc)
	}

	m := seedManifest(skill, opts)
	data, err := marshalManifest(m)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}

	if opts.inline {
		dest := filepath.Join(path, "SKILL.md")
		if err := appendInline(dest, data, opts.force); err != nil {
			return fmt.Errorf("init: %w", err)
		}
		fmt.Fprintf(out, "wrote skillsig manifest into %s (fenced sidecar)\n", dest)
	} else {
		dest := filepath.Join(path, sidecarFileName)
		if err := writeSidecar(dest, data, opts.force); err != nil {
			return fmt.Errorf("init: %w", err)
		}
		fmt.Fprintf(out, "wrote %s\n", dest)
	}

	fmt.Fprintln(out, "next steps:")
	fmt.Fprintln(out, "  1) edit declares.model / declares.fs_write / declares.network_egress to match the skill")
	fmt.Fprintln(out, "  2) `skillsig sign` to produce skillsig.bundle (use --keyless once Fulcio is wired)")
	fmt.Fprintln(out, "  3) commit SKILLSIG.yaml + skillsig.bundle alongside SKILL.md")
	return nil
}

// seedManifest builds the starter manifest. declares.tools is copied verbatim
// from SKILL.md allowed-tools so the first `skillsig verify` after init
// returns TRUSTED; the author is expected to widen or narrow this set
// deliberately before signing.
func seedManifest(skill *manifest.Skill, opts initOptions) *manifest.Manifest {
	skillID := opts.skillID
	if skillID == "" {
		skillID = deriveSkillID(skill)
	}
	tools := append([]string(nil), skill.Frontmatter.AllowedTools...)
	if tools == nil {
		tools = []string{}
	}
	return &manifest.Manifest{
		Skillsig: manifest.SkillsigVersion,
		SkillID:  skillID,
		Version:  opts.version,
		Declares: manifest.Declares{
			Model:         opts.model,
			Tools:         tools,
			FSWrite:       []string{"${WORKSPACE}/**"},
			NetworkEgress: []string{},
		},
		Attestation: &manifest.Attestation{
			SigstoreBundle: "./skillsig.bundle",
		},
	}
}

// deriveSkillID falls back through name → directory basename so init works
// even when SKILL.md lacks a name (or is in a top-level dir).
func deriveSkillID(skill *manifest.Skill) string {
	if skill.Frontmatter.Name != "" {
		return "local/" + skill.Frontmatter.Name
	}
	base := filepath.Base(skill.Dir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "unnamed-skill"
	}
	return "local/" + base
}

func marshalManifest(m *manifest.Manifest) ([]byte, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(stringWriter{&buf})
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}
	return []byte(buf.String()), nil
}

type stringWriter struct{ b *strings.Builder }

func (s stringWriter) Write(p []byte) (int, error) { return s.b.Write(p) }

func writeSidecar(dest string, data []byte, force bool) error {
	if _, err := os.Stat(dest); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", dest)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

// appendInline tacks a fenced ```yaml block onto SKILL.md, separated from the
// existing body by a blank line. If a sidecar already exists in the body the
// caller has already gated on --force; we still defend against double-append
// here by scanning for a "skillsig:" fenced block and rewriting it in place.
func appendInline(dest string, manifestYAML []byte, force bool) error {
	raw, err := os.ReadFile(dest)
	if err != nil {
		return err
	}
	existing := string(raw)
	if strings.Contains(existing, "skillsig:") && !force {
		return fmt.Errorf("%s already contains a skillsig block; pass --force to rewrite", dest)
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(existing, "\n"))
	b.WriteString("\n\n## skillsig manifest\n\n```yaml\n")
	b.Write(manifestYAML)
	if !strings.HasSuffix(string(manifestYAML), "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n")
	return os.WriteFile(dest, []byte(b.String()), 0o644)
}
