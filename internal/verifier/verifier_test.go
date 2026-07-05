package verifier

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
	"github.com/SuperMarioYL/skillsig/internal/signer"
)

// writeSkillDir writes a minimal SKILL.md + SKILLSIG.yaml sidecar into a fresh
// temp dir and returns the parsed skill. tools seeds both allowed-tools and
// declares.tools so the skill is scope-consistent; the tests here only care
// about the SIGNATURE, which is independent of the scope check.
func writeSkillDir(t *testing.T, skillID string, tools []string) (*manifest.Skill, string) {
	t.Helper()
	dir := t.TempDir()
	var b string
	for _, tl := range tools {
		b += "  - " + tl + "\n"
	}
	skillMD := "---\nname: fixture\nallowed-tools:\n" + b + "---\n# fixture\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	var d string
	for _, tl := range tools {
		d += "    - " + tl + "\n"
	}
	sidecar := "skillsig: v1\nskill_id: " + skillID + "\nversion: v1\ndeclares:\n  tools:\n" + d
	if err := os.WriteFile(filepath.Join(dir, "SKILLSIG.yaml"), []byte(sidecar), 0o644); err != nil {
		t.Fatalf("write SKILLSIG.yaml: %v", err)
	}
	sk, err := manifest.ParseSkill(dir)
	if err != nil {
		t.Fatalf("parse skill: %v", err)
	}
	return sk, dir
}

// signInto signs sk's canonical payload with a fresh dev ed25519 key and writes
// the bundle to <dir>/skillsig.bundle (the default path VerifySkill resolves).
func signInto(t *testing.T, sk *manifest.Skill, dir string) {
	t.Helper()
	payload, err := CanonicalPayload(sk.Manifest)
	if err != nil {
		t.Fatalf("canonical payload: %v", err)
	}
	s, err := signer.NewDev("test-subject")
	if err != nil {
		t.Fatalf("new dev signer: %v", err)
	}
	bundle, err := s.Sign(context.Background(), payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	writeBundleJSON(t, filepath.Join(dir, "skillsig.bundle"), bundle)
}

func writeBundleJSON(t *testing.T, path string, b *signer.Bundle) {
	t.Helper()
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
}

// TestVerifySkill_ValidBundleIsSigned is the happy path: a skill signed over its
// own canonical payload verifies SIGNED and surfaces the signer identity.
func TestVerifySkill_ValidBundleIsSigned(t *testing.T) {
	sk, dir := writeSkillDir(t, "examples/signed", []string{"Read", "Bash(git status*)"})
	signInto(t, sk, dir)

	got := VerifySkill(sk)
	if got.Verdict != VerdictSigned {
		t.Fatalf("verdict: got %q want %q (err=%v)", got.Verdict, VerdictSigned, got.Err)
	}
	if got.Err != nil {
		t.Errorf("SIGNED result should carry no error, got %v", got.Err)
	}
	if got.Identity.Subject != "test-subject" {
		t.Errorf("identity subject: got %q want %q", got.Identity.Subject, "test-subject")
	}
}

// TestVerifySkill_NoBundleIsNoBundle: a skill with no bundle on disk resolves to
// NO-BUNDLE (not an error verdict) so verify can keep walking the corpus.
func TestVerifySkill_NoBundleIsNoBundle(t *testing.T) {
	sk, _ := writeSkillDir(t, "examples/unsigned", []string{"Read"})
	got := VerifySkill(sk)
	if got.Verdict != VerdictNoBundle {
		t.Fatalf("verdict: got %q want %q", got.Verdict, VerdictNoBundle)
	}
}

// TestVerifySkill_TamperedManifestFailsSignature is the security case: sign the
// skill, then broaden its declared scope on disk (the jqwik re-signing vector
// WITHOUT re-signing). The bundle no longer matches the canonical payload, so
// verification must report BAD-SIGNATURE — never SIGNED.
func TestVerifySkill_TamperedManifestFailsSignature(t *testing.T) {
	sk, dir := writeSkillDir(t, "examples/tampered", []string{"Read"})
	signInto(t, sk, dir)

	// Tamper: broaden declares.tools AFTER signing, leaving the old bundle.
	sidecar := "skillsig: v1\nskill_id: examples/tampered\nversion: v1\ndeclares:\n  tools:\n    - Read\n    - Bash(rm -rf ~/)\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILLSIG.yaml"), []byte(sidecar), 0o644); err != nil {
		t.Fatalf("tamper sidecar: %v", err)
	}
	tampered, err := manifest.ParseSkill(dir)
	if err != nil {
		t.Fatalf("re-parse tampered: %v", err)
	}

	got := VerifySkill(tampered)
	if got.Verdict != VerdictBadSignature {
		t.Fatalf("tampered manifest must be BAD-SIGNATURE; got %q (err=%v)", got.Verdict, got.Err)
	}
	if got.Err == nil {
		t.Errorf("BAD-SIGNATURE result should carry the verification error")
	}
}

// TestVerifySkill_CorruptBundleIsBadSignature: a bundle that isn't valid JSON is
// BAD-SIGNATURE (a present-but-broken attestation is a red flag, not NO-BUNDLE).
func TestVerifySkill_CorruptBundleIsBadSignature(t *testing.T) {
	sk, dir := writeSkillDir(t, "examples/corrupt", []string{"Read"})
	if err := os.WriteFile(filepath.Join(dir, "skillsig.bundle"), []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("write corrupt bundle: %v", err)
	}
	got := VerifySkill(sk)
	if got.Verdict != VerdictBadSignature {
		t.Fatalf("corrupt bundle must be BAD-SIGNATURE; got %q", got.Verdict)
	}
}

// TestVerifySkill_KeylessBundleIsPending: a bundle carrying a certificate (the
// Fulcio keyless flow) but no public key is KEYLESS-PENDING in this dev build —
// it does not verify, but it is not a hard failure either; the identity still
// surfaces so the CLI can print actionable guidance.
func TestVerifySkill_KeylessBundleIsPending(t *testing.T) {
	sk, dir := writeSkillDir(t, "examples/keyless", []string{"Read"})
	// Hand-craft a keyless-shaped bundle: certificate set, publicKey empty.
	b := &signer.Bundle{
		MediaType: signer.MediaType,
		VerificationMaterial: signer.VerificationMaterial{
			Certificate: "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
			Identity:    signer.Identity{Issuer: "https://oauth2.sigstore.dev/auth", Subject: "user@example.com"},
		},
	}
	writeBundleJSON(t, filepath.Join(dir, "skillsig.bundle"), b)

	got := VerifySkill(sk)
	if got.Verdict != VerdictKeylessPending {
		t.Fatalf("keyless bundle must be KEYLESS-PENDING; got %q", got.Verdict)
	}
	if got.Identity.Subject != "user@example.com" {
		t.Errorf("keyless identity should surface; got subject %q", got.Identity.Subject)
	}
}

// TestVerifySkill_NilAndEmpty guards the defensive paths: a nil skill and a skill
// with no manifest both resolve to NO-BUNDLE without panicking.
func TestVerifySkill_NilAndEmpty(t *testing.T) {
	if got := VerifySkill(nil); got.Verdict != VerdictNoBundle {
		t.Errorf("nil skill: got %q want %q", got.Verdict, VerdictNoBundle)
	}
	if got := VerifySkill(&manifest.Skill{Dir: "x"}); got.Verdict != VerdictNoBundle {
		t.Errorf("no-manifest skill: got %q want %q", got.Verdict, VerdictNoBundle)
	}
}
