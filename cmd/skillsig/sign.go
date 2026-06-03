package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
	"github.com/SuperMarioYL/skillsig/internal/signer"
)

// bundleFileName is the default sidecar name; matches the path the manifest
// example points at (`./skillsig.bundle`).
const bundleFileName = "skillsig.bundle"

func newSignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sign [path]",
		Short: "Sign a skill directory's declared scope and write skillsig.bundle",
		Long: `sign parses the skill at PATH, canonicalizes its declared scope
(skill_id, version, declares), hashes that canonical form, and produces a
sidecar bundle (skillsig.bundle) committing to the hash.

By default sign uses an ephemeral ed25519 keypair (--dev), which produces a
locally-verifiable bundle without contacting Sigstore. Pass --keyless to
request a short-lived Fulcio certificate via OIDC; that backend is wired in
once sigstore-go lands in the dependency tree.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			keyless, _ := cmd.Flags().GetBool("keyless")
			identity, _ := cmd.Flags().GetString("identity")
			issuer, _ := cmd.Flags().GetString("oidc-issuer")
			output, _ := cmd.Flags().GetString("output")
			return runSign(cmd.Context(), cmd.OutOrStdout(), path, signOptions{
				keyless:  keyless,
				identity: identity,
				issuer:   issuer,
				output:   output,
			})
		},
	}
	cmd.Flags().Bool("keyless", false, "use Sigstore keyless OIDC (default: dev/ephemeral ed25519)")
	cmd.Flags().String("identity", "", "OIDC subject for keyless / label for dev (default: dev-ephemeral)")
	cmd.Flags().String("oidc-issuer", "", "OIDC issuer URL for keyless (default: Sigstore Fulcio)")
	cmd.Flags().StringP("output", "o", "", "bundle output path (default: <skill-dir>/skillsig.bundle)")
	return cmd
}

type signOptions struct {
	keyless  bool
	identity string
	issuer   string
	output   string
}

// runSign is the testable core. It is deliberately not a method on a struct so
// tests can pass any io.Writer and any working directory.
func runSign(ctx context.Context, out io.Writer, path string, opts signOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("sign: %s is not a directory", path)
	}

	skill, err := manifest.ParseSkill(path)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	if skill.Manifest == nil {
		return fmt.Errorf("sign: %s has no skillsig manifest (run `skillsig init` first)", path)
	}
	if skill.Manifest.Skillsig != manifest.SkillsigVersion {
		return fmt.Errorf("sign: unsupported manifest schema %q (want %q)", skill.Manifest.Skillsig, manifest.SkillsigVersion)
	}
	if skill.Manifest.SkillID == "" {
		return errors.New("sign: manifest is missing required skill_id")
	}
	if skill.Manifest.Version == "" {
		return errors.New("sign: manifest is missing required version")
	}

	payload, err := canonicalPayload(skill.Manifest)
	if err != nil {
		return fmt.Errorf("sign: canonicalize: %w", err)
	}

	mode := signer.ModeDev
	if opts.keyless {
		mode = signer.ModeKeyless
	}
	sg, err := signer.New(signer.Options{
		Mode:            mode,
		IdentitySubject: opts.identity,
		IdentityIssuer:  opts.issuer,
	})
	if err != nil {
		if errors.Is(err, signer.ErrFulcioNotWired) {
			// Translate the package-level error into actionable CLI guidance.
			return fmt.Errorf("sign: %w\n       retry without --keyless to sign with an ephemeral dev key", err)
		}
		return fmt.Errorf("sign: %w", err)
	}

	bundle, err := sg.Sign(ctx, payload)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	bundlePath := opts.output
	if bundlePath == "" {
		bundlePath = filepath.Join(path, bundleFileName)
	}
	if err := writeBundle(bundlePath, bundle); err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	fmt.Fprintf(out, "signed %s@%s\n", skill.Manifest.SkillID, skill.Manifest.Version)
	fmt.Fprintf(out, "  identity: %s\n", sg.IdentityHint())
	fmt.Fprintf(out, "  digest:   %s (%s)\n", bundle.MessageSignature.MessageDigest.Algorithm, bundle.MessageSignature.MessageDigest.Digest)
	fmt.Fprintf(out, "  bundle:   %s\n", bundlePath)
	if skill.Manifest.Attestation == nil || skill.Manifest.Attestation.SigstoreBundle == "" {
		fmt.Fprintln(out, "  hint:     add `attestation: { sigstore_bundle: ./skillsig.bundle }` to the manifest so verify can find it")
	}
	return nil
}

// canonicalPayload returns the bytes that get hashed and signed. The payload
// commits to (skillsig, skill_id, version, declares) — everything that
// describes "what this skill claims it can do." attestation is excluded
// because it points back at the bundle itself; including it would create a
// chicken-and-egg cycle (the bundle would commit to its own filename).
//
// The yaml encoder is configured with a 2-space indent and produces stable,
// alphabetically-keyed output via the struct tag order, so the same manifest
// produces the same bytes across machines and Go versions.
func canonicalPayload(m *manifest.Manifest) ([]byte, error) {
	canonical := manifest.Manifest{
		Skillsig: m.Skillsig,
		SkillID:  m.SkillID,
		Version:  m.Version,
		Declares: manifest.Declares{
			Model:         m.Declares.Model,
			Tools:         append([]string(nil), m.Declares.Tools...),
			FSWrite:       append([]string(nil), m.Declares.FSWrite...),
			NetworkEgress: append([]string(nil), m.Declares.NetworkEgress...),
		},
	}
	return yaml.Marshal(canonical)
}

// writeBundle marshals the bundle as indented JSON (so diffs in a Skill repo
// are reviewable) and writes it to path with 0644. The file is written via a
// temp+rename to avoid leaving a half-written bundle on disk if the process
// dies mid-write.
func writeBundle(path string, b *signer.Bundle) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bundle: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".skillsig-bundle-*.tmp")
	if err != nil {
		return fmt.Errorf("create tempfile: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tempfile: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	return nil
}
