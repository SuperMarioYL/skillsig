// Package signer produces and represents signature bundles over a skillsig
// manifest's declared scope. The package is split into three pieces:
//
//   - The Signer interface — everything the sign command needs from a backend.
//     Backends are swapped at construction time; the rest of the codebase only
//     sees this interface, which keeps the Fulcio/Rekor network calls out of
//     every unit test.
//   - The Bundle / Identity types — what gets written to disk and what verify
//     consumes. Their JSON shape is a thin subset of the Sigstore protobuf
//     bundle (mediaType + messageSignature + verificationMaterial) so future
//     migration to a real sigstore-go bundle is a field-rename, not a rewrite.
//   - A stdlib-only DevSigner backed by ephemeral ed25519. It exists so the
//     sign command is runnable end-to-end without network access (CI, air-
//     gapped, first-run author exploration). The keyless Fulcio backend
//     plugs in alongside it without changing any caller — see NewKeyless.
package signer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// MediaType identifies the on-disk bundle format. The "+json;version=0.1"
// suffix tracks the skillsig bundle schema independently of the Sigstore
// protobuf bundle media type so a future migration can be detected by the
// verifier without ambiguity.
const MediaType = "application/vnd.dev.skillsig.bundle+json;version=0.1"

// DigestAlgorithm is the only hash function the v0.1 bundle accepts. Stored
// as a string to match the Sigstore protobuf enum naming.
const DigestAlgorithm = "SHA2_256"

// Errors a caller may want to check with errors.Is.
var (
	// ErrFulcioNotWired is returned by NewKeyless when the binary has been
	// built without the sigstore-go integration. The sign command surfaces a
	// hint about --dev so authors are never stuck.
	ErrFulcioNotWired = errors.New("sigstore keyless backend is not wired in this build (use --dev to sign with an ephemeral key)")

	// ErrEmptyPayload guards against accidentally signing a zero-byte payload,
	// which would produce a structurally valid bundle that attests nothing.
	ErrEmptyPayload = errors.New("refusing to sign empty payload")
)

// Signer is the only contract the sign command depends on. Implementations
// MUST be safe for sequential reuse but need not be concurrent-safe (the CLI
// signs one manifest per invocation).
type Signer interface {
	// Sign hashes payload with DigestAlgorithm and returns a bundle that
	// commits to that digest. Implementations attach their own
	// verificationMaterial (public key, certificate chain, identity).
	Sign(ctx context.Context, payload []byte) (*Bundle, error)

	// IdentityHint returns a short, human-readable string describing who the
	// signer believes itself to be (e.g. an email for keyless, "dev-ephemeral"
	// for the local backend). It is printed by the sign command so the author
	// can spot an obviously wrong identity before publishing.
	IdentityHint() string
}

// Bundle is the on-disk artifact. Field tags are JSON, not YAML, because the
// bundle ships as a sidecar file (skillsig.bundle) that verify reads
// independently of the manifest. Keeping it JSON sidesteps YAML's whitespace
// pitfalls for a binary-ish payload.
type Bundle struct {
	MediaType            string               `json:"mediaType"`
	MessageSignature     MessageSignature     `json:"messageSignature"`
	VerificationMaterial VerificationMaterial `json:"verificationMaterial"`
	CreatedAt            time.Time            `json:"createdAt"`
}

// MessageSignature pairs a digest with the signature over that digest. The
// digest is included (not just the signature) so a verifier can confirm it is
// looking at the same payload the signer hashed.
type MessageSignature struct {
	MessageDigest Digest `json:"messageDigest"`
	Signature     string `json:"signature"` // base64
}

// Digest names the hash algorithm and the resulting bytes. The algorithm
// string mirrors the Sigstore protobuf enum so a future migration can map
// 1:1.
type Digest struct {
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"` // base64
}

// VerificationMaterial is what a verifier needs to check the signature and
// surface the signer's identity. The PublicKey field is populated by the dev
// backend; the Certificate / RekorEntry fields are populated by keyless
// backends. A given bundle uses exactly one of (PublicKey, Certificate).
type VerificationMaterial struct {
	PublicKey   string   `json:"publicKey,omitempty"`   // base64 raw key bytes
	Certificate string   `json:"certificate,omitempty"` // PEM, when keyless
	RekorEntry  string   `json:"rekorEntry,omitempty"`  // transparency log pointer, when keyless
	Identity    Identity `json:"identity"`
}

// Identity is the (issuer, subject) pair extracted from the OIDC token used
// to obtain the short-lived signing certificate. For the dev backend the
// subject is "dev-ephemeral" and the issuer is empty.
type Identity struct {
	Issuer  string `json:"issuer,omitempty"`
	Subject string `json:"subject"`
}

// HashPayload exposes the same digest computation Sign uses internally, so
// callers can pre-compute and compare digests (e.g. for tests that don't want
// to depend on a specific signer's byte-for-byte output).
func HashPayload(payload []byte) Digest {
	sum := sha256.Sum256(payload)
	return Digest{
		Algorithm: DigestAlgorithm,
		Digest:    base64.StdEncoding.EncodeToString(sum[:]),
	}
}

