# Changelog

All notable changes to skillsig are tracked here. Format roughly follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning is
[Semantic](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing yet — m3 (`skillsig diff old/ new/` + lockfile-driven cross-version
drift) and the v0.2 hosted-mirror tier are next.

## [0.1.0] — 2026-06-04

First public release. Targets the m1 + m2 milestones from the project roadmap,
plus the polish work that makes the repo runnable and reviewable.

### Added — m1 (manifest + verify)
- `manifest` package: schema (`Manifest`, `Declares`, `Attestation`,
  `SkillFrontmatter`, `Skill`), `ParseSkill` for SKILL.md + sidecar / sibling
  `SKILLSIG.yaml`, and `FindSkillDirs` for walking a tree.
- `scope` package: `Evaluate` / `EvaluateAll` producing
  `TRUSTED` / `UNSIGNED` / `SCOPE-DRIFTED` verdicts, with grant-grammar globs
  that mirror Claude Code's `allowed-tools` syntax (`Tool(prefix*)`).
- `scope.Scanner`: lock-file-aware walker (reads `~/.skillsig/lock.yaml` via
  `SKILLSIG_HOME` or `$HOME/.skillsig/`) that upgrades a TRUSTED row to
  SCOPE-DRIFTED when cross-version growth is detected — the m3 seam.
- `report` package: lipgloss-styled 3-column table + plain-text fallback for
  CI; one-line summary tally.
- `cmd/skillsig verify [path]` with `--ci` (exits non-zero on drift) and
  `--no-color`.

### Added — m2 (sign)
- `signer` package: `Signer` interface, on-disk `Bundle` JSON shape with
  media type `application/vnd.dev.skillsig.bundle+json;version=0.1`,
  ephemeral ed25519 dev backend, and a `NewKeyless` seam returning
  `ErrFulcioNotWired` until sigstore-go lands.
- `cmd/skillsig sign [path]` with `--keyless` / `--identity` / `--oidc-issuer`
  flags, atomic write-and-rename to avoid half-written bundles.
- `verifier` package: re-canonicalizes the manifest's declared scope and
  re-runs `signer.VerifyBundle` so verify can confirm round-trip integrity.

### Added — polish
- `cmd/skillsig init [path]` seeds a starter manifest from the SKILL.md
  `allowed-tools` frontmatter, with `--inline` to append fenced sidecar and
  `--force` to overwrite. Placeholder defaults for `model`, `fs_write`, and
  `network_egress` (the three fields that have no source in existing
  SKILL.md metadata).
- Three fixtures under `testdata/skills/`: `safe-skill` (TRUSTED),
  `jqwik-style-bad` (SCOPE-DRIFTED — reproduces the Ars Technica May 2026
  incident), `scope-mismatch` (UNSIGNED — most common state today).
- Bilingual READMEs (zh-CN primary + English sibling), visually polished
  with shields.io badges and a capsule-render banner.
- GitHub Actions CI: `go vet` + `go build` + `go test -race` +
  `skillsig verify --no-color ./testdata/skills/`.
- `assets/demo.tape` (vhs script) + `assets/README.md` for regenerating the
  asciinema cast and GIF.
- Documented the one-time post-clone work that has to happen by hand
  (recording the demo, configuring real Fulcio OIDC).

### Known limitations
- `sign --keyless` returns `ErrFulcioNotWired` until sigstore-go lands.
  Use `--dev` (the default) for round-trippable but distribution-unsafe
  signing.
- `skillsig diff` is a stub (m3); cross-version drift is enforced inside
  `verify` via the lock file rather than as its own subcommand.

## [0.0.1] — 2026-06-04 (scaffold)

Initial cobra wiring and Go module skeleton. Not released.
