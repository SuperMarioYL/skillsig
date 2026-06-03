// Package verifier checks a parsed skill's signature bundle against the
// canonicalized scope it claims. It is the bridge between the scope package
// (which says "the declared scope and the runtime grants match") and the
// signer package (which says "this scope was signed by Y").
//
// The verifier is local-only at v0.1: it validates dev-backend ed25519
// bundles produced by `skillsig sign`. The keyless Fulcio path lands when
// sigstore-go is wired in; the signer package already exposes the seam
// (signer.NewKeyless) and the verifier surfaces ErrKeylessNotWired so the
// CLI can print actionable guidance instead of crashing.
package verifier

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
	"github.com/SuperMarioYL/skillsig/internal/signer"
)

// Verdict names the outcome of a single signature check. The values stay
// distinct from scope.Verdict because verification answers a different
// question (is the signature good?) than scope evaluation (does the declared
// scope match the runtime grants?). The verify command composes both.
type Verdict string

const (
	VerdictSigned       Verdict = "SIGNED"
	VerdictNoBundle     Verdict = "NO-BUNDLE"
	VerdictBadSignature Verdict = "BAD-SIGNATURE"
	VerdictKeylessPending Verdict = "KEYLESS-PENDING"
)

// Result is one verification outcome plus the (issuer, subject) tuple the
// bundle attests to. Empty Identity is the no-bundle and bad-signature cases.
type Result struct {
	SkillID  string
	Verdict  Verdict
	Identity signer.Identity
	Err      error
}

// ErrKeylessNotWired surfaces when a bundle's verification material claims a
// certificate (i.e. it came from the Fulcio flow) but this build has no
// keyless verifier. Callers translate it into a "rebuild with keyless tag
// or use --dev to sign" hint.
var ErrKeylessNotWired = errors.New("verifier: keyless bundle verification not yet wired")

// VerifySkill canonicalizes the skill's declared scope, locates the bundle
// (via manifest.Attestation.SigstoreBundle or a sibling skillsig.bundle),
// and validates the signature with the signer package. A missing bundle
// resolves to VerdictNoBundle (NOT an error) so verify can keep walking
// the rest of the skill set.
func VerifySkill(s *manifest.Skill) Result {
	r := Result{SkillID: skillID(s)}
	if s == nil || s.Manifest == nil {
		r.Verdict = VerdictNoBundle
		return r
	}

	bundlePath, err := resolveBundlePath(s)
	if err != nil {
		r.Verdict = VerdictNoBundle
		return r
	}

	bundle, err := readBundle(bundlePath)
	if errors.Is(err, fs.ErrNotExist) {
		r.Verdict = VerdictNoBundle
		return r
	}
	if err != nil {
		r.Verdict = VerdictBadSignature
		r.Err = fmt.Errorf("read bundle %s: %w", bundlePath, err)
		return r
	}

	payload, err := CanonicalPayload(s.Manifest)
	if err != nil {
		r.Verdict = VerdictBadSignature
		r.Err = fmt.Errorf("canonicalize: %w", err)
		return r
	}

	if bundle.VerificationMaterial.PublicKey == "" && bundle.VerificationMaterial.Certificate != "" {
		r.Verdict = VerdictKeylessPending
		r.Err = ErrKeylessNotWired
		r.Identity = bundle.VerificationMaterial.Identity
		return r
	}

	if err := signer.VerifyBundle(bundle, payload); err != nil {
		r.Verdict = VerdictBadSignature
		r.Err = err
		return r
	}

	r.Verdict = VerdictSigned
	r.Identity = bundle.VerificationMaterial.Identity
	return r
}

// CanonicalPayload produces the same bytes the sign command hashed. The
// function is exported so tests (and a future `skillsig verify --explain`
// flag) can produce the digest themselves and compare. Keeping the rule in
// one place — here — is what makes sign and verify round-trip safely.
func CanonicalPayload(m *manifest.Manifest) ([]byte, error) {
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

// resolveBundlePath honors manifest.Attestation.SigstoreBundle when present
// (relative paths are resolved against the skill dir), else falls back to
// "<dir>/skillsig.bundle".
func resolveBundlePath(s *manifest.Skill) (string, error) {
	if s.Manifest.Attestation != nil && s.Manifest.Attestation.SigstoreBundle != "" {
		p := s.Manifest.Attestation.SigstoreBundle
		if !filepath.IsAbs(p) {
			p = filepath.Join(s.Dir, p)
		}
		return p, nil
	}
	return filepath.Join(s.Dir, "skillsig.bundle"), nil
}

func readBundle(path string) (*signer.Bundle, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b signer.Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("decode bundle json: %w", err)
	}
	return &b, nil
}

func skillID(s *manifest.Skill) string {
	if s == nil {
		return ""
	}
	if s.Manifest != nil && s.Manifest.SkillID != "" {
		return s.Manifest.SkillID
	}
	if s.Frontmatter.Name != "" {
		return s.Frontmatter.Name
	}
	return s.Dir
}