// Options configures a signer at construction time. Zero values are valid:
// the default Mode is ModeDev (no network), the default IdentitySubject is
// "dev-ephemeral".
type Options struct {
	// Mode selects the backend.
	Mode Mode

	// IdentitySubject is the OIDC subject the keyless flow should request, or
	// the label written into the bundle for ModeDev. For keyless, leaving it
	// empty triggers an interactive browser flow at sign time.
	IdentitySubject string

	// IdentityIssuer pins the OIDC issuer (e.g.
	// "https://oauth2.sigstore.dev/auth"). Empty = sigstore-go default.
	IdentityIssuer string
}

// Mode selects which Signer implementation New returns.
type Mode int

const (
	// ModeDev signs with an ephemeral ed25519 keypair generated in-process.
	// No network, no Fulcio, no Rekor. The resulting bundle is verifiable
	// only against the public key embedded in itself — fine for local
	// development and CI smoke tests, not for distribution.
	ModeDev Mode = iota

	// ModeKeyless requests a short-lived signing certificate from Sigstore
	// Fulcio via OIDC and writes the resulting transparency log entry into
	// the bundle. Wired in when sigstore-go lands; until then New returns
	// ErrFulcioNotWired.
	ModeKeyless
)

// New constructs a Signer for opts.Mode. It is the only public constructor
// the sign command calls so backends can grow without touching the CLI.
func New(opts Options) (Signer, error) {
	switch opts.Mode {
	case ModeDev:
		return NewDev(opts.IdentitySubject)
	case ModeKeyless:
		return NewKeyless(opts)
	default:
		return nil, fmt.Errorf("unknown signer mode: %d", opts.Mode)
	}
}

// NewDev returns an ed25519-backed Signer. If subject is empty, "dev-
// ephemeral" is used. The keypair is generated once per call and not
// persisted — re-signing the same payload produces a different bundle each
// time (different public key, different signature).
func NewDev(subject string) (Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	if subject == "" {
		subject = "dev-ephemeral"
	}
	return &devSigner{pub: pub, priv: priv, subject: subject}, nil
}

// NewKeyless is the plug point for the real Sigstore Fulcio flow. The signer
// package keeps a stub so the rest of the codebase compiles and tests run
// without sigstore-go in the dependency tree; the production wrapper drops
// in alongside this file in a follow-up build stage.
func NewKeyless(_ Options) (Signer, error) {
	return nil, ErrFulcioNotWired
}

type devSigner struct {
	pub     ed25519.PublicKey
	priv    ed25519.PrivateKey
	subject string
}

func (d *devSigner) Sign(ctx context.Context, payload []byte) (*Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, ErrEmptyPayload
	}
	digest := HashPayload(payload)
	rawDigest, err := base64.StdEncoding.DecodeString(digest.Digest)
	if err != nil {
		// Cannot happen — HashPayload produces valid base64. Belt-and-braces
		// so a future refactor that swaps the encoding gets a loud failure.
		return nil, fmt.Errorf("digest re-decode: %w", err)
	}
	sig := ed25519.Sign(d.priv, rawDigest)
	return &Bundle{
		MediaType: MediaType,
		MessageSignature: MessageSignature{
			MessageDigest: digest,
			Signature:     base64.StdEncoding.EncodeToString(sig),
		},
		VerificationMaterial: VerificationMaterial{
			PublicKey: base64.StdEncoding.EncodeToString(d.pub),
			Identity: Identity{
				Subject: d.subject,
			},
		},
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (d *devSigner) IdentityHint() string {
	return d.subject + " (dev/ephemeral ed25519)"
}

// VerifyBundle re-checks a bundle against the payload that was supposedly
// signed. It returns nil on success. The function deliberately covers only
// the dev backend (PublicKey-based) — verification of keyless bundles
// requires Fulcio/Rekor trust roots and lives next to NewKeyless.
//
// VerifyBundle is exposed so the verify command and tests share one code
// path. Without it, signer would be write-only and signature regressions
// could go unnoticed until release.
func VerifyBundle(b *Bundle, payload []byte) error {
	if b == nil {
		return errors.New("nil bundle")
	}
	if b.MediaType != MediaType {
		return fmt.Errorf("unexpected mediaType %q", b.MediaType)
	}
	if b.MessageSignature.MessageDigest.Algorithm != DigestAlgorithm {
		return fmt.Errorf("unexpected digest algorithm %q", b.MessageSignature.MessageDigest.Algorithm)
	}
	want := HashPayload(payload)
	if want.Digest != b.MessageSignature.MessageDigest.Digest {
		return errors.New("payload digest does not match bundle")
	}
	if b.VerificationMaterial.PublicKey == "" {
		return errors.New("bundle has no public key (keyless verification not yet wired)")
	}
	pubBytes, err := base64.StdEncoding.DecodeString(b.VerificationMaterial.PublicKey)
	if err != nil {
		return fmt.Errorf("decode publicKey: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("publicKey is not ed25519 (len %d)", len(pubBytes))
	}
	sigBytes, err := base64.StdEncoding.DecodeString(b.MessageSignature.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	digestBytes, err := base64.StdEncoding.DecodeString(b.MessageSignature.MessageDigest.Digest)
	if err != nil {
		return fmt.Errorf("decode digest: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), digestBytes, sigBytes) {
		return errors.New("ed25519 signature verification failed")
	}
	return nil
}
