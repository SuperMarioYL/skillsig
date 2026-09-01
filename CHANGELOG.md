# Changelog

All notable changes to skillsig are tracked here. Format roughly follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning is
[Semantic](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing yet — the hosted-mirror tier (`skillsig.cloud` + team policy +
webhook alerts) is next.

## [0.10.0] — 2026-09-02

One version-correctness fix. The release no longer mis-reports its own
version when installed via the documented `go install ...@latest` path.

### Fixed
- **`go install ...@latest` now reports the real release version, not
  `0.1.0-dev` (high).** `cmd/skillsig/main.go` shipped
  `var version = "0.1.0-dev"` (a dev sentinel frozen since the m1 scaffold)
  and only the goreleaser `-X main.version={{.Version}}` ldflags overrode it
  at release time. The README hero command and `web/site.json` both point
  users at `go install github.com/SuperMarioYL/skillsig/cmd/skillsig@latest`,
  which is plain `go install`/`go build` — it passes no `-ldflags`, so the
  only in-source version surface shipped verbatim. Empirically confirmed at
  the v0.9.0 tag: `go build ./cmd/skillsig && ./skillsig --version` printed
  `skillsig version 0.1.0-dev`, not `0.9.0`. So every release from v0.1.0
  through v0.9.0 mis-reports its own version to any user who installs via the
  documented path (and the advertised Homebrew tap does not yet exist, so
  `go install ...@latest` is the only install path). **Fix:** the VERSION file
  is now the single source of truth via `//go:embed`. A new module-root
  `skillsig` package embeds the VERSION file and exposes
  `Version = strings.TrimSpace(rawVersion)`; `cmd/skillsig/main.go` now
  initializes `var version = skillsig.Version`. A plain
  `go install ...@latest` embeds the VERSION file at the tagged commit and
  reports the real release version; the goreleaser ldflags override still wins
  when present, so the release-artifact path is unchanged. No new
  dependencies (only the stdlib `embed` package). New
  `cmd/skillsig/version_test.go` builds the binary WITHOUT ldflags (exactly
  the `go install ...@latest` path) and asserts `--version` equals
  `skillsig version <VERSION-file>` (fails on v0.9.0 — `0.1.0-dev` ≠ `0.9.0`;
  passes after), plus a lockstep test asserting `main.version`,
  `skillsig.Version`, the root command's `Version`, and
  `web/site.json` `content_version` all equal the canonical VERSION file so a
  future bump cannot silently re-drift an in-source version surface.

## [0.9.0] — 2026-08-21

Two verification-correctness fixes. One closes a supply-chain bypass where a
forged keyless-shaped bundle read `TRUSTED` (and passed `verify --ci`, and could
be pinned by `verify --trust` as the drift baseline); the other stops a
narrowing change (removing `declares.model`) from being falsely flagged as
scope growth.

### Security / Fixed
- **A forged keyless bundle no longer reads TRUSTED (high).** A keyless-shaped
  bundle (`verificationMaterial.publicKey` empty, `certificate` non-empty) still
  reaches `VerdictKeylessPending` at the verifier level (unchanged), but the CLI
  path used to leave the row `TRUSTED` — `applySignatureVerdicts` only annotated
  `Details` — so a forged bundle read `TRUSTED` and passed `verify --ci`. Since
  `skillsig sign --keyless` returns `ErrFulcioNotWired` in this build, any
  keyless bundle `verify` encounters is a forgery. **Fix:**
  `applySignatureVerdicts` now downgrades `VerdictKeylessPending` to `UNSIGNED`
  (set `scope.VerdictUnsigned` + `"keyless bundle not verified in this build"`),
  so `verify --ci` fails and the row cannot pass. The `--trust` gate
  (`TrustRecordGate`) now pins ONLY `VerdictSigned` (was: `VerdictSigned ||
  VerdictKeylessPending`), so a never-verified keyless scope is never pinned into
  `~/.skillsig/lock.yaml` as the drift baseline. The verifier-level
  `VerdictKeylessPending` (`internal/verifier/verifier.go`) stays so a future
  keyless-enabled build can still surface it; only the CLI display / trust /
  `--ci` decisions change. New `cmd/skillsig/verify_test.go` test
  `TestRunVerify_KeylessForgedBundleIsUnsignedNotTrusted` covers all three
  directions (row is UNSIGNED not TRUSTED; `--ci` fails with `ErrCIDrift`;
  `--trust` does not pin it).
- **Removing `declares.model` is no longer falsely reported as scope growth
  (high).** `scopeGrowth`'s model axis read `curr.Model != prev.Model &&
  prev.Model != ""` with NO `curr.Model != ""` guard, so REMOVING
  `declares.model` (`curr.Model == ""`) was reported as scope growth — and
  `applyLockDrift` then upgraded a `TRUSTED` row to `SCOPE-DRIFTED`, blocking a
  non-escalating (narrowing) change. This violated the "a removed entry is NOT
  growth, only additions are" contract the `tools` / `fs_write` /
  `network_egress` axes honor (they early-return on an empty `curr`). **Fix:**
  add `&& curr.Model != ""` to the guard. A genuine model swap (A→B) still
  surfaces for re-attestation; only removal stops being flagged. New
  `internal/scope/scanner_test.go` test
  `TestScopeGrowth_ModelRemovalIsNotGrowthButSwapIs` covers both directions.

## [0.7.0] — 2026-07-13

One verification-correctness fix in the same trust/lock class the two v0.6.0
fixes opened — the completing gap they missed. v0.6.0 made a `TRUSTED` verdict
require a valid signature in what `verify` *displays* and fails on in `--ci`,
but the `verify --trust` *recording* path still decided what to pin from the
scope check alone.

### Security / Fixed
- **`verify --trust` no longer pins the scope of an UNSIGNED or forged-bundle
  skill into the lock (high).** v0.6.0 folded signature verification into the
  verify report rows (`applySignatureVerdicts`), so a scope-clean skill with no
  bundle shows `UNSIGNED` and one with a bundle that doesn't verify shows
  `SCOPE-DRIFTED`. But `Scanner.ScanAndTrust` — the code `verify --trust` uses to
  write `~/.skillsig/lock.yaml` — chose what to pin purely from the **scope**
  verdict, which the `scope` package computes with no knowledge of the signature.
  So a skill with a manifest and matching scope but **no signature bundle** was
  displayed `UNSIGNED` yet still had its declared scope silently recorded into
  the lock — contradicting `ScanAndTrust`'s own "UNSIGNED skills are never
  recorded — you only ever pin scopes you actually vouch for" contract, and
  re-opening a laundering path: once that never-vouched skill later ships any
  valid bundle, its (never-attested) broad scope matches the pre-seeded baseline
  and reads fully `TRUSTED` with no drift. Reproduced with a bundle-less skill
  declaring `Bash(rm -rf ~/)` — `verify --trust` printed `UNSIGNED` but wrote
  that `rm -rf ~/` scope into the lock. **Fix:** the trust path is now gated on
  the signature verdict. `Scanner` gains a `TrustRecordGate` hook (nil keeps the
  old record-every-scope-TRUSTED behaviour); `ScanAndTrust` consults it before
  pinning any entry, on both the clean-`TRUSTED` and the `--force-trust`
  re-baseline paths. `verify` computes each skill's signature verdict once (new
  shared `signatureVerdicts` helper) and, under `--trust`, pins only `SIGNED` /
  `KEYLESS-PENDING` skills — exactly the set `verify` shows as `TRUSTED` — so
  `NO-BUNDLE` and `BAD-SIGNATURE` skills are never recorded. Two new tests in
  `cmd/skillsig/verify_test.go` cover both directions (an unsigned skill is left
  out of the lock; a properly signed skill is still pinned).

## [0.6.0] — 2026-07-06

Two verification-correctness fixes. Until now a `TRUSTED` verdict meant "the
declared scope matches the runtime grants (and the lock)" but never "the
signature is valid" — and `verify --trust` could silently launder a
cross-version scope escalation into the lock. Both are closed.

### Security / Fixed
- **`verify` now actually checks the signature bundle (high).** `runVerify`
  never called the `verifier` package — `verifier.VerifySkill` (which
  canonicalizes the manifest, locates `skillsig.bundle`, and validates the
  ed25519 signature) was fully implemented but had **zero callers**. So a
  scope-clean skill with no bundle, or with a bundle that didn't match its
  manifest, still read `TRUSTED`. An attacker who tampered a manifest to cover a
  malicious grant and shipped no/invalid bundle passed `verify --ci` `TRUSTED`
  at cold-start (before any lock baseline exists) — the exact supply-chain
  vector skillsig exists to catch, and one the README already promised was
  covered. `verify` now folds signature verification into every row: a missing
  bundle downgrades `TRUSTED` → `UNSIGNED`, a bundle that doesn't verify (tampered
  or forged) downgrades `TRUSTED` → `SCOPE-DRIFTED` (and fails `--ci`), keyless
  bundles surface as pending, and `TRUSTED` now requires a valid signature. A new
  `internal/verifier/verifier_test.go` covers the signed / no-bundle / tampered /
  corrupt / keyless cases.
- **`verify --trust` no longer silently re-baselines a drifted scope (high).**
  `ScanAndTrust` ran only the in-version check and then recorded the *current*
  (possibly broadened) declares into the lock for every in-version-`TRUSTED`
  skill — so a skill that quietly broadened its scope vs. the existing baseline
  got re-baselined to the broadened scope and printed `TRUSTED`, permanently
  erasing the drift. `verify --trust` now applies the cross-version lock-drift
  check first: a drifted skill is reported `SCOPE-DRIFTED` and its baseline is
  left intact (so a later plain `verify` still catches it). Re-baselining a
  drifted skill on purpose requires the new explicit `verify --trust --force-trust`.

## [0.5.0] — 2026-07-03

Closes the last un-boundaried scope axis. The path-shaped axes (`fs_write`,
`network_egress`) got boundary-aware coverage in v0.3.0, but the **tools** axis
still matched wildcards with a raw prefix — so a re-signed skill could swap a
declared subcommand for a same-prefix sibling command and escape SCOPE-DRIFTED.
All four declared-scope axes now agree on what "broader" means.

### Security / Fixed
- **Tools-axis wildcard prefix confusion (high).** `matchGlob` honored a
  `Tool(prefix*)` grant with a raw `strings.HasPrefix`, so a declared
  `Bash(git status*)` covered not just refinements like `Bash(git status -s)`
  but also a look-alike **sibling** command `Bash(git statuscheckout --force)` —
  a *different* subcommand token that merely shares the text prefix `git status`.
  Because the cross-version growth check flows through this same predicate
  (`scopeGrowth → addedTools → covered → matchGlob`), a re-signed skill that
  swapped a declared subcommand for a same-prefix sibling passed SCOPE-DRIFTED
  silently on both `skillsig diff` and lock-aware `verify --ci` — the exact
  cross-version escalation skillsig exists to catch. The wildcard is now
  **argument-token boundary-aware**: the prefix covers a candidate only when the
  extra text begins a new argument token (a space boundary), mirroring the `/`
  (path) and `.` (host) boundary discipline already used by `fs_write` /
  `network_egress` in `internal/scope/glob.go`. Refinements
  (`Bash(git status -s)`, `Bash(git status --porcelain)`) stay covered; the
  look-alike sibling is now flagged growth.

### Tests
- New `internal/scope/scope_test.go` pins both directions: a look-alike sibling
  token (`git statuscheckout`) is reported as growth on the shared
  `scopeGrowth`/`covered`/`matchGlob` path, and a space-delimited refinement
  (`git status -s` / `--porcelain`) is not — no false positive.

## [0.4.0] — 2026-06-30

Closes the gap that let the product's headline check sit unreachable: the
cross-version (lock) drift detector now runs inside `verify` — and therefore
inside `verify --ci` and the SARIF annotations — not only inside `diff`. Plus a
SARIF stdout fix so `--sarif -` is machine-readable.

### Security / Fixed
- **`verify` skipped the lock-aware drift path (high).** `verify` called the
  in-version `scope.EvaluateAll` directly and never constructed a
  `scope.Scanner`, so a re-signed skill that quietly broadened its `fs_write` /
  `network_egress` / `tools` (while keeping its `allowed-tools` inside the
  declared set) passed `verify --ci` silently — the exact cross-version jqwik
  vector skillsig exists to catch. The v0.3.0 boundary-aware glob fixes lived in
  the lock path, so they only took effect in `diff`, never at the CI gate or in
  the SARIF output. `verify` now goes through `scope.DefaultScanner().Scan`,
  which applies the lock-drift comparison, so a broadened re-signed skill is
  flagged `SCOPE-DRIFTED` on a plain `verify` and fails `verify --ci`.
- **`--sarif -` produced unparseable stdout (medium).** With the SARIF target set
  to `-` (stdout), `verify` wrote the human table (or, with `--json`, the JSON
  report) to stdout and then appended the SARIF document to the same stream — so
  stdout was neither a valid SARIF file nor a valid `--json` object, and
  `--json --sarif -` joined two root JSON objects back-to-back. SARIF is now the
  **sole** stdout artifact in `--sarif -` mode (the table/JSON is suppressed),
  so `github/codeql-action/upload-sarif` reading from stdout gets one valid
  document.

### Added
- **`verify --trust`** seeds (or refreshes) `~/.skillsig/lock.yaml` from the
  currently-TRUSTED corpus, so the lock has a baseline for the next run to
  compare against. Honors `$SKILLSIG_HOME` for hermetic/CI use. A skill that is
  SCOPE-DRIFTED or UNSIGNED is never recorded — you only pin scopes you trust.

### Tests
- Lock-drift through `verify`: trust a corpus, re-sign one skill with a broadened
  grant, assert plain `verify` reports `SCOPE-DRIFTED` and `verify --ci` exits
  non-zero; plus the inverse (no baseline ⇒ stays TRUSTED).
- SARIF stdout: `--sarif -` and `--json --sarif -` each emit exactly one valid
  JSON/SARIF document with no concatenated second object and no table header.

## [0.3.0] — 2026-06-27

Hardens the cross-version drift detector against two glob-coverage holes the
v0.2.0 "glob-aware on every axis" change introduced, and adds SARIF output so
GitHub code-scanning annotates drift inline on the pull request.

### Security / Fixed
- **Prefix-confusion in drift glob coverage (high).** The v0.2.0 coverage used a
  raw `strings.HasPrefix` after stripping a trailing `*`/`**`, with no segment
  boundary. A declared `network_egress` glob `api.github.com*` therefore reported
  the newly added host `api.github.com.attacker.net` as already covered, and an
  `fs_write` glob `/workspace/foo*` covered `/workspace/foobar-evil` — so a
  re-signed skill that broadened its grants was **not** flagged SCOPE-DRIFTED on
  either `diff` or lock-aware `verify`. Coverage is now segment-boundary aware:
  a glob prefix only covers a candidate when the remainder is empty or begins at
  a path/host separator (`/` for paths, `.` for hosts).
- **`*` vs `**` collapse (medium).** A single trailing `*` and `**` were treated
  identically, so `${WORKSPACE}/*` (intended: direct children) silently covered
  the deep path `${WORKSPACE}/a/b/secret` and even a `..` traversal. A single `*`
  now matches within one segment only, `**` is recursive, and a `..` remainder is
  always reported as growth.
- Both fixes are factored into a new boundary-aware `internal/scope/glob.go`
  helper shared by the `verify` and `diff` paths so they can never disagree on
  what "broader" means. New `glob_test.go` covers the look-alike host, the
  sibling-prefix path, the deep single-`*` path, traversal, and a true refinement.

### Added
- `verify --ci --sarif <out.sarif>` (`-` for stdout): emit a SARIF 2.1.0 report
  with one result per SCOPE-DRIFTED (level=error) / UNSIGNED (level=warning)
  skill, so a GitHub Actions `github/codeql-action/upload-sarif` step renders the
  drift as an inline annotation on the offending pull request. Plain `--json`
  (v0.2.0) is unchanged. Backed by `report.RenderSARIF`.

## [0.2.0] — 2026-06-19

First feature iteration on top of the v0.1 line. Tightens cross-version drift
correctness and gives CI a machine-readable output it can branch on.

### Added
- `verify --json` and `diff --json`: emit a stable, snake-keyed JSON report
  instead of the human table. `verify --json` carries a per-skill array, a
  summary tally, and a top-level `drift` boolean (true iff any row is UNSIGNED
  or SCOPE-DRIFTED — the same condition `--ci` exits non-zero on). `diff --json`
  carries `escalation` plus the offending grants. CI pipelines can now parse
  verdicts with `jq` rather than scraping the colored table.
- `report.RenderJSON` backing the new flag, alongside the existing `Render`.

### Fixed
- Cross-version scope diff (`skillsig diff` and the `~/.skillsig/lock.yaml`
  lock check) is now glob-aware on every axis. Previously `scopeGrowth` used a
  literal string set-difference, so *tightening* an existing grant was reported
  as an escalation: narrowing `Bash(git status*)` to `Bash(git status -s)`, or
  narrowing `${WORKSPACE}/**` to `${WORKSPACE}/build/out.txt`, both falsely
  failed CI. The diff now reuses the same coverage predicate as the in-version
  `verify` check (literal match or a `Tool(prefix*)` / path-prefix glob), so a
  refinement under an already-declared scope is no longer flagged — only a
  genuinely new, uncovered grant is. Regression tests cover both the tools axis
  and the fs_write / network_egress path axes.

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
